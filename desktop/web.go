package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// The local web page (`toss web`, the tray menu on Windows): what was received, what waits
// for the phone, and pairing with a QR code, without a terminal. It listens on 127.0.0.1 only,
// one port above the phone port. Every API call needs the token from web-token (in the config
// folder), which `toss web` passes in the URL fragment; the page sends it as a header, so other
// websites can't call the API (a custom header needs a CORS preflight, which is refused), and
// the Host check stops DNS rebinding.

//go:embed web/index.html
var webPage []byte

func webPort(cfg Config) int { return cfg.Port + 1 }

// WebToken returns the local page's token, creating it the first time.
func (s *Store) WebToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.path("web-token")
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) >= 20 {
		return strings.TrimSpace(string(data)), nil
	}
	token := randomToken(24)
	return token, os.WriteFile(path, []byte(token+"\n"), 0o600)
}

// WebURL is the address `toss web` opens.
func WebURL(cfg Config, token string) string {
	return fmt.Sprintf("http://127.0.0.1:%d/#%s", webPort(cfg), token)
}

type webServer struct {
	store *Store
	port  int
	token string
}

func (s *Server) ListenAndServeWeb(ctx context.Context, port int) error {
	token, err := s.store.WebToken()
	if err != nil {
		return err
	}
	ws := &webServer{store: s.store, port: port, token: token}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: ws.handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (ws *webServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; img-src 'self' data: blob:")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		_, _ = w.Write(webPage)
	})
	mux.HandleFunc("GET /api/state", ws.api(ws.state))
	mux.HandleFunc("POST /api/send/text", ws.api(ws.sendText))
	mux.HandleFunc("POST /api/send/file", ws.api(ws.sendFile))
	mux.HandleFunc("DELETE /api/outbox/{id}", ws.api(ws.cancel))
	mux.HandleFunc("POST /api/open", ws.api(ws.open))
	mux.HandleFunc("POST /api/show", ws.api(ws.show))
	mux.HandleFunc("GET /api/file", ws.api(ws.file))
	mux.HandleFunc("POST /api/pair", ws.api(ws.pair))
	mux.HandleFunc("DELETE /api/history", ws.api(ws.clearHistory))
	mux.HandleFunc("POST /api/folder", ws.api(ws.folder))
	return ws.guard(mux)
}

// guard refuses requests whose Host isn't this machine (DNS rebinding).
func (ws *webServer) guard(next http.Handler) http.Handler {
	allowed := map[string]bool{
		"127.0.0.1:" + strconv.Itoa(ws.port): true,
		"localhost:" + strconv.Itoa(ws.port): true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (ws *webServer) api(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !constantTimeEqual(r.Header.Get("X-Toss-Web"), ws.token) {
			writeError(w, http.StatusUnauthorized, "open this page with `toss web`")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next(w, r)
	}
}

func (ws *webServer) state(w http.ResponseWriter, r *http.Request) {
	cfg, _ := ws.store.Config()
	type device struct {
		Name     string    `json:"name"`
		Paired   time.Time `json:"paired"`
		LastSeen time.Time `json:"lastSeen"`
	}
	var devices []device
	for _, d := range ws.store.Devices() {
		devices = append(devices, device{d.Name, d.Paired, d.LastSeen})
	}
	history := ws.store.History()
	if len(history) > 100 {
		history = history[:100]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"computerName": cfg.ComputerName,
		"version":      version,
		"saveDir":      saveDir(cfg),
		"devices":      orEmpty(devices),
		"history":      orEmpty(history),
		"outbox":       orEmpty(ws.store.Outbox()),
	})
}

func orEmpty[T any](list []T) []T {
	if list == nil {
		return []T{}
	}
	return list
}

func (ws *webServer) sendText(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxTextBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "couldn't read the text")
		return
	}
	it, err := ws.store.AddOutText(string(body))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (ws *webServer) sendFile(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "file"
	}
	it, err := ws.store.AddOutReader(name, r.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, it)
}

