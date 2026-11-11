package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func authed(t *testing.T, method, url, key string, body io.Reader) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, url, body)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestResumableUpload(t *testing.T) {
	store, _, ts, dir := newTestServer(t)
	key := pairDevice(t, store, ts)
	data := bytes.Repeat([]byte("0123456789"), 1000)
	id := "upload-abc123"
	total := strconv.Itoa(len(data))

	// First part only: the computer keeps it and says how much it has.
	resp := send(t, ts, key, "file", "clip.mov", data[:4000], map[string]string{"X-Toss-Upload": id, "X-Toss-Size": total})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("partial upload: %d", resp.StatusCode)
	}
	var st map[string]int64
	r := authed(t, "GET", ts.URL+"/v1/uploads/"+id, key, nil)
	json.NewDecoder(r.Body).Decode(&st)
	if st["received"] != 4000 {
		t.Fatalf("received %v", st)
	}
	// A wrong offset is refused.
	resp = send(t, ts, key, "file", "clip.mov", data[100:], map[string]string{"X-Toss-Upload": id, "X-Toss-Size": total, "X-Toss-Offset": "100"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("wrong offset: %d", resp.StatusCode)
	}
	// The rest completes the file.
	resp = send(t, ts, key, "file", "clip.mov", data[4000:], map[string]string{"X-Toss-Upload": id, "X-Toss-Size": total, "X-Toss-Offset": "4000"})
	if resp.StatusCode != 200 {
		t.Fatalf("resume: %d", resp.StatusCode)
	}
	got, err := os.ReadFile(dir + "/clip.mov")
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("file differs (%d bytes, %v)", len(got), err)
	}
	if _, err := os.Stat(partialPath(dir, id)); !os.IsNotExist(err) {
		t.Fatal("partial file left behind")
	}
	if resp := send(t, ts, key, "file", "x", []byte("x"), map[string]string{"X-Toss-Upload": "../../etc"}); resp.StatusCode != 400 {
		t.Fatalf("bad upload id: %d", resp.StatusCode)
	}
}

func TestCaptureTimeAndHistory(t *testing.T) {
	store, _, ts, dir := newTestServer(t)
	key := pairDevice(t, store, ts)
	taken := time.Date(2024, 7, 14, 18, 30, 5, 0, time.FixedZone("CEST", 2*3600))
	resp := send(t, ts, key, "file", "IMG_1.jpg", []byte("jpeg"), map[string]string{"X-Toss-Modified": taken.Format(time.RFC3339)})
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	info, err := os.Stat(dir + "/IMG_1.jpg")
	if err != nil || !info.ModTime().Equal(taken) {
		t.Fatalf("mtime %v, want %v (%v)", info.ModTime(), taken, err)
	}
	send(t, ts, key, "text", "", []byte("hello"), nil)
	h := store.History()
	if len(h) != 2 || h[0].Kind != "text" || h[0].Text != "hello" || h[1].Path != dir+"/IMG_1.jpg" || h[1].Device != "Hiha" || h[1].Size != 4 {
		t.Fatalf("history %+v", h)
	}
}

func TestInbox(t *testing.T) {
	store, notes, ts, _ := newTestServer(t)
	key := pairDevice(t, store, ts)
	if _, err := store.AddOutText("https://example.com/a"); err != nil {
		t.Fatal(err)
	}
	file, err := store.AddOutReader("report.pdf", strings.NewReader("%PDF-1.7 hello"))
	if err != nil {
		t.Fatal(err)
	}
	var list struct{ Items []OutItem }
	r := authed(t, "GET", ts.URL+"/v1/inbox", key, nil)
	json.NewDecoder(r.Body).Decode(&list)
	if len(list.Items) != 2 || list.Items[0].Kind != "url" || list.Items[1].Name != "report.pdf" || list.Items[1].Size != 14 {
		t.Fatalf("inbox %+v", list.Items)
	}
	r = authed(t, "GET", ts.URL+"/v1/inbox/"+file.ID, key, nil)
	body, _ := io.ReadAll(r.Body)
	if string(body) != "%PDF-1.7 hello" || r.Header.Get("X-Toss-Name") != "report.pdf" || r.Header.Get("Content-Type") != "application/pdf" {
		t.Fatalf("download %q %v", body, r.Header)
	}
	// Range requests resume big downloads.
	req, _ := http.NewRequest("GET", ts.URL+"/v1/inbox/"+file.ID, nil)
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Range", "bytes=9-")
	r, _ = http.DefaultClient.Do(req)
	body, _ = io.ReadAll(r.Body)
	if r.StatusCode != http.StatusPartialContent || string(body) != "hello" {
		t.Fatalf("range %d %q", r.StatusCode, body)
	}
	if r := authed(t, "GET", ts.URL+"/v1/inbox", "wrong", nil); r.StatusCode != 401 {
		t.Fatalf("inbox without key: %d", r.StatusCode)
	}
	for _, it := range list.Items {
		if r := authed(t, "DELETE", ts.URL+"/v1/inbox/"+it.ID, key, nil); r.StatusCode != 200 {
			t.Fatalf("delete: %d", r.StatusCode)
		}
	}
	if len(store.Outbox()) != 0 {
		t.Fatal("inbox not empty")
	}
	if _, err := os.Stat(store.outboxFile(file.ID)); !os.IsNotExist(err) {
		t.Fatal("file left in the outbox")
	}
	if r := authed(t, "DELETE", ts.URL+"/v1/inbox/"+file.ID, key, nil); r.StatusCode != 404 {
		t.Fatalf("second delete: %d", r.StatusCode)
	}
	time.Sleep(2500 * time.Millisecond)
	var got bool
	for _, n := range notes.all() {
		if n.Title == "Hiha got 2 items" {
			got = true
		}
	}
	if !got {
		t.Fatalf("no pickup notice: %+v", notes.all())
	}
}

