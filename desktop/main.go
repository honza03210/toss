// Toss receives things shared from an iPhone (text, links, photos, files) and shows them
// as desktop notifications. See ../PROTOCOL.md.
package main

import (
	"context"
	"crypto/tls"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Set at build time: -ldflags "-X main.version=1.2.3"
var version = "dev"

//go:embed assets/icon.png
var iconPNG []byte

const usage = `Toss: share from your iPhone to this computer.

Usage:
  toss pair              Show a QR code to pair an iPhone (scan it with the Camera or the Toss app)
  toss serve             Receive in the foreground (what the background service runs)
  toss install           Start Toss automatically when you log in, and start it now
  toss uninstall         Stop starting it automatically, and stop it
  toss status            Show the settings, paired phones and whether Toss is running
  toss devices           List paired phones
  toss unpair <name>     Remove a paired phone
  toss open              Open the folder received files go to
  toss send <file>…      Send files to the iPhone (also: --text <text>, --clipboard, - for stdin);
                         they wait until you open Toss on the phone
  toss inbox [clear]     List (or drop) what waits for the phone
  toss history [n|clear] Show what was received recently
  toss web [--pair]      Open Toss's page in the browser: history, sending, pairing
  toss config <key> <value>
                         name <text>       name shown on the phone
                         dir <folder>      where received files go ("default" for Downloads/From iPhone)
                         autocopy on|off   copy received text and links right away
                         autoopen on|off   open received links in the browser right away
                         port <number>     port to listen on (pair again after changing)
  toss version
`

func main() {
	log.SetFlags(log.Ldate | log.Ltime)
	enableTerminalColors()
	args := os.Args[1:]
	cmd := "help"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "pair":
		err = cmdPair()
	case "serve":
		err = cmdServe()
	case "install":
		err = cmdInstall()
	case "uninstall":
		err = cmdUninstall()
	case "status":
		err = cmdStatus()
	case "devices":
		err = cmdDevices()
	case "unpair":
		err = cmdUnpair(args)
	case "open":
		err = cmdOpen()
	case "send":
		err = cmdSend(args)
	case "inbox":
		err = cmdInbox(args)
	case "history":
		err = cmdHistory(args)
	case "web":
		err = cmdWeb(args)
	case "config":
		err = cmdConfig(args)
	case "version", "--version", "-v":
		fmt.Println("toss", version, runtime.GOOS+"/"+runtime.GOARCH)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "toss:", err)
		os.Exit(1)
	}
}

// iconPath writes the embedded icon next to the config, for notifications.
func iconPath() string {
	dir, err := defaultConfigDir()
	if err != nil {
		return ""
	}
	path := filepath.Join(dir, "icon.png")
	if info, err := os.Stat(path); err == nil && info.Size() == int64(len(iconPNG)) {
		return path
	}
	_ = os.MkdirAll(dir, 0o700)
	if os.WriteFile(path, iconPNG, 0o644) != nil {
		return ""
	}
	return path
}

func cmdServe() error {
	store, err := OpenStore()
	if err != nil {
		return err
	}
	cfg, err := store.Config()
	if err != nil {
		return err
	}
	setupLog(store, isBackground())
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv := NewServer(store, SystemNotifier{})
	log.Printf("toss %s receiving on port %d; files go to %s", version, cfg.Port, saveDir(cfg))
	go func() {
		if err := srv.ListenAndServeWeb(ctx, webPort(cfg)); err != nil {
			log.Printf("local page (port %d): %v", webPort(cfg), err)
		}
	}()
	startTray(ctx, store, cfg)
	err = srv.ListenAndServe(ctx, cfg.Port)
	if isAddrInUse(err) {
		return fmt.Errorf("port %d is in use: Toss is probably already running (toss status)", cfg.Port)
	}
	return err
}

// isBackground reports whether output goes nowhere useful (autostart, tossw.exe).
func isBackground() bool {
	info, err := os.Stdout.Stat()
	return err != nil || info.Mode()&os.ModeCharDevice == 0
}

