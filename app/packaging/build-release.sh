#!/usr/bin/env bash
# Builds every distributable artifact: a portable Windows .exe (zipped),
# Linux .deb/.rpm/Arch packages, and a source tarball. Run from the repo
# root or from app/ — it figures out where it is.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$APP_DIR/.." && pwd)"
DIST_DIR="$REPO_ROOT/dist"

if [[ $# -lt 1 ]]; then
  echo "usage: $(basename "$0") <version>   (e.g. 0.1.3)" >&2
  exit 2
fi
VERSION="$1"

command -v wails >/dev/null || { echo "wails CLI not found on PATH"; exit 1; }
command -v nfpm >/dev/null || { echo "nfpm not found on PATH"; exit 1; }

# The version lives in three places that all end up user-visible: this
# argument (which names the artifacts), wails.json (which is stamped into
# the Windows .exe properties), and the PKGBUILD (which decides what tarball
# Arch downloads). v0.1.2 shipped an .exe labelled 0.1.0 because nothing
# checked. Now a mismatch stops the release instead of shipping wrong.
wails_version="$(sed -n 's/.*"productVersion"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$APP_DIR/wails.json")"
pkgbuild_version="$(sed -n 's/^pkgver=\(.*\)$/\1/p' "$SCRIPT_DIR/PKGBUILD")"
fail=0
if [[ "$wails_version" != "$VERSION" ]]; then
  echo "version mismatch: wails.json productVersion is '$wails_version', expected '$VERSION'" >&2
  fail=1
fi
if [[ "$pkgbuild_version" != "$VERSION" ]]; then
  echo "version mismatch: PKGBUILD pkgver is '$pkgbuild_version', expected '$VERSION'" >&2
  fail=1
fi
[[ $fail -eq 0 ]] || { echo "Bump those to $VERSION and commit before tagging." >&2; exit 1; }

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

echo "==> Shipping the PKGBUILD"
# So Arch users can grab it from the release page without cloning the repo,
# which is what the README tells them they can do.
cp "$SCRIPT_DIR/PKGBUILD" "$DIST_DIR/PKGBUILD"

echo "==> Writing checksums"
( cd "$DIST_DIR" && sha256sum -- * > SHA256SUMS )

echo "==> Done. Artifacts in $DIST_DIR:"
ls -la "$DIST_DIR"
