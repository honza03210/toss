package main

import (
	"bufio"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// tagged maps a notice tag to the ID notify-send printed for it, so a later notice with the
// same tag replaces it.
var (
	taggedMu sync.Mutex
	tagged   = map[string]string{}
)

func (SystemNotifier) Notify(n Notice) {
	if _, err := exec.LookPath("notify-send"); err != nil {
		LogNotifier{}.Notify(n)
		return
	}
	args := []string{"--app-name=Toss", "--expire-time=10000"}
	icon := iconPath()
	if n.Image != "" {
		icon = n.Image
	}
	if icon != "" {
		args = append(args, "--icon="+icon)
	}
	if n.Default != nil {
		args = append(args, "--action=default="+n.Default.Label)
	}
	for i, a := range n.Actions {
		args = append(args, "--action="+strconv.Itoa(i)+"="+a.Label)
	}
	if len(n.Actions) > 0 || n.Default != nil {
		args = append(args, "--wait")
	}
	if n.Tag != "" {
		taggedMu.Lock()
		if id := tagged[n.Tag]; id != "" {
			args = append(args, "--replace-id="+id)
		}
		taggedMu.Unlock()
		args = append(args, "--print-id")
	}
	// GNOME interprets a little markup in the body.
	args = append(args, "--", n.Title, markupEscape(n.Body))
	cmd := exec.Command("notify-send", args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("notify-send: %v", err)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("notify-send: %v", err)
		return
	}
	// --wait blocks until the notification is closed; it prints the chosen action's key.
	go func() {
		defer cmd.Wait()
		scanner := bufio.NewScanner(out)
		if n.Tag != "" && scanner.Scan() { // --print-id: the first line is the notice's ID
			taggedMu.Lock()
			tagged[n.Tag] = strings.TrimSpace(scanner.Text())
			taggedMu.Unlock()
		}
		for scanner.Scan() {
			key := strings.TrimSpace(scanner.Text())
			if key == "default" && n.Default != nil {
				run(*n.Default)
			} else if i, err := strconv.Atoi(key); err == nil && i >= 0 && i < len(n.Actions) {
				run(n.Actions[i])
			}
		}
	}()
}

func markupEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

func CopyText(text string) error {
	var cmd *exec.Cmd
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "" && has("wl-copy"):
		cmd = exec.Command("wl-copy")
	case has("xclip"):
		cmd = exec.Command("xclip", "-selection", "clipboard")
	case has("xsel"):
		cmd = exec.Command("xsel", "--clipboard", "--input")
	case has("wl-copy"):
		cmd = exec.Command("wl-copy")
	default:
		return fmt.Errorf("no clipboard tool found: install wl-clipboard (Wayland) or xclip (X11)")
	}
	// These tools fork to keep serving the clipboard; leaving stdout/stderr unset keeps
	// Run from waiting on that child.
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

// PasteText reads the clipboard's text.
func PasteText() (string, error) {
	var cmd *exec.Cmd
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "" && has("wl-paste"):
		cmd = exec.Command("wl-paste", "--no-newline", "--type", "text/plain;charset=utf-8")
	case has("xclip"):
		cmd = exec.Command("xclip", "-selection", "clipboard", "-o")
	case has("xsel"):
		cmd = exec.Command("xsel", "--clipboard", "--output")
	case has("wl-paste"):
		cmd = exec.Command("wl-paste", "--no-newline")
	default:
		return "", fmt.Errorf("no clipboard tool found: install wl-clipboard (Wayland) or xclip (X11)")
	}
	out, err := cmd.Output()
	if err != nil {
		// An empty clipboard makes these tools exit with an error: that's just no text.
		return "", nil
	}
	return string(out), nil
}

func has(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}

func OpenPath(target string) error {
	return startReaped(exec.Command("xdg-open", target))
}

// Browsers that take --new-window, by a word in their .desktop file name, with the commands
// that run them.
var newWindowBrowsers = []struct {
	match    string
	commands []string
}{
	{"firefox", []string{"firefox"}},
	{"librewolf", []string{"librewolf"}},
	{"google-chrome", []string{"google-chrome-stable", "google-chrome"}},
	{"chromium", []string{"chromium", "chromium-browser"}},
	{"brave", []string{"brave-browser", "brave"}},
	{"microsoft-edge", []string{"microsoft-edge-stable", "microsoft-edge"}},
	{"vivaldi", []string{"vivaldi-stable", "vivaldi"}},
}

