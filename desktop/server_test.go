package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordingNotifier struct {
	mu      sync.Mutex
	notices []Notice
}

func (r *recordingNotifier) Notify(n Notice) {
	r.mu.Lock()
	r.notices = append(r.notices, n)
	r.mu.Unlock()
}

func (r *recordingNotifier) all() []Notice {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Notice(nil), r.notices...)
}

func newTestServer(t *testing.T) (*Store, *recordingNotifier, *httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TOSS_CONFIG_DIR", filepath.Join(dir, "config"))
	store, err := OpenStore()
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := store.Config()
	cfg.SaveDir = filepath.Join(dir, "received")
	cfg.AutoCopy = false
	off := false
	cfg.AutoOpen = &off
	store.SaveConfig(cfg)
	notes := &recordingNotifier{}
	srv := NewServer(store, notes)
	srv.open = func(target string) error { t.Errorf("opened %s with auto-open off", target); return nil }
	srv.paste = func() (string, error) { return "clip ěš", nil }
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return store, notes, ts, cfg.SaveDir
}

func pairDevice(t *testing.T, store *Store, ts *httptest.Server) string {
	t.Helper()
	p, _ := store.NewPairing(time.Minute)
	body, _ := json.Marshal(map[string]string{"token": p.Token, "deviceName": "Hiha", "deviceId": "dev-1"})
	resp, err := http.Post(ts.URL+"/v1/pair", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("pair: %d", resp.StatusCode)
	}
	var out map[string]string
	json.NewDecoder(resp.Body).Decode(&out)
	if out["deviceKey"] == "" || out["computerId"] == "" {
		t.Fatalf("pair response %v", out)
	}
	return out["deviceKey"]
}

