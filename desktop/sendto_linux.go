package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// "Send to iPhone" in the file manager's right-click menu: a script for Files (Nautilus) and
// Nemo, a service menu for Dolphin. Each runs `toss send --notify <files>`.

func dataHome() string {
	if d := os.Getenv("XDG_DATA_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share")
}

const sendToName = "Send to iPhone (Toss)"

func scriptPaths() map[string]string {
	return map[string]string{
		"nautilus": filepath.Join(dataHome(), "nautilus", "scripts", sendToName),
		"nemo":     filepath.Join(dataHome(), "nemo", "scripts", sendToName),
	}
}

func dolphinMenu() string {
	return filepath.Join(dataHome(), "kio", "servicemenus", "toss-send.desktop")
}

func installSendTo(exe string) (string, error) {
	var where []string
	script := fmt.Sprintf("#!/bin/sh\n# Added by `toss install`; removed by `toss uninstall`.\nexec %s send --notify -- \"$@\"\n", shellQuote(exe))
	labels := map[string]string{"nautilus": "Files (right-click → Scripts)", "nemo": "Nemo (right-click → Scripts)"}
	for fm, path := range scriptPaths() {
		if !has(fm) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			return "", err
		}
		where = append(where, labels[fm])
	}
	if has("dolphin") {
		menu := fmt.Sprintf(`[Desktop Entry]
Type=Service
MimeType=application/octet-stream;
X-KDE-ServiceTypes=KonqPopupMenu/Plugin
Actions=tossSend
X-KDE-Priority=TopLevel

[Desktop Action tossSend]
Name=Send to iPhone
Icon=phone
Exec="%s" send --notify -- %%F
`, exe)
		path := dolphinMenu()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		// Dolphin only runs service menus that are executable.
		if err := os.WriteFile(path, []byte(menu), 0o755); err != nil {
			return "", err
		}
		where = append(where, "Dolphin (right-click)")
	}
	return strings.Join(where, " and "), nil
}

func removeSendTo() {
	for _, path := range scriptPaths() {
		os.Remove(path)
	}
	os.Remove(dolphinMenu())
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// No tray icon on Linux (it would need a D-Bus library): `toss web` has the same things.
func startTray(ctx context.Context, store *Store, cfg Config) {}