// OpenURL opens a web link. A program running in the background can't bring a window to the
// front on GNOME/Wayland, so a link opened as a tab stays hidden behind other windows; a new
// window does appear in front. So known browsers get --new-window, others xdg-open.
func OpenURL(link string) error {
	if out, err := exec.Command("xdg-settings", "get", "default-web-browser").Output(); err == nil {
		desktop := strings.ToLower(strings.TrimSpace(string(out)))
		for _, b := range newWindowBrowsers {
			if !strings.Contains(desktop, b.match) {
				continue
			}
			for _, name := range b.commands {
				if path, err := exec.LookPath(name); err == nil {
					return startReaped(exec.Command(path, "--new-window", link))
				}
			}
		}
	}
	return OpenPath(link)
}

// startReaped starts a launcher and waits for it in the background, so it doesn't linger as a
// zombie, logging a failure with what it printed.
func startReaped(cmd *exec.Cmd) error {
	var stderr strings.Builder
	cmd.Stderr = &stderr
	cmd.Env = launchEnv()
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("%s: %v %s", filepath.Base(cmd.Path), err, truncate(strings.TrimSpace(stderr.String()), 300))
		}
	}()
	return nil
}

// launchEnv is this process's environment minus variables that describe whoever started Toss
// (a terminal app, an IDE…), which would make the desktop attribute the browser to that app.
func launchEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name == "GIO_LAUNCHED_DESKTOP_FILE" || name == "GIO_LAUNCHED_DESKTOP_FILE_PID" || name == "DESKTOP_STARTUP_ID" ||
			name == "XDG_ACTIVATION_TOKEN" || name == "CHROME_DESKTOP" || strings.HasPrefix(name, "ELECTRON_") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// ShowInFolder opens the file manager with the file selected (FileManager1 D-Bus API,
// which Nautilus, Dolphin and Nemo implement), else just opens the folder.
func ShowInFolder(path string) error {
	uri := (&url.URL{Scheme: "file", Path: path}).String()
	err := exec.Command("gdbus", "call", "--session", "--dest", "org.freedesktop.FileManager1",
		"--object-path", "/org/freedesktop/FileManager1", "--method", "org.freedesktop.FileManager1.ShowItems",
		"['"+strings.ReplaceAll(uri, "'", "%27")+"']", "").Run()
	if err != nil {
		return OpenPath(filepath.Dir(path))
	}
	return nil
}

func downloadsDir() string {
	if out, err := exec.Command("xdg-user-dir", "DOWNLOAD").Output(); err == nil {
		if dir := strings.TrimSpace(string(out)); dir != "" {
			if home, _ := os.UserHomeDir(); dir != home {
				return dir
			}
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

func enableTerminalColors() {}

// startDetached runs `toss serve` in the background, outliving this process.
func startDetached(exe string) error {
	cmd := exec.Command(exe, "serve")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

func autostartFile() string {
	dir, _ := os.UserConfigDir()
	return filepath.Join(dir, "autostart", "toss.desktop")
}

// installAutostart starts Toss with the desktop session (XDG autostart works on GNOME, KDE,
// Xfce… and runs inside the session, so notifications and the clipboard work).
func installAutostart(exe string) error {
	file := autostartFile()
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	entry := fmt.Sprintf(`[Desktop Entry]
Type=Application
Name=Toss
Comment=Receive things shared from your iPhone
Exec="%s" serve
Icon=%s
Terminal=false
X-GNOME-Autostart-enabled=true
NoDisplay=true
`, exe, iconPath())
	return os.WriteFile(file, []byte(entry), 0o644)
}

func removeAutostart() error {
	err := os.Remove(autostartFile())
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func autostartInstalled() bool {
	_, err := os.Stat(autostartFile())
	return err == nil
}

// stopRunning stops a background `toss serve` (best effort).
func stopRunning() {
	_ = exec.Command("pkill", "-f", "toss serve").Run()
}

func firewallHint(port int) string {
	return fmt.Sprintf("If the phone can't connect and you use a firewall: sudo ufw allow %d/tcp", port)
}
