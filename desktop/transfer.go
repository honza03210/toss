package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Resumable uploads (PROTOCOL.md): a file sent with X-Toss-Upload goes into a partial file
// that survives a dropped connection. The phone asks GET /v1/uploads/{id} how much arrived and
// sends the rest with X-Toss-Offset.

const partialPrefix = ".toss-partial-"

// bigFile is the size from which the computer shows "Receiving…" while a file arrives.
const bigFile = 50 << 20

func validUploadID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return true
}

func partialPath(dir, id string) string { return filepath.Join(dir, partialPrefix+id) }

func (s *Server) uploadStatus(w http.ResponseWriter, r *http.Request, dev Device) {
	id := r.PathValue("id")
	if !validUploadID(id) {
		writeError(w, http.StatusBadRequest, "bad upload id")
		return
	}
	cfg, _ := s.store.Config()
	var n int64
	if info, err := os.Stat(partialPath(saveDir(cfg), id)); err == nil {
		n = info.Size()
	}
	writeJSON(w, http.StatusOK, map[string]int64{"received": n})
}

// resumableUpload appends the request body to the upload's partial file. It returns done
// with the final path once the whole file is there; otherwise it has answered the request.
func (s *Server) resumableUpload(w http.ResponseWriter, r *http.Request, dev Device, dir, name, id string, it *received) (string, bool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", true, err
	}
	offset, _ := strconv.ParseInt(r.Header.Get("X-Toss-Offset"), 10, 64)
	total, _ := strconv.ParseInt(r.Header.Get("X-Toss-Size"), 10, 64)
	part := partialPath(dir, id)
	var have int64
	if info, err := os.Stat(part); err == nil {
		have = info.Size()
	}
	if offset != 0 && offset != have {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "upload offset doesn't match", "received": have})
		return "", false, nil
	}
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(part, flags, 0o644)
	if err != nil {
		return "", true, err
	}
	if total >= bigFile {
		it.progressTag = "upload-" + id
		what := "Receiving"
		if offset > 0 {
			what = "Resuming"
		}
		s.notifier.Notify(Notice{Tag: it.progressTag, Title: what + " from " + dev.Name + "…",
			Body: fmt.Sprintf("%s (%s)", safeFileName(name), humanSize(total))})
	}
	_, copyErr := io.Copy(f, r.Body)
	closeErr := f.Close()
	info, statErr := os.Stat(part)
	if copyErr != nil || closeErr != nil || statErr != nil {
		// Keep what arrived: the phone resumes from here.
		log.Printf("upload %s from %s interrupted at %d bytes", truncate(name, 60), dev.Name, sizeOr0(info))
		writeError(w, http.StatusBadRequest, "upload interrupted")
		return "", false, nil
	}
	if total > 0 && info.Size() < total {
		writeJSON(w, http.StatusConflict, map[string]any{"error": "upload incomplete", "received": info.Size()})
		return "", false, nil
	}
	path, err := moveIntoPlace(part, dir, name)
	return path, true, err
}

func sizeOr0(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	return info.Size()
}

// removeStalePartials deletes uploads nobody resumed for two days.
func removeStalePartials(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, partialPrefix+"*"))
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && time.Since(info.ModTime()) > 48*time.Hour {
			os.Remove(m)
		}
	}
}

// setCaptureTime sets a received file's modification time to when the photo or video was
// taken (X-Toss-Modified, RFC 3339), so photo apps on the computer sort it correctly.
func setCaptureTime(path, value string) {
	if value == "" {
		return
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil || t.Year() < 1990 || t.After(time.Now().Add(48*time.Hour)) {
		return
	}
	_ = os.Chtimes(path, t, t)
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// Inbox: what the computer sends to the phone.

func (s *Server) inbox(w http.ResponseWriter, r *http.Request, dev Device) {
	items := s.store.Outbox()
	if items == nil {
		items = []OutItem{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) inboxItem(w http.ResponseWriter, r *http.Request, dev Device) {
	it, ok := s.store.OutboxItem(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such item (already picked up?)")
		return
	}
	w.Header().Set("X-Toss-Kind", it.Kind)
	if it.Kind != "file" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, it.Text)
		return
	}
	f, err := os.Open(s.store.outboxFile(it.ID))
	if err != nil {
		writeError(w, http.StatusGone, "the file is gone from the computer")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", it.Mime)
	w.Header().Set("X-Toss-Name", url.PathEscape(it.Name))
	// ServeContent handles Range requests, so a big download can resume too.
	http.ServeContent(w, r, "", it.Created, f)
}

func (s *Server) inboxDelete(w http.ResponseWriter, r *http.Request, dev Device) {
	it, ok := s.store.OutboxItem(r.PathValue("id"))
	if !ok || !s.store.RemoveOut(it.ID) {
		writeError(w, http.StatusNotFound, "no such item")
		return
	}
	log.Printf("%s picked up %s", dev.Name, it.Label())
	s.notePickup(dev.Name, it.Label())
	writeJSON(w, http.StatusOK, map[string]string{})
}

// notePickup shows one "Hiha got …" notice for items picked up close together.
func (s *Server) notePickup(device, label string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.pickups[device]
	if p == nil {
		p = &pickup{}
		s.pickups[device] = p
	}
	p.labels = append(p.labels, label)
	if p.timer != nil {
		p.timer.Stop()
	}
	p.timer = time.AfterFunc(2*time.Second, func() {
		s.mu.Lock()
		cur := s.pickups[device]
		delete(s.pickups, device)
		s.mu.Unlock()
		if cur == nil {
			return
		}
		title := device + " got " + cur.labels[0]
		body := ""
		if len(cur.labels) > 1 {
			title = fmt.Sprintf("%s got %d items", device, len(cur.labels))
			body = truncate(strings.Join(cur.labels, ", "), 300)
		}
		s.notifier.Notify(Notice{Title: title, Body: body})
	})
}
