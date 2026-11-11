package main

import (
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

// The outbox holds what the computer sends to the phone (`toss send`, the file manager's
// "Send to iPhone", the local web page). iOS doesn't let an app receive in the background
// without Apple's push service, so items wait here and the Toss app picks them up when it
// opens (or a Shortcut asks for them). They expire after outboxTTL.
const outboxTTL = 7 * 24 * time.Hour

// OutItem is one item waiting for the phone (PROTOCOL.md, GET /v1/inbox).
type OutItem struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"` // text, url, file
	Text    string    `json:"text,omitempty"`
	Name    string    `json:"name,omitempty"`
	Size    int64     `json:"size"`
	Mime    string    `json:"mime,omitempty"`
	Created time.Time `json:"created"`
}

func (s *Store) outboxDir() string { return s.path("outbox") }

func (s *Store) outboxFile(id string) string { return filepath.Join(s.outboxDir(), id) }

// Outbox lists waiting items, oldest first, dropping expired ones.
func (s *Store) Outbox() []OutItem {
	var keep []OutItem
	_ = s.updateOutbox(func(list []OutItem) []OutItem {
		keep = nil
		for _, it := range list {
			if time.Since(it.Created) > outboxTTL {
				os.Remove(s.outboxFile(it.ID))
				continue
			}
			keep = append(keep, it)
		}
		return keep
	})
	return keep
}

func (s *Store) updateOutbox(change func([]OutItem) []OutItem) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []OutItem
	_ = s.readJSON("outbox.json", &list)
	return s.writeJSON("outbox.json", change(list))
}

// OutboxItem finds a waiting item by ID.
func (s *Store) OutboxItem(id string) (OutItem, bool) {
	for _, it := range s.Outbox() {
		if it.ID == id {
			return it, true
		}
	}
	return OutItem{}, false
}

// AddOutText queues text, or a link when it's a single web address.
func (s *Store) AddOutText(text string) (OutItem, error) {
	if strings.TrimSpace(text) == "" {
		return OutItem{}, errors.New("nothing to send: the text is empty")
	}
	if len(text) > maxTextBytes {
		return OutItem{}, errors.New("text too long")
	}
	it := OutItem{ID: randomToken(12), Kind: "text", Text: text, Size: int64(len(text)), Created: time.Now()}
	if isWebLink(text) {
		it.Kind, it.Text = "url", strings.TrimSpace(text)
	}
	return it, s.updateOutbox(func(list []OutItem) []OutItem { return append(list, it) })
}

// AddOutFile queues a copy of a file (a copy, so moving or editing the original afterwards
// doesn't change what the phone gets).
func (s *Store) AddOutFile(path string) (OutItem, error) {
	info, err := os.Stat(path)
	if err != nil {
		return OutItem{}, err
	}
	if info.IsDir() {
		return OutItem{}, fmt.Errorf("%s is a folder: only files can be sent", filepath.Base(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return OutItem{}, err
	}
	defer f.Close()
	return s.AddOutReader(filepath.Base(path), f)
}

// AddOutReader queues a file read from r (uploads from the local web page).
func (s *Store) AddOutReader(name string, r io.Reader) (OutItem, error) {
	if err := os.MkdirAll(s.outboxDir(), 0o700); err != nil {
		return OutItem{}, err
	}
	it := OutItem{ID: randomToken(12), Kind: "file", Name: safeFileName(name), Mime: mimeForName(name), Created: time.Now()}
	dst, err := os.OpenFile(s.outboxFile(it.ID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return OutItem{}, err
	}
	n, err := io.Copy(dst, r)
	if cerr := dst.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(dst.Name())
		return OutItem{}, err
	}
	it.Size = n
	return it, s.updateOutbox(func(list []OutItem) []OutItem { return append(list, it) })
}

// RemoveOut deletes a waiting item (after the phone got it, or when cancelled).
func (s *Store) RemoveOut(id string) bool {
	found := false
	_ = s.updateOutbox(func(list []OutItem) []OutItem {
		var keep []OutItem
		for _, it := range list {
			if it.ID == id {
				found = true
				continue
			}
			keep = append(keep, it)
		}
		return keep
	})
	if found {
		os.Remove(s.outboxFile(id))
	}
	return found
}

func isWebLink(text string) bool {
	t := strings.TrimSpace(text)
	if strings.ContainsAny(t, " \t\n") {
		return false
	}
	u, err := url.Parse(t)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (it OutItem) Label() string {
	if it.Kind == "file" {
		return it.Name
	}
	return truncate(strings.ReplaceAll(it.Text, "\n", " "), 60)
}

func mimeForName(name string) string {
	types := map[string]string{
		".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif", ".webp": "image/webp",
		".heic": "image/heic", ".pdf": "application/pdf", ".mp4": "video/mp4", ".mov": "video/quicktime",
		".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".txt": "text/plain", ".zip": "application/zip",
	}
	if m, ok := types[strings.ToLower(filepath.Ext(name))]; ok {
		return m
	}
	return "application/octet-stream"
}

// HistoryEntry is something received, kept so it can be found again after the notification
// is gone (`toss history`, the local web page).
type HistoryEntry struct {
	Time   time.Time `json:"time"`
	Device string    `json:"device"`
	Kind   string    `json:"kind"` // text, url, file
	Text   string    `json:"text,omitempty"`
	Path   string    `json:"path,omitempty"`
	Size   int64     `json:"size,omitempty"`
}

const historyLimit = 200

// maxHistoryText keeps history.json small; longer text is cut (the clipboard had the whole of it).
const maxHistoryText = 20 << 10

func (s *Store) AddHistory(e HistoryEntry) {
	if len(e.Text) > maxHistoryText {
		e.Text = truncateBytes(e.Text, maxHistoryText) + "…"
	}
	if !utf8.ValidString(e.Text) {
		e.Text = strings.ToValidUTF8(e.Text, "�")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []HistoryEntry
	_ = s.readJSON("history.json", &list)
	list = append(list, e)
	if len(list) > historyLimit {
		list = list[len(list)-historyLimit:]
	}
	_ = s.writeJSON("history.json", list)
}

// History returns received items, newest first.
func (s *Store) History() []HistoryEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []HistoryEntry
	_ = s.readJSON("history.json", &list)
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list
}

func (s *Store) ClearHistory() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSON("history.json", []HistoryEntry{})
}
