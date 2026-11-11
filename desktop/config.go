package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const defaultPort = 47800

// Config is stored as config.json in the user's config folder
// (~/.config/toss on Linux, %AppData%\toss on Windows).
type Config struct {
	ComputerID   string `json:"computerId"`
	ComputerName string `json:"computerName"`
	Port         int    `json:"port"`
	// Folder for received files; empty means <Downloads>/Toss.
	SaveDir string `json:"saveDir,omitempty"`
	// Copy received text and links to the clipboard right away.
	AutoCopy bool `json:"autoCopy"`
	// Open received web links in the browser right away. Unset (older configs) means on.
	AutoOpen *bool `json:"autoOpen,omitempty"`
}

// OpensLinks reports whether received links open by themselves.
func (c Config) OpensLinks() bool { return c.AutoOpen == nil || *c.AutoOpen }

type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	KeyHash  string    `json:"keyHash"` // hex SHA-256 of the device key
	Paired   time.Time `json:"paired"`
	LastSeen time.Time `json:"lastSeen,omitempty"`
}

// PendingPairing is a token `toss pair` created; the server accepts it once.
type PendingPairing struct {
	Token   string    `json:"token"`
	Expires time.Time `json:"expires"`
}

// Store reads and writes the files in the config folder. The server and a `toss pair`
// process may run at the same time, so everything is re-read from disk on use.
type Store struct {
	Dir string
	mu  sync.Mutex
}

func defaultConfigDir() (string, error) {
	if dir := os.Getenv("TOSS_CONFIG_DIR"); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "toss"), nil
}

func OpenStore() (*Store, error) {
	dir, err := defaultConfigDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{Dir: dir}, nil
}

func (s *Store) path(name string) string { return filepath.Join(s.Dir, name) }

func (s *Store) readJSON(name string, v any) error {
	data, err := os.ReadFile(s.path(name))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// writeJSON writes atomically (temp file + rename) and readable only by the user.
func (s *Store) writeJSON(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(name + ".tmp")
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(name))
}

// Config loads config.json, creating it with defaults the first time.
func (s *Store) Config() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var c Config
	err := s.readJSON("config.json", &c)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return c, fmt.Errorf("reading config.json: %w", err)
	}
	changed := false
	if c.ComputerID == "" {
		c.ComputerID = newUUID()
		changed = true
	}
	if c.ComputerName == "" {
		c.ComputerName, _ = os.Hostname()
		if c.ComputerName == "" {
			c.ComputerName = "Computer"
		}
		changed = true
	}
	if c.Port == 0 {
		c.Port = defaultPort
		changed = true
	}
	if errors.Is(err, os.ErrNotExist) {
		c.AutoCopy = true
	}
	if changed {
		if err := s.writeJSON("config.json", c); err != nil {
			return c, err
		}
	}
	return c, nil
}

func (s *Store) SaveConfig(c Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSON("config.json", c)
}

func (s *Store) Devices() []Device {
	s.mu.Lock()
	defer s.mu.Unlock()
	var d []Device
	_ = s.readJSON("devices.json", &d)
	return d
}

func (s *Store) updateDevices(change func([]Device) []Device) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var d []Device
	_ = s.readJSON("devices.json", &d)
	return s.writeJSON("devices.json", change(d))
}

// AddDevice stores a newly paired device (replacing an older pairing with the same ID).
func (s *Store) AddDevice(dev Device) error {
	return s.updateDevices(func(list []Device) []Device {
		out := []Device{dev}
		for _, d := range list {
			if d.ID != dev.ID {
				out = append(out, d)
			}
		}
		return out
	})
}

// RemoveDevice unpairs devices whose name or ID matches. Returns how many.
func (s *Store) RemoveDevice(nameOrID string) (int, error) {
	removed := 0
	err := s.updateDevices(func(list []Device) []Device {
		var out []Device
		for _, d := range list {
			if d.ID == nameOrID || d.Name == nameOrID {
				removed++
			} else {
				out = append(out, d)
			}
		}
		return out
	})
	return removed, err
}

// DeviceForKey finds the device a bearer key belongs to and records that it was seen.
func (s *Store) DeviceForKey(key string) (Device, bool) {
	hash := hashKey(key)
	var found Device
	ok := false
	_ = s.updateDevices(func(list []Device) []Device {
		for i := range list {
			if list[i].KeyHash == hash {
				list[i].LastSeen = time.Now()
				found, ok = list[i], true
			}
		}
		return list
	})
	return found, ok
}

// NewPairing creates a one-time token valid for `ttl`.
func (s *Store) NewPairing(ttl time.Duration) (PendingPairing, error) {
	p := PendingPairing{Token: randomToken(16), Expires: time.Now().Add(ttl)}
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []PendingPairing
	_ = s.readJSON("pairing.json", &list)
	var keep []PendingPairing
	for _, old := range list {
		if time.Now().Before(old.Expires) {
			keep = append(keep, old)
		}
	}
	return p, s.writeJSON("pairing.json", append(keep, p))
}

// TakePairing consumes `token` if it is pending and not expired.
func (s *Store) TakePairing(token string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []PendingPairing
	_ = s.readJSON("pairing.json", &list)
	ok := false
	var keep []PendingPairing
	for _, p := range list {
		if !time.Now().Before(p.Expires) {
			continue
		}
		if !ok && constantTimeEqual(p.Token, token) {
			ok = true
			continue
		}
		keep = append(keep, p)
	}
	_ = s.writeJSON("pairing.json", keep)
	return ok
}

// CancelPairing drops a token, e.g. when `toss pair` is interrupted.
func (s *Store) CancelPairing(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []PendingPairing
	_ = s.readJSON("pairing.json", &list)
	var keep []PendingPairing
	for _, p := range list {
		if p.Token != token {
			keep = append(keep, p)
		}
	}
	_ = s.writeJSON("pairing.json", keep)
}

func randomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func newUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
