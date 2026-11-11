#!/usr/bin/env bash
# Puts your GitHub repository into the app, website and installers (they say OWNER/toss
# until then): Scripts/set-repo.sh yourname/toss
set -euo pipefail
repo="${1:?usage: Scripts/set-repo.sh <owner>/<repo>}"
owner="${repo%%/*}"
name="${repo##*/}"
root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"
files=(ios/Sources/TossKit/Links.swift docs/index.html docs/privacy.html desktop/install/install.sh desktop/install/install.ps1 README.md ios/AppStore/APPSTORE.md)
sed -i "s#OWNER/toss#$repo#g; s#OWNER\.github\.io/toss#$owner.github.io/$name#g" "${files[@]}"
echo "Set the repository to $repo in: ${files[*]}"
grep -rn "OWNER" "${files[@]}" || echo "No placeholders left."