func send(t *testing.T, ts *httptest.Server, key, kind, name string, body []byte, headers map[string]string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/v1/items", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("X-Toss-Kind", kind)
	if name != "" {
		req.Header.Set("X-Toss-Name", url.PathEscape(name))
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestPairingTokenWorksOnce(t *testing.T) {
	store, notes, ts, _ := newTestServer(t)
	p, _ := store.NewPairing(time.Minute)
	body, _ := json.Marshal(map[string]string{"token": p.Token, "deviceName": "Hiha", "deviceId": "d"})
	r1, _ := http.Post(ts.URL+"/v1/pair", "application/json", bytes.NewReader(body))
	r2, _ := http.Post(ts.URL+"/v1/pair", "application/json", bytes.NewReader(body))
	if r1.StatusCode != 200 || r2.StatusCode != 403 {
		t.Fatalf("statuses %d %d, want 200 403", r1.StatusCode, r2.StatusCode)
	}
	if d := store.Devices(); len(d) != 1 || d[0].Name != "Hiha" || d[0].KeyHash == "" {
		t.Fatalf("devices %+v", d)
	}
	if n := notes.all(); len(n) != 1 || !strings.Contains(n[0].Title, "Paired") {
		t.Fatalf("notices %+v", n)
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	store, _, ts, _ := newTestServer(t)
	p, _ := store.NewPairing(-time.Second)
	body, _ := json.Marshal(map[string]string{"token": p.Token, "deviceName": "x", "deviceId": "d"})
	r, _ := http.Post(ts.URL+"/v1/pair", "application/json", bytes.NewReader(body))
	if r.StatusCode != 403 {
		t.Fatalf("status %d", r.StatusCode)
	}
}

func TestAuthRequired(t *testing.T) {
	store, _, ts, _ := newTestServer(t)
	pairDevice(t, store, ts)
	if r := send(t, ts, "wrong", "text", "", []byte("hi"), nil); r.StatusCode != 401 {
		t.Fatalf("bad key: %d", r.StatusCode)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/v1/ping", nil)
	r, _ := http.DefaultClient.Do(req)
	if r.StatusCode != 401 {
		t.Fatalf("no key: %d", r.StatusCode)
	}
}

func TestUnpairRevokes(t *testing.T) {
	store, _, ts, _ := newTestServer(t)
	key := pairDevice(t, store, ts)
	if n, _ := store.RemoveDevice("Hiha"); n != 1 {
		t.Fatal("not removed")
	}
	if r := send(t, ts, key, "text", "", []byte("hi"), nil); r.StatusCode != 401 {
		t.Fatalf("revoked key still works: %d", r.StatusCode)
	}
}

func TestTextAndLinkNotices(t *testing.T) {
	store, notes, ts, _ := newTestServer(t)
	key := pairDevice(t, store, ts)
	send(t, ts, key, "text", "", []byte("Ahoj ěščř"), nil)
	send(t, ts, key, "url", "", []byte("https://example.com/a?b=1"), nil)
	send(t, ts, key, "url", "", []byte("javascript:alert(1)"), nil)
	n := notes.all()[1:] // after the pairing notice
	if len(n) != 3 {
		t.Fatalf("got %d notices", len(n))
	}
	if n[0].Title != "Text from Hiha" || n[0].Body != "Ahoj ěščř" || n[0].Actions[0].Kind != ActionCopy {
		t.Errorf("text notice %+v", n[0])
	}
	if n[1].Title != "Link from Hiha" || n[1].Default == nil || n[1].Default.Target != "https://example.com/a?b=1" {
		t.Errorf("link notice %+v", n[1])
	}
	if n[2].Title != "Text from Hiha" {
		t.Errorf("non-web link should be shown as text, got %+v", n[2])
	}
}

func TestFileUploadSavedUniquely(t *testing.T) {
	store, notes, ts, dir := newTestServer(t)
	key := pairDevice(t, store, ts)
	for i := 0; i < 2; i++ {
		r := send(t, ts, key, "file", "IMG 1.jpg", []byte("jpegdata"), map[string]string{"Content-Type": "image/jpeg"})
		if r.StatusCode != 200 {
			t.Fatalf("upload %d", r.StatusCode)
		}
	}
	r := send(t, ts, key, "file", "../../evil.txt", []byte("x"), nil)
	var out map[string]string
	json.NewDecoder(r.Body).Decode(&out)
	if filepath.Dir(out["saved"]) != dir {
		t.Fatalf("saved outside the folder: %s", out["saved"])
	}
	entries, _ := os.ReadDir(dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "IMG 1 (2).jpg,IMG 1.jpg,evil.txt" {
		t.Fatalf("files %v", names)
	}
	n := notes.all()[1]
	if n.Title != "Photo from Hiha" || n.Image == "" || n.Default.Kind != ActionOpen {
		t.Errorf("photo notice %+v", n)
	}
}

func TestBatchMakesOneNotice(t *testing.T) {
	store, notes, ts, _ := newTestServer(t)
	key := pairDevice(t, store, ts)
	for i, name := range []string{"a.jpg", "b.jpg", "c.png"} {
		send(t, ts, key, "file", name, []byte("x"), map[string]string{
			"X-Toss-Batch": "b1", "X-Toss-Index": string(rune('0' + i)), "X-Toss-Count": "3", "Content-Type": "image/jpeg",
		})
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(notes.all()) < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	n := notes.all()
	if len(n) != 2 || n[1].Title != "3 photos from Hiha" || n[1].Actions[0].Kind != ActionShowFolder {
		t.Fatalf("notices %+v", n)
	}
}

func TestSafeFileName(t *testing.T) {
	for in, want := range map[string]string{
		"photo.jpg":          "photo.jpg",
		"a/b/c.txt":          "c.txt",
		`C:\Users\x\d.pdf`:   "d.pdf",
		"what?.txt":          "what_.txt",
		"CON.txt":            "_CON.txt",
		"...":                "file",
		"":                   "file",
		"žluťoučký kůň.heic": "žluťoučký kůň.heic",
	} {
		if got := safeFileName(in); got != want {
			t.Errorf("safeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("é", 300) + ".jpg"
	if got := safeFileName(long); len(got) > 200 || !strings.HasSuffix(got, ".jpg") {
		t.Errorf("long name %d bytes", len(got))
	}
}

func TestPairingLink(t *testing.T) {
	link := PairingLink("honza laptop", []string{"honza-t490.local", "192.168.0.5"}, 47800, "FP_-x", "TOK")
	want := "toss://pair?v=1&n=honza+laptop&h=honza-t490.local%2C192.168.0.5&p=47800&fp=FP_-x&t=TOK"
	if link != want {
		t.Fatalf("link\n%s\nwant\n%s", link, want)
	}
	if len(PairingLink("honza-t490", []string{"honza-t490.local", "192.168.100.100", "10.10.10.10", "172.20.10.2"}, 47800,
		randomToken(32), randomToken(16))) > 192 {
		t.Error("a typical link should fit a version 8 code (49×49)")
	}
}

func TestCertificateStable(t *testing.T) {
	t.Setenv("TOSS_CONFIG_DIR", t.TempDir())
	store, _ := OpenStore()
	a, err := store.Certificate("x")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := store.Certificate("x")
	if Fingerprint(a) != Fingerprint(b) || len(Fingerprint(a)) != 43 {
		t.Fatal("fingerprint changed or wrong length")
	}
}

func TestAutoOpenLinks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TOSS_CONFIG_DIR", dir)
	store, _ := OpenStore()
	cfg, _ := store.Config()
	if !cfg.OpensLinks() {
		t.Fatal("auto-open should default to on")
	}
	cfg.SaveDir = filepath.Join(dir, "received")
	cfg.AutoCopy = false
	store.SaveConfig(cfg)
	notes := &recordingNotifier{}
	srv := NewServer(store, notes)
	var opened []string
	srv.open = func(target string) error { opened = append(opened, target); return nil }
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	key := pairDevice(t, store, ts)
	send(t, ts, key, "url", "", []byte(" https://example.com/x \n"), nil)
	send(t, ts, key, "url", "", []byte("file:///etc/passwd"), nil)
	send(t, ts, key, "text", "", []byte("https://not-a-link-item.example"), nil)
	if len(opened) != 1 || opened[0] != "https://example.com/x" {
		t.Fatalf("opened %v, want only the web link", opened)
	}
	n := notes.all()
	if n[1].Title != "Opened link from Hiha" || n[1].Default != nil || n[1].Actions[0].Label != "Open again" {
		t.Errorf("opened-link notice %+v", n[1])
	}
	if n[2].Title != "Text from Hiha" {
		t.Errorf("file: link should be plain text, got %+v", n[2])
	}
}

func TestReceivedFilesAreReadable(t *testing.T) {
	path, err := saveUpload(t.TempDir(), "a.txt", strings.NewReader("x"))
	if err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o644 {
		t.Errorf("mode %v, want 0644", info.Mode().Perm())
	}
}

func TestClipboardEndpoint(t *testing.T) {
	store, _, ts, _ := newTestServer(t)
	key := pairDevice(t, store, ts)
	req, _ := http.NewRequest("GET", ts.URL+"/v1/clipboard", nil)
	if r, _ := http.DefaultClient.Do(req); r.StatusCode != 401 {
		t.Fatalf("clipboard without a key: %d", r.StatusCode)
	}
	req.Header.Set("Authorization", "Bearer "+key)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	json.NewDecoder(r.Body).Decode(&out)
	if out["text"] != "clip ěš" {
		t.Fatalf("clipboard %q", out["text"])
	}
}
