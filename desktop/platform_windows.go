package main

import (
	"encoding/base64"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"
)

const aumid = "Toss.Companion"

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")
)

// Notify shows a Windows toast through PowerShell's WinRT bridge. Buttons open files,
// folders or links via protocol activation, so no COM activator is needed.
func (SystemNotifier) Notify(n Notice) {
	registerAUMID()
	var b strings.Builder
	launch := ""
	if n.Default != nil {
		launch = actionURI(*n.Default)
	}
	b.WriteString(`<toast activationType="protocol" launch="` + xmlEscape(launch) + `"><visual><binding template="ToastGeneric">`)
	b.WriteString("<text>" + xmlEscape(n.Title) + "</text><text>" + xmlEscape(n.Body) + "</text>")
	if n.Image != "" {
		b.WriteString(`<image placement="hero" src="` + xmlEscape(fileURI(n.Image)) + `"/>`)
	}
	b.WriteString("</binding></visual>")
	var actions []string
	for _, a := range n.Actions {
		if uri := actionURI(a); uri != "" {
			actions = append(actions, `<action content="`+xmlEscape(a.Label)+`" activationType="protocol" arguments="`+xmlEscape(uri)+`"/>`)
		}
	}
	if len(actions) > 0 {
		b.WriteString("<actions>" + strings.Join(actions, "") + "</actions>")
	}
	b.WriteString("</toast>")

	script := `$ErrorActionPreference = 'Stop'
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
[Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime] | Out-Null
$xml = New-Object Windows.Data.Xml.Dom.XmlDocument
$xml.LoadXml([System.Text.Encoding]::UTF8.GetString([System.Convert]::FromBase64String('` +
		base64.StdEncoding.EncodeToString([]byte(b.String())) + `')))
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
` + toastTag(n.Tag) + `[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('` + aumid + `').Show($toast)
`
	if err := hiddenCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script)).Start(); err != nil {
		log.Printf("toast: %v", err)
	}
}

// toastTag makes a toast replace an earlier one with the same tag.
func toastTag(tag string) string {
	if tag == "" {
		return ""
	}
	sum := hashKey(tag)[:16] // tags are at most 64 characters and go into a PowerShell string
	return "$toast.Tag = '" + sum + "'\n"
}

// actionURI turns an action into something a toast button can launch. Copying isn't
// possible from a toast button, so Copy buttons are left out (text is copied on arrival
// when autoCopy is on).
func actionURI(a Action) string {
	switch a.Kind {
	case ActionOpen:
		if strings.HasPrefix(a.Target, "http://") || strings.HasPrefix(a.Target, "https://") {
			return a.Target
		}
		return fileURI(a.Target)
	case ActionShowFolder:
		return fileURI(filepath.Dir(a.Target))
	}
	return ""
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: "/" + filepath.ToSlash(path)}).String()
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;").Replace(s)
}