// setupLog also writes to toss.log in the config folder (restarted when over 1 MB).
func setupLog(store *Store, background bool) {
	path := store.path("toss.log")
	if info, err := os.Stat(path); err == nil && info.Size() > 1<<20 {
		_ = os.Rename(path, path+".old")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	if background {
		log.SetOutput(f)
	} else {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
	}
}

func isAddrInUse(err error) bool {
	return err != nil && (errors.Is(err, syscall.EADDRINUSE) || strings.Contains(err.Error(), "address already in use") ||
		strings.Contains(err.Error(), "Only one usage of each socket address"))
}

// running checks whether a Toss server answers on the port.
func running(port int) bool {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 700 * time.Millisecond}, "tcp",
		net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), &tls.Config{InsecureSkipVerify: true})
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func cmdPair() error {
	store, err := OpenStore()
	if err != nil {
		return err
	}
	cfg, err := store.Config()
	if err != nil {
		return err
	}
	cert, err := store.Certificate(cfg.ComputerName)
	if err != nil {
		return err
	}
	pending, err := store.NewPairing(10 * time.Minute)
	if err != nil {
		return err
	}
	defer store.CancelPairing(pending.Token)

	// Pair through the background server if it runs, else serve here until paired.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	paired := make(chan Device, 1)
	ownServer := !running(cfg.Port)
	if ownServer {
		srv := NewServer(store, SystemNotifier{})
		srv.onPaired = func(d Device) { paired <- d }
		errc := make(chan error, 1)
		go func() { errc <- srv.ListenAndServe(ctx, cfg.Port) }()
		select {
		case err := <-errc:
			if isAddrInUse(err) {
				return fmt.Errorf("port %d is in use by another program (toss config port <n>)", cfg.Port)
			}
			return err
		case <-time.After(300 * time.Millisecond):
		}
	} else {
		before := map[string]time.Time{}
		for _, d := range store.Devices() {
			before[d.ID] = d.Paired
		}
		go func() {
			for ctx.Err() == nil {
				time.Sleep(500 * time.Millisecond)
				for _, d := range store.Devices() {
					if t, ok := before[d.ID]; !ok || d.Paired.After(t) {
						paired <- d
						return
					}
				}
			}
		}()
	}

	hosts := pairingHosts()
	link := PairingLink(cfg.ComputerName, hosts, cfg.Port, Fingerprint(cert), pending.Token)
	code, err := EncodeQR([]byte(link), eccL)
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Print(code.Terminal())
	fmt.Printf("\nScan this with the iPhone's Camera (or in the Toss app) to pair with %q.\n", cfg.ComputerName)
	fmt.Printf("The phone will connect to: %s (port %d)\n", strings.Join(hosts, ", "), cfg.Port)
	fmt.Printf("If scanning doesn't work, open this link on the iPhone instead:\n  %s\n", link)
	fmt.Println("The code works once and expires in 10 minutes. Waiting… (Ctrl+C to cancel)")

	select {
	case d := <-paired:
		fmt.Printf("\n✓ Paired with %s.\n", d.Name)
		if !autostartInstalled() {
			if ownServer {
				fmt.Println("Toss isn't running in the background yet. Run `toss install` to keep receiving.")
			}
		}
		// Give the phone's response time to arrive before this process (and its server) exits.
		time.Sleep(time.Second)
		return nil
	case <-time.After(time.Until(pending.Expires)):
		return errors.New("the pairing code expired: run `toss pair` again")
	case <-ctx.Done():
		fmt.Println("\nCancelled.")
		return nil
	}
}

