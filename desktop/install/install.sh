#!/bin/sh
# Installs Toss for Linux or macOS into ~/.local/bin and starts it at login. No root needed.
#   curl -fsSL https://github.com/honza03210/toss/releases/latest/download/install.sh | sh
set -eu
REPO="${TOSS_REPO:-honza03210/toss}"
BIN_DIR="${TOSS_BIN_DIR:-$HOME/.local/bin}"

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) echo "This installer is for Linux and macOS. On Windows use install.ps1." >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) echo "Unsupported CPU: $(uname -m)" >&2; exit 1 ;;
esac

asset="toss-$os-$arch.tar.gz"
base="${TOSS_BASE_URL:-https://github.com/$REPO/releases/latest/download}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading ${asset}…"
curl -fsSL "$base/$asset" -o "$tmp/$asset"
curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"
if command -v sha256sum >/dev/null 2>&1; then sha="sha256sum -c --quiet -"; else sha="shasum -a 256 -c --quiet -"; fi
(cd "$tmp" && grep " $asset\$" checksums.txt | $sha) || { echo "Checksum mismatch: not installing." >&2; exit 1; }

tar -xzf "$tmp/$asset" -C "$tmp"
mkdir -p "$BIN_DIR"
# Stop a running copy so the binary can be replaced.
"$BIN_DIR/toss" uninstall >/dev/null 2>&1 || true
install -m 755 "$tmp/toss" "$BIN_DIR/toss"
echo "Installed $BIN_DIR/toss"

"$BIN_DIR/toss" install

if [ "$os" = darwin ]; then
  command -v terminal-notifier >/dev/null 2>&1 || echo "Tip: brew install terminal-notifier makes Toss's notifications clickable."
elif ! command -v notify-send >/dev/null 2>&1; then
  echo "Tip: install libnotify (e.g. sudo apt install libnotify-bin) to get notifications."
fi
if [ -n "${WAYLAND_DISPLAY:-}" ] && ! command -v wl-copy >/dev/null 2>&1 && ! command -v xclip >/dev/null 2>&1; then
  echo "Tip: install wl-clipboard (sudo apt install wl-clipboard) so received text is copied to the clipboard."
fi
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *) echo "Note: $BIN_DIR is not on your PATH; open a new terminal or add it to run 'toss' directly." ;;
esac
