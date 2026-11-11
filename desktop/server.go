package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const maxTextBytes = 10 << 20

// Server receives items from paired phones.
type Server struct {
	store    *Store
	notifier Notifier
	// Called after a phone pairs (for `toss pair`'s own server).
	onPaired func(Device)
	// Opens links that arrive (OpenURL; replaced in tests).
	open func(string) error
	// Reads the clipboard (PasteText; replaced in tests).
	paste func() (string, error)

	mu      sync.Mutex
	batches map[string]*batch
	// Items the phone picked up from the inbox, per device, for one "Hiha got 3 items" notice.
	pickups map[string]*pickup
}

type pickup struct {
	labels []string
	timer  *time.Timer
}

// batch collects items shared together so they make one notification.
type batch struct {
	device string
	count  int
	items  []received
	timer  *time.Timer
}

type received struct {
	kind   string // text, url, file
	text   string // text or URL
	path   string // saved file
	mime   string
	opened bool // a link already opened in the browser
	// Tag of the "Receiving…" notice shown for a big file, which the final notice replaces.
	progressTag string
}

func NewServer(store *Store, notifier Notifier) *Server {
	return &Server{store: store, notifier: notifier, open: OpenURL, paste: PasteText,
		batches: map[string]*batch{}, pickups: map[string]*pickup{}}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/pair", s.pair)
	mux.HandleFunc("GET /v1/ping", s.authed(s.ping))
	mux.HandleFunc("POST /v1/items", s.authed(s.item))
	mux.HandleFunc("GET /v1/clipboard", s.authed(s.clipboard))
	mux.HandleFunc("GET /v1/uploads/{id}", s.authed(s.uploadStatus))
	mux.HandleFunc("GET /v1/inbox", s.authed(s.inbox))
	mux.HandleFunc("GET /v1/inbox/{id}", s.authed(s.inboxItem))
	mux.HandleFunc("DELETE /v1/inbox/{id}", s.authed(s.inboxDelete))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	return mux
}

// ListenAndServe serves HTTPS until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, port int) error {
	cfg, err := s.store.Config()
	if err != nil {
		return err
	}
	cert, err := s.store.Certificate(cfg.ComputerName)
	if err != nil {
		return fmt.Errorf("certificate: %w", err)
	}
	removeStalePartials(saveDir(cfg))
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       60 * time.Second,
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		ErrorLog:          log.New(io.Discard, "", 0), // TLS handshake noise from port scanners
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	err = srv.ServeTLS(ln, "", "")
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type ctxKey struct{}

