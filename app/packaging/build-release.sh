#!/usr/bin/env bash
# Builds every distributable artifact: a portable Windows .exe (zipped),
# Linux .deb/.rpm/Arch packages, and a source tarball. Run from the repo
# root or from app/ — it figures out where it is.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$APP_DIR/.." && pwd)"
VERSION="${1:-0.1.0}"
DIST_DIR="$REPO_ROOT/dist"

command -v wails >/dev/null || { echo "wails CLI not found on PATH"; exit 1; }
command -v nfpm >/dev/null || { echo "nfpm not found on PATH"; exit 1; }

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

cd "$APP_DIR"

echo "==> Building Linux binary"
wails build -tags webkit2_41
cp build/bin/shogun2sync "$DIST_DIR/shogun2sync-linux-amd64"
chmod +x "$DIST_DIR/shogun2sync-linux-amd64"

echo "==> Building Windows binary (portable .exe, no installer)"
wails build -platform windows/amd64
( cd build/bin && zip -q "$DIST_DIR/shogun2sync-windows-amd64.zip" shogun2sync.exe )

echo "==> Building .deb / .rpm / Arch packages"
export VERSION
for pkg in deb rpm archlinux; do
  nfpm package --config "$SCRIPT_DIR/nfpm.yaml" --packager "$pkg" --target "$DIST_DIR/"
done

echo "==> Building source tarball"
git -C "$REPO_ROOT" archive --format=tar.gz \
  --prefix="shogun2-sync-$VERSION/" \
  -o "$DIST_DIR/shogun2-sync-$VERSION-src.tar.gz" HEAD

echo "==> Done. Artifacts in $DIST_DIR:"
ls -la "$DIST_DIR"
