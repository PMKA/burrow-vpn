#!/bin/bash
set -e

VERSION="0.1.0"
ARCH="amd64"
PKG="burrow_${VERSION}_${ARCH}"
ROOT="$(cd "$(dirname "$0")" && pwd)"
STAGE="$ROOT/packaging"

echo "==> Building binary..."
cd "$ROOT"
go build -ldflags="-s -w" -o burrow .

echo "==> Staging files..."
cp burrow "$STAGE/usr/local/bin/burrow"
chmod 755 "$STAGE/usr/local/bin/burrow"
chmod 755 "$STAGE/DEBIAN/postinst"

cp icons/burrow.svg "$STAGE/usr/share/icons/hicolor/scalable/apps/burrow.svg"

echo "==> Building .deb..."
dpkg-deb --root-owner-group --build "$STAGE" "${PKG}.deb"

echo ""
echo "Done: ${PKG}.deb"
ls -lh "${PKG}.deb"
