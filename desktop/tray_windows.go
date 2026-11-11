package main

import (
	"context"
	"encoding/binary"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

// The notification-area icon: click for the Toss page, right-click for a menu. Plain Win32
// calls, so no extra libraries.

const (
	wmDestroy     = 0x0002
	wmClose       = 0x0010
	wmNull        = 0x0000
	wmCommand     = 0x0111
	wmApp         = 0x8000
	wmTrayIcon    = wmApp + 1
	wmLButtonUp   = 0x0202
	wmRButtonUp   = 0x0205
	wmContextMenu = 0x007B

	nimAdd    = 0
	nimDelete = 2
	nifMsg    = 0x1
	nifIcon   = 0x2
	nifTip    = 0x4

	mfString    = 0x0
	mfSeparator = 0x800
	tpmReturn   = 0x0100
	tpmNoNotify = 0x0080
	tpmRight    = 0x0002
)

// Tray menu commands (0 means the menu was dismissed).
const (
	menuOpen = 1 + iota
	menuPair
	menuFolder
	menuQuit
)

type notifyIconData struct {
	Size            uint32
	Wnd             uintptr
	ID              uint32
	Flags           uint32
	CallbackMessage uint32
	Icon            uintptr
	Tip             [128]uint16
	State           uint32
	StateMask       uint32
	Info            [256]uint16
	Version         uint32
	InfoTitle       [64]uint16
	InfoFlags       uint32
	GUID            [16]byte
	BalloonIcon     uintptr
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type winMsg struct {
	Wnd     uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      [2]int32
	Private uint32
}

type trayIcon struct {
	store          *Store
	cfg            Config
	wnd            uintptr
	data           notifyIconData
	taskbarCreated uintptr
}

func startTray(ctx context.Context, store *Store, cfg Config) {
	t := &trayIcon{store: store, cfg: cfg}
	ready := make(chan bool, 1)
	go t.run(ready)
	if !<-ready {
		return
	}
	go func() {
		<-ctx.Done()
		user32.NewProc("PostMessageW").Call(t.wnd, wmClose, 0, 0)
	}()
}

func (t *trayIcon) run(ready chan<- bool) {
	// A window and its messages belong to the thread that made it.
	runtime.LockOSThread()
	instance, _, _ := kernel32.NewProc("GetModuleHandleW").Call(0)
	className, _ := syscall.UTF16PtrFromString("TossTray")
	wc := wndClassEx{WndProc: syscall.NewCallback(t.wndProc), Instance: instance, ClassName: className}
	wc.Size = uint32(unsafe.Sizeof(wc))
	if r, _, err := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		log.Printf("tray: RegisterClassEx: %v", err)
		ready <- false
		return
	}
	title, _ := syscall.UTF16PtrFromString("Toss")
	// An ordinary (never shown) window rather than a message-only one: only top-level windows
	// get the TaskbarCreated broadcast that says to re-add the icon after Explorer restarts.
	wnd, _, err := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(className)), uintptr(unsafe.Pointer(title)),
		0, 0, 0, 0, 0, 0, 0, instance, 0)
	if wnd == 0 {
		log.Printf("tray: CreateWindowEx: %v", err)
		ready <- false
		return
	}
	t.wnd = wnd
	tc, _ := syscall.UTF16PtrFromString("TaskbarCreated")
	t.taskbarCreated, _, _ = user32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(tc)))

	t.data = notifyIconData{Wnd: wnd, ID: 1, Flags: nifMsg | nifIcon | nifTip, CallbackMessage: wmTrayIcon, Icon: loadTrayIcon()}
	t.data.Size = uint32(unsafe.Sizeof(t.data))
	copy(t.data.Tip[:len(t.data.Tip)-1], utf16Of("Toss: receiving on "+t.cfg.ComputerName))
	t.add()
	ready <- true

	var m winMsg
	getMessage := user32.NewProc("GetMessageW")
	for {
		r, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		user32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&m)))
		user32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (t *trayIcon) add() {
	if r, _, err := shell32.NewProc("Shell_NotifyIconW").Call(nimAdd, uintptr(unsafe.Pointer(&t.data))); r == 0 {
		log.Printf("tray: Shell_NotifyIcon: %v", err)
	}
}

func (t *trayIcon) wndProc(wnd, msg, wParam, lParam uintptr) uintptr {
	switch {
	case msg == wmTrayIcon:
		switch lParam & 0xffff {
		case wmLButtonUp:
			go t.command(menuOpen)
		case wmRButtonUp, wmContextMenu:
			t.showMenu()
		}
		return 0
	case msg == t.taskbarCreated && t.taskbarCreated != 0:
		t.add()
		return 0
	case msg == wmClose:
		user32.NewProc("DestroyWindow").Call(wnd)
		return 0
	case msg == wmDestroy:
		shell32.NewProc("Shell_NotifyIconW").Call(nimDelete, uintptr(unsafe.Pointer(&t.data)))
		user32.NewProc("PostQuitMessage").Call(0)
		return 0
	}
	r, _, _ := user32.NewProc("DefWindowProcW").Call(wnd, msg, wParam, lParam)
	return r
}

