#!/bin/bash
set -e

VERSION="0.7.2"
ARCH="amd64"
PKG="burrow-vpn_${VERSION}_${ARCH}"
ROOT="$(cd "$(dirname "$0")" && pwd)"
STAGE="$ROOT/packaging"

echo "==> Building binary..."
cd "$ROOT"
go build -ldflags="-s -w" -o burrow-vpn .

echo "==> Staging files..."
cp burrow-vpn "$STAGE/usr/local/bin/burrow-vpn"
chmod 755 "$STAGE/usr/local/bin/burrow-vpn"
chmod 755 "$STAGE/DEBIAN/postinst"

cp icons/burrow-on.png  "$STAGE/usr/share/icons/hicolor/512x512/apps/burrow-vpn.png"
cp icons/burrow-on.png  "$STAGE/usr/share/icons/hicolor/512x512/apps/burrow-on.png"
cp icons/burrow-off.png "$STAGE/usr/share/icons/hicolor/512x512/apps/burrow-off.png"

echo "==> Building .deb..."
dpkg-deb --root-owner-group --build "$STAGE" "${PKG}.deb"

echo "==> Writing checksum..."
sha256sum "${PKG}.deb" | awk '{print $1, "'"${PKG}.deb"'"}' > "${PKG}.deb.sha256"

echo ""
echo "Done: ${PKG}.deb"
ls -lh "${PKG}.deb" "${PKG}.deb.sha256"