func (s *Server) authed(next func(http.ResponseWriter, *http.Request, Device)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || key == "" {
			writeError(w, http.StatusUnauthorized, "missing device key")
			return
		}
		dev, ok := s.store.DeviceForKey(key)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unknown device: pair again")
			return
		}
		next(w, r, dev)
	}
}

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token      string `json:"token"`
		DeviceName string `json:"deviceName"`
		DeviceID   string `json:"deviceId"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || req.Token == "" || req.DeviceID == "" {
		writeError(w, http.StatusBadRequest, "bad pairing request")
		return
	}
	if !s.store.TakePairing(req.Token) {
		writeError(w, http.StatusForbidden, "pairing code expired or already used: run `toss pair` again")
		return
	}
	name := strings.TrimSpace(req.DeviceName)
	if name == "" || !utf8.ValidString(name) {
		name = "iPhone"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	key := randomToken(32)
	dev := Device{ID: req.DeviceID, Name: name, KeyHash: hashKey(key), Paired: time.Now()}
	if err := s.store.AddDevice(dev); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cfg, _ := s.store.Config()
	log.Printf("paired with %s", name)
	s.notifier.Notify(Notice{Title: "Paired with " + name, Body: "Share anything to Toss on " + name + " and it shows up here."})
	if s.onPaired != nil {
		s.onPaired(dev)
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"deviceKey": key, "computerName": cfg.ComputerName, "computerId": cfg.ComputerID,
	})
}

func (s *Server) ping(w http.ResponseWriter, r *http.Request, dev Device) {
	cfg, _ := s.store.Config()
	writeJSON(w, http.StatusOK, map[string]string{"computerName": cfg.ComputerName, "version": version})
}

// clipboard hands the computer's clipboard text to the phone ("Get computer's clipboard").
func (s *Server) clipboard(w http.ResponseWriter, r *http.Request, dev Device) {
	text, err := s.paste()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	log.Printf("clipboard sent to %s (%d characters)", dev.Name, utf8.RuneCountInString(text))
	writeJSON(w, http.StatusOK, map[string]string{"text": text})
}

func (s *Server) item(w http.ResponseWriter, r *http.Request, dev Device) {
	kind := r.Header.Get("X-Toss-Kind")
	cfg, _ := s.store.Config()
	var item received
	switch kind {
	case "text", "url":
		body, err := io.ReadAll(io.LimitReader(r.Body, maxTextBytes+1))
		if err != nil {
			writeError(w, http.StatusBadRequest, "couldn't read the text")
			return
		}
		if len(body) > maxTextBytes {
			writeError(w, http.StatusRequestEntityTooLarge, "text too long")
			return
		}
		item = received{kind: kind, text: string(body)}
		if kind == "url" {
			if u, err := url.Parse(strings.TrimSpace(item.text)); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
				item.kind = "text" // only web links get an Open button
			}
		}
		if cfg.AutoCopy {
			if err := CopyText(item.text); err != nil {
				log.Printf("clipboard: %v", err)
			}
		}
		if item.kind == "url" && cfg.OpensLinks() {
			if err := s.open(strings.TrimSpace(item.text)); err != nil {
				log.Printf("opening link: %v", err)
			} else {
				item.opened = true
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{})
	case "file":
		name, _ := url.PathUnescape(r.Header.Get("X-Toss-Name"))
		var path string
		var err error
		uploadID := r.Header.Get("X-Toss-Upload")
		if uploadID != "" {
			if !validUploadID(uploadID) {
				writeError(w, http.StatusBadRequest, "bad X-Toss-Upload")
				return
			}
			var done bool
			path, done, err = s.resumableUpload(w, r, dev, saveDir(cfg), name, uploadID, &item)
			if !done {
				return // answered already (incomplete, or offset mismatch)
			}
		} else {
			path, err = saveUpload(saveDir(cfg), name, r.Body)
		}
		if err != nil {
			log.Printf("saving %q from %s: %v", name, dev.Name, err)
			writeError(w, http.StatusInternalServerError, "couldn't save the file: "+err.Error())
			return
		}
		setCaptureTime(path, r.Header.Get("X-Toss-Modified"))
		item.kind, item.path, item.mime = "file", path, r.Header.Get("Content-Type")
		writeJSON(w, http.StatusOK, map[string]string{"saved": path})
	default:
		writeError(w, http.StatusBadRequest, "unknown X-Toss-Kind")
		return
	}
	log.Printf("%s from %s: %s", item.kind, dev.Name, describe(item))
	entry := HistoryEntry{Time: time.Now(), Device: dev.Name, Kind: item.kind, Text: item.text, Path: item.path}
	if info, err := os.Stat(item.path); item.path != "" && err == nil {
		entry.Size = info.Size()
	}
	s.store.AddHistory(entry)
	count, _ := strconv.Atoi(r.Header.Get("X-Toss-Count"))
	s.collect(dev.Name, r.Header.Get("X-Toss-Batch"), count, item)
}

func describe(it received) string {
	if it.kind == "file" {
		return it.path
	}
	return truncate(strings.ReplaceAll(it.text, "\n", " "), 80)
}

// collect groups a batch's items and notifies once all arrived, or 4 s after the last one.
func (s *Server) collect(device, batchID string, count int, it received) {
	if batchID == "" || count <= 1 {
		s.notifyItems(device, []received{it})
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	b := s.batches[batchID]
	if b == nil {
		b = &batch{device: device, count: count}
		s.batches[batchID] = b
	}
	b.items = append(b.items, it)
	flush := func() {
		s.mu.Lock()
		cur := s.batches[batchID]
		delete(s.batches, batchID)
		s.mu.Unlock()
		if cur != nil {
			s.notifyItems(cur.device, cur.items)
		}
	}
	if b.timer != nil {
		b.timer.Stop()
	}
	if len(b.items) >= b.count {
		go flush()
	} else {
		b.timer = time.AfterFunc(4*time.Second, flush)
	}
}

func (s *Server) notifyItems(device string, items []received) {
	cfg, _ := s.store.Config()
	n := noticeFor(device, items, cfg.AutoCopy)
	for _, it := range items {
		if it.progressTag != "" {
			n.Tag = it.progressTag // replaces the "Receiving…" notice
		}
	}
	s.notifier.Notify(n)
}

// noticeFor builds the notification for items shared together.
func noticeFor(device string, items []received, autoCopied bool) Notice {
	if len(items) == 1 {
		it := items[0]
		switch it.kind {
		case "text":
			n := Notice{Title: "Text from " + device, Body: truncate(it.text, 300)}
			if autoCopied {
				n.Title += " (copied)"
			}
			n.Actions = []Action{{Label: "Copy", Kind: ActionCopy, Target: it.text}}
			return n
		case "url":
			n := Notice{Title: "Link from " + device, Body: truncate(it.text, 300)}
			if it.opened {
				// Already in the browser: the notice just says so, and can open it again.
				n.Title = "Opened link from " + device
				n.Actions = []Action{
					{Label: "Open again", Kind: ActionOpen, Target: strings.TrimSpace(it.text)},
					{Label: "Copy", Kind: ActionCopy, Target: it.text},
				}
				return n
			}
			if autoCopied {
				n.Title += " (copied)"
			}
			n.Actions = []Action{
				{Label: "Open", Kind: ActionOpen, Target: strings.TrimSpace(it.text)},
				{Label: "Copy", Kind: ActionCopy, Target: it.text},
			}
			n.Default = &n.Actions[0]
			return n
		default:
			n := Notice{Title: kindLabel(it) + " from " + device, Body: filepath.Base(it.path)}
			if strings.HasPrefix(it.mime, "image/") || isImageName(it.path) {
				n.Image = it.path
			}
			n.Actions = []Action{
				{Label: "Open", Kind: ActionOpen, Target: it.path},
				{Label: "Show in folder", Kind: ActionShowFolder, Target: it.path},
			}
			n.Default = &n.Actions[0]
			return n
		}
	}
	files, texts := 0, 0
	var names []string
	var lastFile, image string
	for _, it := range items {
		if it.kind == "file" {
			files++
			lastFile = it.path
			names = append(names, filepath.Base(it.path))
			if image == "" && (strings.HasPrefix(it.mime, "image/") || isImageName(it.path)) {
				image = it.path
			}
		} else {
			texts++
			names = append(names, truncate(strings.ReplaceAll(it.text, "\n", " "), 40))
		}
	}
	title := fmt.Sprintf("%d items from %s", len(items), device)
	if texts == 0 {
		allImages := true
		for _, it := range items {
			if !(strings.HasPrefix(it.mime, "image/") || isImageName(it.path)) {
				allImages = false
			}
		}
		if allImages {
			title = fmt.Sprintf("%d photos from %s", len(items), device)
		} else {
			title = fmt.Sprintf("%d files from %s", len(items), device)
		}
	}
	n := Notice{Title: title, Body: truncate(strings.Join(names, ", "), 300), Image: image}
	if files > 0 {
		n.Actions = []Action{{Label: "Show in folder", Kind: ActionShowFolder, Target: lastFile}}
		n.Default = &n.Actions[0]
	}
	return n
}

func kindLabel(it received) string {
	switch {
	case strings.HasPrefix(it.mime, "image/") || isImageName(it.path):
		return "Photo"
	case strings.HasPrefix(it.mime, "video/"):
		return "Video"
	default:
		return "File"
	}
}

func isImageName(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".heic", ".heif", ".bmp", ".tiff":
		return true
	}
	return false
}

func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}

// saveDir is where received files go: the configured folder or <Downloads>/From iPhone.
func saveDir(cfg Config) string {
	if cfg.SaveDir != "" {
		return cfg.SaveDir
	}
	return filepath.Join(downloadsDir(), "From iPhone")
}

// saveUpload streams body into dir under a safe, unique version of name.
func saveUpload(dir, name string, body io.Reader) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".toss-upload-*")
	if err != nil {
		return "", err
	}
	// CreateTemp makes the file private (0600); received files should be ordinary ones.
	_ = tmp.Chmod(0o644)
	defer os.Remove(tmp.Name()) // no-op after the rename
	if _, err := io.Copy(tmp, body); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	return moveIntoPlace(tmp.Name(), dir, name)
}

// moveIntoPlace gives a finished upload its final, unique name. Picking the name only now
// means two uploads of "IMG_1.jpg" at once don't collide.
func moveIntoPlace(tmp, dir, name string) (string, error) {
	for i := 1; ; i++ {
		final := filepath.Join(dir, uniqueName(safeFileName(name), i))
		if err := linkNoReplace(tmp, final); err == nil {
			return final, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
}

// uniqueName returns name for i == 1, then "name (2).ext", "name (3).ext"…
func uniqueName(name string, i int) string {
	if i <= 1 {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return fmt.Sprintf("%s (%d)%s", base, i, ext)
}

// safeFileName keeps a received name usable on Linux and Windows.
func safeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	var b strings.Builder
	for _, r := range name {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			b.WriteRune('_')
		} else {
			b.WriteRune(r)
		}
	}
	out := strings.Trim(strings.TrimSpace(b.String()), ".")
	if out == "" || out == "." {
		out = "file"
	}
	// Reserved device names on Windows.
	upper := strings.ToUpper(strings.TrimSuffix(out, filepath.Ext(out)))
	switch upper {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		out = "_" + out
	}
	if len(out) > 200 {
		ext := filepath.Ext(out)
		if len(ext) > 20 {
			ext = ""
		}
		out = truncateBytes(strings.TrimSuffix(out, ext), 200-len(ext)) + ext
	}
	return out
}

func truncateBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// linkNoReplace moves src to dst only if dst doesn't exist yet.
func linkNoReplace(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return os.Remove(src)
	} else if errors.Is(err, os.ErrExist) {
		return err
	}
	// Hard links can fail (e.g. FAT/exFAT drives): fall back to a checked rename.
	if _, err := os.Lstat(dst); err == nil {
		return os.ErrExist
	}
	return os.Rename(src, dst)
}
