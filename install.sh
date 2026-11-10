#!/usr/bin/env bash
# openv6 one-line installer (downloads the latest release from GitHub).
#   panel + proxies on this machine:  curl -fsSL https://raw.githubusercontent.com/synthcoz/openv6-releases/main/install.sh | sudo bash
#   remote node:                      ... | sudo bash -s -- agent --panel URL --token TOKEN
set -euo pipefail
REPO="${OPENV6_REPO:-synthcoz/openv6-releases}"
VERSION="${OPENV6_VERSION:-latest}"
if [ "$(id -u)" != "0" ]; then echo "Hãy chạy với sudo / run as root"; exit 1; fi
if [ "$(uname -s)" != "Linux" ]; then echo "openv6 nodes require Linux"; exit 1; fi
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  armv7l|armv7) ARCH=arm ;;
  *) echo "unsupported CPU: $(uname -m)"; exit 1 ;;
esac
if ! command -v ip >/dev/null 2>&1; then
  if command -v apt-get >/dev/null 2>&1; then apt-get update -qq && apt-get install -y -qq iproute2;
  elif command -v dnf >/dev/null 2>&1; then dnf install -y iproute;
  else echo "please install iproute2"; exit 1; fi
fi
if [ "$VERSION" = "latest" ]; then
  URL="https://github.com/$REPO/releases/latest/download/openv6-linux-$ARCH"
  API="https://api.github.com/repos/$REPO/releases/latest"
else
  URL="https://github.com/$REPO/releases/download/$VERSION/openv6-linux-$ARCH"
  API="https://api.github.com/repos/$REPO/releases/tags/$VERSION"
fi
OUT=/usr/local/bin/openv6.new
echo ">> downloading openv6-linux-$ARCH"
if ! curl -fL --retry 3 --retry-delay 3 --connect-timeout 15 --progress-bar "$URL" -o "$OUT"; then
  # github.com download links sometimes fail (504); fall back to the API asset endpoint
  echo ">> direct download failed, trying GitHub API"
  ASSET=$(curl -fsSL --retry 3 "$API" | awk -v n="openv6-linux-$ARCH" '
    /"url": *"https:\/\/api.github.com\/repos\/.*\/releases\/assets\// { gsub(/[",]/, "", $2); u=$2 }
    /"name":/ { gsub(/[",]/, "", $2); if ($2 == n && u != "") { print u; exit } }')
  if [ -z "$ASSET" ]; then echo "cannot find release asset openv6-linux-$ARCH"; exit 1; fi
  curl -fL --retry 3 --retry-delay 3 --progress-bar -H "Accept: application/octet-stream" "$ASSET" -o "$OUT"
fi
if ! head -c 4 "$OUT" | grep -q "ELF"; then echo "downloaded file is not a Linux binary"; rm -f "$OUT"; exit 1; fi
chmod +x "$OUT"
mv -f "$OUT" /usr/local/bin/openv6
if [ $# -eq 0 ]; then set -- server; fi
/usr/local/bin/openv6 install "$@"
if [ "$1" = "server" ]; then
  for i in 1 2 3 4 5 6 7 8 9 10; do [ -s /var/lib/openv6/setup-code.txt ] && break; sleep 1; done
  if [ -s /var/lib/openv6/setup-code.txt ]; then
    echo ">> first-time setup code: $(cat /var/lib/openv6/setup-code.txt)"
    echo "   (asked only when you create the admin account from outside this machine/LAN)"
  fi
fi
