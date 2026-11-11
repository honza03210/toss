#!/usr/bin/env bash
# Builds xtool 1.24.0 with two patches and installs it as ~/.local/bin/xtool-toss:
#  - xtool-paid-capabilities.patch (same as SensorLab's xtool-patched),
#  - xtool-keychain-groups.patch: keeps keychain-access-groups from the entitlements file
#    instead of replacing them, so the app and its share extension can share the pairing.
# Uses its own clone (~/.cache/xtool-toss-src) and the libimobiledevice build that
# SensorLab/Scripts/build-patched-xtool.sh put in ~/.local/xtool-deps.
set -euo pipefail
here="$(cd "$(dirname "$0")" && pwd)"
work="$HOME/.cache/xtool-toss-src"
prefix="$HOME/.local/xtool-deps"
. "$HOME/.local/share/swiftly/env.sh"
[ -f "$prefix/lib/libimobiledevice-1.0.so" ] || { echo "Run SensorLab/Scripts/build-patched-xtool.sh once first (builds ~/.local/xtool-deps)"; exit 1; }

export PKG_CONFIG_PATH="$prefix/lib/pkgconfig" CPATH="$prefix/include" LIBRARY_PATH="$prefix/lib" LD_LIBRARY_PATH="$prefix/lib"

if [ ! -d "$work" ]; then
  git clone --depth 1 --branch 1.24.0 https://github.com/xtool-org/xtool.git "$work"
fi
if git -C "$work" diff --quiet; then
  git -C "$work" apply "$here/xtool-paid-capabilities.patch"
  git -C "$work" apply "$here/xtool-keychain-groups.patch"
else
  echo "==> Patches already applied"
fi

echo "==> Building xtool"
cd "$work"
swift build -c release --product xtool -Xlinker -rpath -Xlinker "$prefix/lib"
install -m 755 "$(swift build -c release --show-bin-path)/xtool" "$HOME/.local/bin/xtool-toss"
"$HOME/.local/bin/xtool-toss" --version
echo "Installed $HOME/.local/bin/xtool-toss"
