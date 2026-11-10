#!/usr/bin/env bash
# build-deb.sh VERSION ARCH BINARY -> openv6_VERSION_ARCH.deb
set -euo pipefail
VERSION=$1; ARCH=$2; BIN=$3
case "$ARCH" in arm) DARCH=armhf ;; *) DARCH=$ARCH ;; esac
ROOT=$(mktemp -d)
install -Dm755 "$BIN" "$ROOT/usr/lib/openv6/openv6"
mkdir -p "$ROOT/DEBIAN"
cat > "$ROOT/DEBIAN/control" <<CTL
Package: openv6
Version: ${VERSION#v}
Section: net
Priority: optional
Architecture: $DARCH
Depends: iproute2
Recommends: util-linux
Maintainer: openv6 <noreply@users.noreply.github.com>
Homepage: https://thephuc.xyz/openv6/
Description: Static IPv6 proxies (HTTP + SOCKS5) with a web dashboard
 Turns the IPv6 prefix of a home network, VPS or homelab into static proxies.
 Includes a background service and a tray icon.
CTL
cat > "$ROOT/DEBIAN/postinst" <<'SH'
#!/bin/sh
set -e
if [ "$1" = "configure" ]; then
  /usr/lib/openv6/openv6 install server || true
fi
SH
cat > "$ROOT/DEBIAN/prerm" <<'SH'
#!/bin/sh
if [ "$1" = "remove" ] || [ "$1" = "purge" ]; then
  /usr/local/bin/openv6 uninstall || true
fi
SH
chmod 755 "$ROOT/DEBIAN/postinst" "$ROOT/DEBIAN/prerm"
dpkg-deb --root-owner-group --build "$ROOT" "openv6_${VERSION#v}_${DARCH}.deb"
rm -rf "$ROOT"