func encodePowerShell(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, len(u)*2)
	for i, v := range u {
		b[2*i], b[2*i+1] = byte(v), byte(v>>8)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func hiddenCommand(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	return cmd
}

var aumidRegistered bool

// registerAUMID lets an unpackaged app show toasts under its own name and icon
// (HKCU\Software\Classes\AppUserModelId\<AUMID>).
func registerAUMID() {
	if aumidRegistered {
		return
	}
	aumidRegistered = true
	key := `HKCU\Software\Classes\AppUserModelId\` + aumid
	_ = hiddenCommand("reg.exe", "add", key, "/v", "DisplayName", "/t", "REG_SZ", "/d", "Toss", "/f").Run()
	if icon := iconPath(); icon != "" {
		_ = hiddenCommand("reg.exe", "add", key, "/v", "IconUri", "/t", "REG_SZ", "/d", icon, "/f").Run()
	}
}

func CopyText(text string) error {
	openClipboard := user32.NewProc("OpenClipboard")
	var ok uintptr
	for i := 0; i < 10; i++ {
		if ok, _, _ = openClipboard.Call(0); ok != 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if ok == 0 {
		return fmt.Errorf("clipboard is busy")
	}
	defer user32.NewProc("CloseClipboard").Call()
	user32.NewProc("EmptyClipboard").Call()
	u, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	size := uintptr(len(u) * 2)
	const gmemMoveable = 0x0002
	h, _, err := kernel32.NewProc("GlobalAlloc").Call(gmemMoveable, size)
	if h == 0 {
		return err
	}
	p, _, err := kernel32.NewProc("GlobalLock").Call(h)
	if p == 0 {
		return err
	}
	kernel32.NewProc("RtlMoveMemory").Call(p, uintptr(unsafe.Pointer(&u[0])), size)
	kernel32.NewProc("GlobalUnlock").Call(h)
	const cfUnicodeText = 13
	if r, _, err := user32.NewProc("SetClipboardData").Call(cfUnicodeText, h); r == 0 {
		return err
	}
	return nil
}

// PasteText reads the clipboard's text (CF_UNICODETEXT).
func PasteText() (string, error) {
	openClipboard := user32.NewProc("OpenClipboard")
	var ok uintptr
	for i := 0; i < 10; i++ {
		if ok, _, _ = openClipboard.Call(0); ok != 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if ok == 0 {
		return "", fmt.Errorf("clipboard is busy")
	}
	defer user32.NewProc("CloseClipboard").Call()
	const cfUnicodeText = 13
	h, _, _ := user32.NewProc("GetClipboardData").Call(cfUnicodeText)
	if h == 0 {
		return "", nil // no text on the clipboard
	}
	size, _, _ := kernel32.NewProc("GlobalSize").Call(h)
	p, _, _ := kernel32.NewProc("GlobalLock").Call(h)
	if p == 0 || size < 2 {
		return "", nil
	}
	defer kernel32.NewProc("GlobalUnlock").Call(h)
	buf := make([]uint16, size/2)
	kernel32.NewProc("RtlMoveMemory").Call(uintptr(unsafe.Pointer(&buf[0])), p, size)
	return syscall.UTF16ToString(buf), nil
}

func shellExecute(verb, file, args string) error {
	v, _ := syscall.UTF16PtrFromString(verb)
	f, _ := syscall.UTF16PtrFromString(file)
	var a *uint16
	if args != "" {
		a, _ = syscall.UTF16PtrFromString(args)
	}
	r, _, _ := shell32.NewProc("ShellExecuteW").Call(0, uintptr(unsafe.Pointer(v)), uintptr(unsafe.Pointer(f)), uintptr(unsafe.Pointer(a)), 0, 1)
	if r <= 32 {
		return fmt.Errorf("ShellExecute failed (%d)", r)
	}
	return nil
}

func OpenPath(target string) error {
	return shellExecute("open", target, "")
}

// OpenURL opens a web link with the default browser.
func OpenURL(link string) error { return OpenPath(link) }

func ShowInFolder(path string) error {
	return shellExecute("open", "explorer.exe", `/select,"`+path+`"`)
}

func downloadsDir() string {
	// FOLDERID_Downloads {374DE290-123F-4565-9164-39C4925E467B}
	guid := [16]byte{0x90, 0xE2, 0x4D, 0x37, 0x3F, 0x12, 0x65, 0x45, 0x91, 0x64, 0x39, 0xC4, 0x92, 0x5E, 0x46, 0x7B}
	var out *uint16
	r, _, _ := shell32.NewProc("SHGetKnownFolderPath").Call(uintptr(unsafe.Pointer(&guid[0])), 0, 0, uintptr(unsafe.Pointer(&out)))
	if r == 0 && out != nil {
		defer ole32.NewProc("CoTaskMemFree").Call(uintptr(unsafe.Pointer(out)))
		return syscall.UTF16ToString(unsafe.Slice(out, 32768))
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Downloads")
}

// enableTerminalColors turns on ANSI escape handling in classic consoles (for the QR code).
func enableTerminalColors() {
	h, err := syscall.GetStdHandle(syscall.STD_OUTPUT_HANDLE)
	if err != nil {
		return
	}
	var mode uint32
	if syscall.GetConsoleMode(h, &mode) == nil {
		kernel32.NewProc("SetConsoleMode").Call(uintptr(h), uintptr(mode|0x0004)) // ENABLE_VIRTUAL_TERMINAL_PROCESSING
	}
}

// backgroundExe is tossw.exe next to toss.exe: the same program built without a console
// window, for running in the background.
func backgroundExe(exe string) string {
	w := filepath.Join(filepath.Dir(exe), "tossw.exe")
	if _, err := os.Stat(w); err == nil {
		return w
	}
	return exe
}

func startDetached(exe string) error {
	cmd := exec.Command(backgroundExe(exe), "serve")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008 | 0x00000200} // DETACHED_PROCESS | CREATE_NEW_PROCESS_GROUP
	return cmd.Start()
}

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

func installAutostart(exe string) error {
	return hiddenCommand("reg.exe", "add", runKey, "/v", "Toss", "/t", "REG_SZ", "/d", `"`+backgroundExe(exe)+`" serve`, "/f").Run()
}

func removeAutostart() error {
	_ = hiddenCommand("reg.exe", "delete", runKey, "/v", "Toss", "/f").Run()
	return nil
}

func autostartInstalled() bool {
	return hiddenCommand("reg.exe", "query", runKey, "/v", "Toss").Run() == nil
}

func stopRunning() {
	_ = hiddenCommand("taskkill.exe", "/IM", "tossw.exe", "/F").Run()
}

func firewallHint(port int) string {
	return "If the phone can't connect: allow Toss through Windows Defender Firewall for private networks (install.ps1 offers to add the rule)."
}
