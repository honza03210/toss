#!/usr/bin/env bash
# Builds the desktop companion for every release target into dist/ (what CI uploads):
#   toss-{linux,darwin}-{amd64,arm64}.tar.gz, toss-windows-{amd64,arm64}.zip, install.sh, install.ps1, checksums.txt
# Usage: Scripts/build-desktop.sh [version]
set -euo pipefail
root="$(cd "$(dirname "$0")/.." && pwd)"
version="${1:-dev}"
dist="$root/dist"
rm -rf "$dist" && mkdir -p "$dist"
cd "$root/desktop"
flags="-s -w -X main.version=$version"
for arch in amd64 arm64; do
  work="$(mktemp -d)"
  CGO_ENABLED=0 GOOS=linux GOARCH=$arch go build -trimpath -ldflags "$flags" -o "$work/toss" .
  tar -czf "$dist/toss-linux-$arch.tar.gz" -C "$work" toss
  rm -rf "$work"

  work="$(mktemp -d)"
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$flags" -o "$work/toss.exe" .
  # tossw.exe: the same program without a console window, for running in the background.
  CGO_ENABLED=0 GOOS=windows GOARCH=$arch go build -trimpath -ldflags "$flags -H windowsgui" -o "$work/tossw.exe" .
  (cd "$work" && zip -q "$dist/toss-windows-$arch.zip" toss.exe tossw.exe)
  rm -rf "$work"

  work="$(mktemp -d)"
  CGO_ENABLED=0 GOOS=darwin GOARCH=$arch go build -trimpath -ldflags "$flags" -o "$work/toss" .
  tar -czf "$dist/toss-darwin-$arch.tar.gz" -C "$work" toss
  rm -rf "$work"
done
repo="${TOSS_REPO:-}"
for f in install.sh install.ps1; do
  if [ -n "$repo" ]; then sed "s#OWNER/toss#$repo#g" "install/$f" > "$dist/$f"; else cp "install/$f" "$dist/$f"; fi
done
if command -v sha256sum >/dev/null; then sum=sha256sum; else sum="shasum -a 256"; fi
# Git Bash on Windows marks binary mode with "*"; keep the plain "hash  name" format.
(cd "$dist" && $sum toss-* | sed 's/ \*/  /' > checksums.txt)
ls -la "$dist"