// PairingLink builds the toss://pair link the QR code carries.
func PairingLink(name string, hosts []string, port int, fingerprint, token string) string {
	q := url.Values{}
	q.Set("v", "1")
	q.Set("n", name)
	q.Set("h", strings.Join(hosts, ","))
	q.Set("p", strconv.Itoa(port))
	q.Set("fp", fingerprint)
	q.Set("t", token)
	// Fixed order keeps the link (and QR code) readable and stable.
	var parts []string
	for _, k := range []string{"v", "n", "h", "p", "fp", "t"} {
		parts = append(parts, k+"="+url.QueryEscape(q.Get(k)))
	}
	return "toss://pair?" + strings.Join(parts, "&")
}

// pairingHosts lists where the phone should look for this computer: the mDNS name first
// (survives DHCP address changes), then IPv4 addresses, private networks first.
func pairingHosts() []string {
	var hosts []string
	if h, err := os.Hostname(); err == nil && h != "" && !strings.Contains(h, ".") && isDNSLabel(h) {
		hosts = append(hosts, strings.ToLower(h)+".local")
	}
	type addr struct {
		ip   string
		rank int
	}
	var addrs []addr
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || isVirtualInterface(iface.Name) {
			continue
		}
		list, _ := iface.Addrs()
		for _, a := range list {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLinkLocalUnicast() || ip.IsLoopback() {
				continue
			}
			rank := 1
			if ip.IsPrivate() {
				rank = 0
			}
			addrs = append(addrs, addr{ip.String(), rank})
		}
	}
	sort.SliceStable(addrs, func(i, j int) bool { return addrs[i].rank < addrs[j].rank })
	for i, a := range addrs {
		if i == 4 {
			break
		}
		hosts = append(hosts, a.ip)
	}
	return hosts
}

