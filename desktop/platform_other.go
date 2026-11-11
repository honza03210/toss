//go:build !linux && !windows && !darwin

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// Other systems (BSD…) get the basics: logged notices, `open` for files.

func (SystemNotifier) Notify(n Notice) { LogNotifier{}.Notify(n) }

func PasteText() (string, error)        { return "", errors.New("clipboard not supported on this system") }
func CopyText(text string) error        { return errors.New("clipboard not supported on this system") }
func OpenPath(target string) error      { return exec.Command("open", target).Start() }
func OpenURL(link string) error         { return OpenPath(link) }
func ShowInFolder(path string) error    { return exec.Command("open", "-R", path).Start() }
func enableTerminalColors()             {}
func startDetached(exe string) error    { return exec.Command(exe, "serve").Start() }
func installAutostart(exe string) error { return errors.New("autostart not supported on this system") }
func removeAutostart() error            { return nil }
func autostartInstalled() bool          { return false }
func stopRunning()                      {}
func firewallHint(port int) string      { return "" }

func downloadsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func installSendTo(exe string) (string, error)                { return "", nil }
func removeSendTo()                                           {}
func startTray(ctx context.Context, store *Store, cfg Config) {}
