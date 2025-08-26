#!/usr/bin/env bash
set -euo pipefail

echo "Getting Phishing Club"
# Get first phishingclub*.tar.gz asset from latest release
URL=$(curl -fsSL https://api.github.com/repos/phishingclub/phishingclub/releases/latest \
  | grep -Eo 'https://[^"]+/releases/download/[^"]+/phishingclub[^"/]*\.tar\.gz' | head -1) || true
[ -n "$URL" ] || { echo "[!] No phishingclub tarball found" >&2; exit 1; }

TMP=$(mktemp -d /tmp/phishingclub.XXXXXX)
curl -fsSL "$URL" -o "$TMP/pc.tgz"

# Extract (flat archive: binary lands directly inside $TMP)
tar -xzf "$TMP/pc.tgz" -C "$TMP"

echo "Installing from $TMP"
cd "$TMP"
sudo ./phishingclub --install