func isDNSLabel(s string) bool {
	for _, r := range s {
		if !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	return len(s) <= 63
}

func isVirtualInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, p := range []string{"docker", "br-", "veth", "virbr", "vmnet", "vboxnet", "lxc", "lxd", "podman", "cni", "flannel", "zt"} {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	for _, s := range []string{"vethernet", "virtualbox", "vmware", "hyper-v", "wsl"} {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

func installedExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func cmdInstall() error {
	exe, err := installedExe()
	if err != nil {
		return err
	}
	store, err := OpenStore()
	if err != nil {
		return err
	}
	cfg, err := store.Config()
	if err != nil {
		return err
	}
	if _, err := store.Certificate(cfg.ComputerName); err != nil {
		return err
	}
	if err := installAutostart(exe); err != nil {
		return fmt.Errorf("autostart: %w", err)
	}
	fmt.Println("✓ Toss will start when you log in.")
	if where, err := installSendTo(exe); err != nil {
		fmt.Printf("(Couldn't add \"Send to iPhone\" to the file manager: %v)\n", err)
	} else if where != "" {
		fmt.Printf("✓ Added \"Send to iPhone\" to %s.\n", where)
	}
	if running(cfg.Port) {
		fmt.Println("✓ Toss is already running.")
	} else {
		if err := startDetached(exe); err != nil {
			return fmt.Errorf("starting in the background: %w", err)
		}
		for i := 0; i < 20 && !running(cfg.Port); i++ {
			time.Sleep(150 * time.Millisecond)
		}
		if running(cfg.Port) {
			fmt.Println("✓ Toss is running in the background.")
		} else {
			fmt.Printf("Toss didn't start: see %s\n", store.path("toss.log"))
		}
	}
	if len(store.Devices()) == 0 {
		fmt.Println("\nNext: get the Toss app on your iPhone (App Store), run `toss pair` and scan the code.")
	}
	return nil
}

func cmdUninstall() error {
	if err := removeAutostart(); err != nil {
		return err
	}
	removeSendTo()
	stopRunning()
	fmt.Println("✓ Toss won't start automatically anymore and was stopped.")
	dir, _ := defaultConfigDir()
	fmt.Printf("Pairings and settings are kept in %s (delete it to forget them).\n", dir)
	return nil
}

func cmdStatus() error {
	store, err := OpenStore()
	if err != nil {
		return err
	}
	cfg, err := store.Config()
	if err != nil {
		return err
	}
	state := "not running"
	if running(cfg.Port) {
		state = "running"
	}
	auto := "no"
	if autostartInstalled() {
		auto = "yes"
	}
	fmt.Printf("Toss %s: %s on port %d, starts at login: %s\n", version, state, cfg.Port, auto)
	fmt.Printf("Name:        %s\n", cfg.ComputerName)
	fmt.Printf("Files go to: %s\n", saveDir(cfg))
	fmt.Printf("Auto-copy:   %v\n", cfg.AutoCopy)
	fmt.Printf("Auto-open:   %v\n", cfg.OpensLinks())
	if n := len(store.Outbox()); n > 0 {
		fmt.Printf("Waiting:     %d items for the phone (toss inbox)\n", n)
	}
	fmt.Printf("Page:        toss web (http://127.0.0.1:%d)\n", webPort(cfg))
	fmt.Printf("Addresses:   %s\n", strings.Join(pairingHosts(), ", "))
	fmt.Printf("Config:      %s\n", store.Dir)
	if hint := firewallHint(cfg.Port); hint != "" {
		fmt.Println(hint)
	}
	fmt.Println()
	return cmdDevices()
}

func cmdDevices() error {
	store, err := OpenStore()
	if err != nil {
		return err
	}
	devices := store.Devices()
	if len(devices) == 0 {
		fmt.Println("No paired phones. Run `toss pair`.")
		return nil
	}
	fmt.Println("Paired phones:")
	for _, d := range devices {
		seen := "never used"
		if !d.LastSeen.IsZero() {
			seen = "last used " + d.LastSeen.Format("2006-01-02 15:04")
		}
		fmt.Printf("  %-20s paired %s, %s\n", d.Name, d.Paired.Format("2006-01-02"), seen)
	}
	return nil
}

func cmdUnpair(args []string) error {
	if len(args) != 1 {
		return errors.New("usage: toss unpair <name>")
	}
	store, err := OpenStore()
	if err != nil {
		return err
	}
	n, err := store.RemoveDevice(args[0])
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("no paired phone named %q (toss devices)", args[0])
	}
	fmt.Printf("✓ Removed %s. It can't send anymore until it pairs again.\n", args[0])
	return nil
}

func cmdOpen() error {
	store, err := OpenStore()
	if err != nil {
		return err
	}
	cfg, err := store.Config()
	if err != nil {
		return err
	}
	dir := saveDir(cfg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return OpenPath(dir)
}

func cmdConfig(args []string) error {
	store, err := OpenStore()
	if err != nil {
		return err
	}
	cfg, err := store.Config()
	if err != nil {
		return err
	}
	if len(args) != 2 {
		return errors.New("usage: toss config <name|dir|autocopy|autoopen|port> <value>")
	}
	key, value := args[0], args[1]
	switch key {
	case "name":
		cfg.ComputerName = value
	case "dir":
		if value == "default" {
			cfg.SaveDir = ""
		} else {
			abs, err := filepath.Abs(value)
			if err != nil {
				return err
			}
			cfg.SaveDir = abs
		}
	case "autocopy", "autoopen":
		var on bool
		switch value {
		case "on", "true", "yes":
			on = true
		case "off", "false", "no":
			on = false
		default:
			return fmt.Errorf("%s takes on or off", key)
		}
		if key == "autocopy" {
			cfg.AutoCopy = on
		} else {
			cfg.AutoOpen = &on
		}
	case "port":
		p, err := strconv.Atoi(value)
		if err != nil || p < 1024 || p > 65535 {
			return errors.New("port must be a number from 1024 to 65535")
		}
		cfg.Port = p
	default:
		return fmt.Errorf("unknown setting %q", key)
	}
	if err := store.SaveConfig(cfg); err != nil {
		return err
	}
	fmt.Printf("✓ %s = %s\n", key, value)
	if key == "port" || key == "name" {
		fmt.Println("Restart Toss (toss uninstall && toss install) and pair again for this to reach the phone.")
	}
	return nil
}
