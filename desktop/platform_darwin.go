package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// macOS: notifications through terminal-notifier when it's installed (clickable, replaceable),
// else AppleScript's `display notification`; pbcopy/pbpaste; a LaunchAgent for login.

func (SystemNotifier) Notify(n Notice) {
	if path, err := exec.LookPath("terminal-notifier"); err == nil {
		args := []string{"-title", "Toss", "-subtitle", n.Title, "-message", nonEmpty(n.Body, " ")}
		if n.Tag != "" {
			args = append(args, "-group", n.Tag)
		}
		if n.Default != nil {
			target := n.Default.Target
			if n.Default.Kind == ActionShowFolder {
				target = filepath.Dir(target)
			}
			if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
				target = "file://" + target
			}
			if n.Default.Kind != ActionCopy {
				args = append(args, "-open", target)
			}
		}
		if n.Image != "" {
			args = append(args, "-contentImage", n.Image)
		}
		_ = startQuiet(exec.Command(path, args...))
		return
	}
	script := []string{"-e", "on run argv", "-e", "display notification (item 2 of argv) with title \"Toss\" subtitle (item 1 of argv)", "-e", "end run"}
	_ = startQuiet(exec.Command("osascript", append(script, n.Title, nonEmpty(n.Body, " "))...))
}

func nonEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// startQuiet starts a helper and reaps it in the background.
func startQuiet(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}

func CopyText(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func PasteText() (string, error) {
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		return "", nil
	}
	return string(out), nil
}

func OpenPath(target string) error                            { return startQuiet(exec.Command("open", target)) }
func OpenURL(link string) error                               { return OpenPath(link) }
func ShowInFolder(path string) error                          { return startQuiet(exec.Command("open", "-R", path)) }
func enableTerminalColors()                                   {}
func installSendTo(exe string) (string, error)                { return "", nil }
func removeSendTo()                                           {}
func startTray(ctx context.Context, store *Store, cfg Config) {}

func downloadsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func startDetached(exe string) error {
	cmd := exec.Command(exe, "serve")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func launchAgent() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", "com.toss.companion.plist")
}

// installAutostart writes a LaunchAgent, which starts Toss at the next login (`toss install`
// starts it now itself).
func installAutostart(exe string) error {
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.toss.companion</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>serve</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>ProcessType</key>
    <string>Interactive</string>
</dict>
</plist>
`, xmlEscapeText(exe))
	if err := os.MkdirAll(filepath.Dir(launchAgent()), 0o755); err != nil {
		return err
	}
	return os.WriteFile(launchAgent(), []byte(plist), 0o644)
}

func xmlEscapeText(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func removeAutostart() error {
	err := os.Remove(launchAgent())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func autostartInstalled() bool {
	_, err := os.Stat(launchAgent())
	return err == nil
}

func stopRunning() {
	_ = exec.Command("pkill", "-f", "toss serve").Run()
}

func firewallHint(port int) string {
	return "If the phone can't connect: allow incoming connections for toss when macOS asks, or in System Settings → Network → Firewall."
}