func (ws *webServer) cancel(w http.ResponseWriter, r *http.Request) {
	if !ws.store.RemoveOut(r.PathValue("id")) {
		writeError(w, http.StatusNotFound, "no such item")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{})
}

// receivedPath reads {"path": …} and accepts it only if it's a file Toss received, so the
// page can't be used to open arbitrary files.
func (ws *webServer) receivedPath(w http.ResponseWriter, path string) (string, bool) {
	for _, e := range ws.store.History() {
		if e.Path != "" && e.Path == path {
			if _, err := os.Stat(path); err != nil {
				writeError(w, http.StatusGone, "the file was moved or deleted")
				return "", false
			}
			return path, true
		}
	}
	writeError(w, http.StatusForbidden, "not a received file")
	return "", false
}

func readPath(r *http.Request) string {
	var req struct {
		Path string `json:"path"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 8192)).Decode(&req)
	return req.Path
}

func (ws *webServer) open(w http.ResponseWriter, r *http.Request) {
	if path, ok := ws.receivedPath(w, readPath(r)); ok {
		replyErr(w, OpenPath(path))
	}
}

func (ws *webServer) show(w http.ResponseWriter, r *http.Request) {
	if path, ok := ws.receivedPath(w, readPath(r)); ok {
		replyErr(w, ShowInFolder(path))
	}
}

func (ws *webServer) folder(w http.ResponseWriter, r *http.Request) {
	cfg, _ := ws.store.Config()
	dir := saveDir(cfg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		replyErr(w, err)
		return
	}
	replyErr(w, OpenPath(dir))
}

func replyErr(w http.ResponseWriter, err error) {
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{})
}

// file serves a received image for the page's thumbnails.
func (ws *webServer) file(w http.ResponseWriter, r *http.Request) {
	path, ok := ws.receivedPath(w, r.URL.Query().Get("path"))
	if !ok {
		return
	}
	if !isImageName(path) {
		writeError(w, http.StatusBadRequest, "not an image")
		return
	}
	http.ServeFile(w, r, path)
}

func (ws *webServer) clearHistory(w http.ResponseWriter, r *http.Request) {
	replyErr(w, ws.store.ClearHistory())
}

// pair makes a pairing code like `toss pair` does and returns it as an SVG QR code.
func (ws *webServer) pair(w http.ResponseWriter, r *http.Request) {
	cfg, err := ws.store.Config()
	if err != nil {
		replyErr(w, err)
		return
	}
	cert, err := ws.store.Certificate(cfg.ComputerName)
	if err != nil {
		replyErr(w, err)
		return
	}
	pending, err := ws.store.NewPairing(10 * time.Minute)
	if err != nil {
		replyErr(w, err)
		return
	}
	link := PairingLink(cfg.ComputerName, pairingHosts(), cfg.Port, Fingerprint(cert), pending.Token)
	code, err := EncodeQR([]byte(link), eccM)
	if err != nil {
		code, err = EncodeQR([]byte(link), eccL)
	}
	if err != nil {
		replyErr(w, err)
		return
	}
	log.Printf("pairing code shown on the local page")
	writeJSON(w, http.StatusOK, map[string]any{"link": link, "svg": code.SVG(), "expires": pending.Expires})
}

// SVG draws the code with a 4-module quiet zone.
func (q *QRCode) SVG() string {
	const quiet = 4
	n := q.Size + 2*quiet
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" shape-rendering="crispEdges">`, n, n)
	fmt.Fprintf(&b, `<rect width="%d" height="%d" fill="#fff"/><path fill="#000" d="`, n, n)
	for y := 0; y < q.Size; y++ {
		for x := 0; x < q.Size; x++ {
			if q.Dark(x, y) {
				fmt.Fprintf(&b, "M%d %dh1v1h-1z", x+quiet, y+quiet)
			}
		}
	}
	b.WriteString(`"/></svg>`)
	return b.String()
}