func TestOutboxExpires(t *testing.T) {
	store, _, _, _ := newTestServer(t)
	it, _ := store.AddOutReader("old.txt", strings.NewReader("x"))
	store.updateOutbox(func(list []OutItem) []OutItem {
		list[0].Created = time.Now().Add(-8 * 24 * time.Hour)
		return list
	})
	if len(store.Outbox()) != 0 {
		t.Fatal("expired item still listed")
	}
	if _, err := os.Stat(store.outboxFile(it.ID)); !os.IsNotExist(err) {
		t.Fatal("expired file kept")
	}
}

func TestIsWebLink(t *testing.T) {
	for text, want := range map[string]bool{
		"https://example.com":  true,
		" http://a.b/c?d=1 \n": true,
		"example.com":          false,
		"see https://a.b":      false,
		"file:///etc/passwd":   false,
		"https://":             false,
		"javascript:alert(1)":  false,
	} {
		if got := isWebLink(text); got != want {
			t.Errorf("isWebLink(%q) = %v", text, got)
		}
	}
}

func newWeb(t *testing.T) (*Store, *httptest.Server, string) {
	store, _, _, _ := newTestServer(t)
	token, err := store.WebToken()
	if err != nil {
		t.Fatal(err)
	}
	ws := &webServer{store: store, token: token}
	ts := httptest.NewServer(nil)
	ws.port, _ = strconv.Atoi(ts.URL[strings.LastIndex(ts.URL, ":")+1:])
	ts.Config.Handler = ws.handler()
	t.Cleanup(ts.Close)
	return store, ts, token
}

func webCall(t *testing.T, ts *httptest.Server, method, path, token, host string, body io.Reader) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, ts.URL+path, body)
	if token != "" {
		req.Header.Set("X-Toss-Web", token)
	}
	if host != "" {
		req.Host = host
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestWebPage(t *testing.T) {
	store, ts, token := newWeb(t)
	if r := webCall(t, ts, "GET", "/", "", "", nil); r.StatusCode != 200 || !strings.Contains(r.Header.Get("Content-Type"), "text/html") {
		t.Fatalf("page: %d", r.StatusCode)
	}
	if r := webCall(t, ts, "GET", "/api/state", "", "", nil); r.StatusCode != 401 {
		t.Fatalf("state without token: %d", r.StatusCode)
	}
	if r := webCall(t, ts, "GET", "/api/state", token, "evil.example:80", nil); r.StatusCode != 403 {
		t.Fatalf("foreign Host: %d", r.StatusCode)
	}
	if r := webCall(t, ts, "POST", "/api/send/text", token, "", strings.NewReader("hi phone")); r.StatusCode != 200 {
		t.Fatalf("send text: %d", r.StatusCode)
	}
	if r := webCall(t, ts, "POST", "/api/send/file?name=a.txt", token, "", strings.NewReader("abc")); r.StatusCode != 200 {
		t.Fatalf("send file: %d", r.StatusCode)
	}
	if out := store.Outbox(); len(out) != 2 || out[0].Text != "hi phone" || out[1].Name != "a.txt" {
		t.Fatalf("outbox %+v", out)
	}
	// Only received files can be opened through the page.
	if r := webCall(t, ts, "POST", "/api/open", token, "", strings.NewReader(`{"path":"/etc/passwd"}`)); r.StatusCode != 403 {
		t.Fatalf("open arbitrary file: %d", r.StatusCode)
	}
	if r := webCall(t, ts, "GET", "/api/file?path=/etc/passwd", token, "", nil); r.StatusCode != 403 {
		t.Fatalf("read arbitrary file: %d", r.StatusCode)
	}
	r := webCall(t, ts, "POST", "/api/pair", token, "", nil)
	var p map[string]any
	json.NewDecoder(r.Body).Decode(&p)
	if svg, _ := p["svg"].(string); !strings.HasPrefix(svg, "<svg") || !strings.HasPrefix(p["link"].(string), "toss://pair?") {
		t.Fatalf("pair: %v", p)
	}
	r = webCall(t, ts, "GET", "/api/state", token, "", nil)
	var st map[string]any
	json.NewDecoder(r.Body).Decode(&st)
	if len(st["outbox"].([]any)) != 2 || st["history"] == nil {
		t.Fatalf("state %v", st)
	}
}
