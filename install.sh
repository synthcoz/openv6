#!/usr/bin/env bash
# openv6 one-line installer (downloads the latest release from GitHub).
#   panel + proxies on this machine:  curl -fsSL https://raw.githubusercontent.com/synthcoz/openv6/main/install.sh | sudo bash
#   remote node:                      ... | sudo bash -s -- agent --panel URL --token TOKEN
set -euo pipefail
REPO="${OPENV6_REPO:-synthcoz/openv6}"
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
else
  URL="https://github.com/$REPO/releases/download/$VERSION/openv6-linux-$ARCH"
fi
echo ">> downloading $URL"
curl -fL --progress-bar "$URL" -o /usr/local/bin/openv6.new
chmod +x /usr/local/bin/openv6.new
mv -f /usr/local/bin/openv6.new /usr/local/bin/openv6
if [ $# -eq 0 ]; then set -- server; fi
/usr/local/bin/openv6 install "$@"