func (t *trayIcon) showMenu() {
	menu, _, _ := user32.NewProc("CreatePopupMenu").Call()
	if menu == 0 {
		return
	}
	defer user32.NewProc("DestroyMenu").Call(menu)
	appendMenu := user32.NewProc("AppendMenuW")
	for _, item := range []struct {
		id    int
		label string
	}{{menuOpen, "Open Toss"}, {menuPair, "Pair an iPhone…"}, {menuFolder, "Received files"}, {0, ""}, {menuQuit, "Quit Toss"}} {
		if item.id == 0 {
			appendMenu.Call(menu, mfSeparator, 0, 0)
			continue
		}
		label, _ := syscall.UTF16PtrFromString(item.label)
		appendMenu.Call(menu, mfString, uintptr(item.id), uintptr(unsafe.Pointer(label)))
	}
	var pt [2]int32
	user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&pt)))
	// Without this the menu doesn't close when clicking elsewhere.
	user32.NewProc("SetForegroundWindow").Call(t.wnd)
	cmd, _, _ := user32.NewProc("TrackPopupMenu").Call(menu, tpmReturn|tpmNoNotify|tpmRight,
		uintptr(pt[0]), uintptr(pt[1]), 0, t.wnd, 0)
	user32.NewProc("PostMessageW").Call(t.wnd, wmNull, 0, 0)
	if cmd != 0 {
		go t.command(int(cmd))
	}
}

func (t *trayIcon) command(id int) {
	switch id {
	case menuOpen, menuPair:
		token, err := t.store.WebToken()
		if err != nil {
			log.Printf("tray: %v", err)
			return
		}
		link := WebURL(t.cfg, token)
		if id == menuPair {
			link = strings.Replace(link, "/#", "/?pair#", 1)
		}
		if err := OpenURL(link); err != nil {
			log.Printf("tray: %v", err)
		}
	case menuFolder:
		if err := cmdOpen(); err != nil {
			log.Printf("tray: %v", err)
		}
	case menuQuit:
		log.Printf("quit from the tray menu")
		user32.NewProc("PostMessageW").Call(t.wnd, wmClose, 0, 0)
		os.Exit(0)
	}
}

func utf16Of(s string) []uint16 {
	u, _ := syscall.UTF16FromString(s)
	return u[:len(u)-1]
}

// loadTrayIcon writes icon.ico (the embedded PNG inside an ICO container, which Windows Vista
// and later read) and loads it at the small-icon size.
func loadTrayIcon() uintptr {
	dir, err := defaultConfigDir()
	if err != nil {
		return 0
	}
	path := filepath.Join(dir, "icon.ico")
	if _, err := os.Stat(path); err != nil {
		ico := make([]byte, 22, 22+len(iconPNG))
		binary.LittleEndian.PutUint16(ico[2:], 1) // type: icon
		binary.LittleEndian.PutUint16(ico[4:], 1) // one image
		// Directory entry: 0 = 256 px wide and high, 32 bits per pixel, then size and offset.
		binary.LittleEndian.PutUint16(ico[10:], 1)
		binary.LittleEndian.PutUint16(ico[12:], 32)
		binary.LittleEndian.PutUint32(ico[14:], uint32(len(iconPNG)))
		binary.LittleEndian.PutUint32(ico[18:], 22)
		ico = append(ico, iconPNG...)
		_ = os.MkdirAll(dir, 0o700)
		if os.WriteFile(path, ico, 0o644) != nil {
			return 0
		}
	}
	p, _ := syscall.UTF16PtrFromString(path)
	cx, _, _ := user32.NewProc("GetSystemMetrics").Call(49) // SM_CXSMICON
	cy, _, _ := user32.NewProc("GetSystemMetrics").Call(50)
	h, _, _ := user32.NewProc("LoadImageW").Call(0, uintptr(unsafe.Pointer(p)), 1 /* IMAGE_ICON */, cx, cy, 0x10 /* LR_LOADFROMFILE */)
	return h
}

// "Send to → iPhone (Toss)" in Explorer's right-click menu.
func sendToShortcut() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return ""
	}
	return filepath.Join(appData, "Microsoft", "Windows", "SendTo", "iPhone (Toss).lnk")
}

func installSendTo(exe string) (string, error) {
	lnk := sendToShortcut()
	if lnk == "" {
		return "", nil
	}
	loadTrayIcon() // writes icon.ico for the shortcut
	dir, _ := defaultConfigDir()
	q := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	script := "$s = (New-Object -ComObject WScript.Shell).CreateShortcut(" + q(lnk) + ")\n" +
		"$s.TargetPath = " + q(backgroundExe(exe)) + "\n" +
		"$s.Arguments = 'send --notify --'\n" +
		"$s.IconLocation = " + q(filepath.Join(dir, "icon.ico")) + "\n" +
		"$s.Description = 'Send to your iPhone with Toss'\n" +
		"$s.Save()\n"
	if err := hiddenCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodePowerShell(script)).Run(); err != nil {
		return "", err
	}
	return "Explorer (right-click → Send to)", nil
}

func removeSendTo() {
	if lnk := sendToShortcut(); lnk != "" {
		os.Remove(lnk)
	}
}
