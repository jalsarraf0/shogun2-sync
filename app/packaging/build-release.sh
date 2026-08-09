#!/usr/bin/env bash
# Builds every distributable artifact: a portable Windows .exe (zipped),
# Linux .deb/.rpm/Arch packages, and a source tarball. Run from the repo
# root or from app/ — it figures out where it is.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$APP_DIR/.." && pwd)"
DIST_DIR="$REPO_ROOT/dist"
RCLONE_VERSION="1.74.4"
RCLONE_ARCHIVE="rclone-v${RCLONE_VERSION}-linux-amd64.zip"
RCLONE_ARCHIVE_SHA256="fe435e0c36228e7c2f116a8701f01127bb1f694005fc11d1f27186c8bca4115d"
RCLONE_COPYING_SHA256="8cd2e9e750b90a04b7d82dbbca3930c696ae0309d7c10464f90a44f45754cd04"
RCLONE_BASE_URL="https://downloads.rclone.org/v${RCLONE_VERSION}"

if [[ $# -lt 1 ]]; then
  echo "usage: $(basename "$0") <version>   (e.g. 1.0.0)" >&2
  exit 2
fi
VERSION="$1"

if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "version must be a plain semantic version (for example, 1.0.0)" >&2
  exit 2
fi

command -v wails >/dev/null || { echo "wails CLI not found on PATH"; exit 1; }
command -v nfpm >/dev/null || { echo "nfpm not found on PATH"; exit 1; }
command -v curl >/dev/null || { echo "curl not found on PATH"; exit 1; }
command -v unzip >/dev/null || { echo "unzip not found on PATH"; exit 1; }
command -v zip >/dev/null || { echo "zip not found on PATH"; exit 1; }
command -v sha256sum >/dev/null || { echo "sha256sum not found on PATH"; exit 1; }

# The version lives in three places that all end up user-visible: this
# argument (which names the artifacts), wails.json (which is stamped into
# the Windows .exe properties), and the PKGBUILD (which decides what tarball
# Arch downloads). v0.1.2 shipped an .exe labelled 0.1.0 because nothing
# checked. Now a mismatch stops the release instead of shipping wrong.
wails_version="$(sed -n 's/.*"productVersion"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$APP_DIR/wails.json")"
pkgbuild_version="$(sed -n 's/^pkgver=\(.*\)$/\1/p' "$SCRIPT_DIR/PKGBUILD")"
pkgbuild_rclone_version="$(sed -n 's/^_rclonever=\(.*\)$/\1/p' "$SCRIPT_DIR/PKGBUILD")"
fail=0
if [[ "$wails_version" != "$VERSION" ]]; then
  echo "version mismatch: wails.json productVersion is '$wails_version', expected '$VERSION'" >&2
  fail=1
fi
if [[ "$pkgbuild_version" != "$VERSION" ]]; then
  echo "version mismatch: PKGBUILD pkgver is '$pkgbuild_version', expected '$VERSION'" >&2
  fail=1
fi
if [[ "$pkgbuild_rclone_version" != "$RCLONE_VERSION" ]]; then
  echo "rclone version mismatch: PKGBUILD has '$pkgbuild_rclone_version', release builder has '$RCLONE_VERSION'" >&2
  fail=1
fi
if ! grep -Fq "$RCLONE_ARCHIVE_SHA256" "$SCRIPT_DIR/PKGBUILD"; then
  echo "rclone checksum mismatch: PKGBUILD does not contain the release builder's pinned checksum" >&2
  fail=1
fi
actual_copying_checksum="$(sha256sum "$SCRIPT_DIR/rclone-COPYING" | awk '{print $1}')"
if [[ "$actual_copying_checksum" != "$RCLONE_COPYING_SHA256" ]]; then
  echo "rclone license mismatch: packaging/rclone-COPYING is not the pinned v$RCLONE_VERSION notice" >&2
  fail=1
fi
[[ $fail -eq 0 ]] || { echo "Resolve release metadata mismatches and commit before tagging." >&2; exit 1; }

# Mixing a dirty working tree into binaries while archiving HEAD would make the
# source artifact impossible to use to reproduce the release.
if ! git -C "$REPO_ROOT" diff --quiet \
  || ! git -C "$REPO_ROOT" diff --cached --quiet \
  || [[ -n "$(git -C "$REPO_ROOT" ls-files --others --exclude-standard)" ]]; then
  echo "release builds require a clean Git checkout" >&2
  exit 1
fi

rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

RCLONE_WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$RCLONE_WORK_DIR"' EXIT

echo "==> Fetching verified rclone $RCLONE_VERSION for Linux"
curl --fail --location --silent --show-error \
  --output "$RCLONE_WORK_DIR/$RCLONE_ARCHIVE" \
  "$RCLONE_BASE_URL/$RCLONE_ARCHIVE"
curl --fail --location --silent --show-error \
  --output "$RCLONE_WORK_DIR/SHA256SUMS" \
  "$RCLONE_BASE_URL/SHA256SUMS"
manifest_checksum="$(awk -v archive="$RCLONE_ARCHIVE" '$2 == archive { print $1 }' \
  "$RCLONE_WORK_DIR/SHA256SUMS")"
if [[ "$manifest_checksum" != "$RCLONE_ARCHIVE_SHA256" ]]; then
  echo "rclone checksum manifest did not contain the pinned checksum" >&2
  exit 1
fi
( cd "$RCLONE_WORK_DIR" \
  && printf '%s  %s\n' "$RCLONE_ARCHIVE_SHA256" "$RCLONE_ARCHIVE" \
  | sha256sum --check --strict - )
unzip -q "$RCLONE_WORK_DIR/$RCLONE_ARCHIVE" -d "$RCLONE_WORK_DIR"
RCLONE_EXTRACTED_DIR="$RCLONE_WORK_DIR/rclone-v${RCLONE_VERSION}-linux-amd64"
[[ -x "$RCLONE_EXTRACTED_DIR/rclone" ]] \
  || { echo "verified rclone archive did not contain its executable" >&2; exit 1; }
"$RCLONE_EXTRACTED_DIR/rclone" version | grep -q "rclone v${RCLONE_VERSION}"
install -Dm755 "$RCLONE_EXTRACTED_DIR/rclone" "$APP_DIR/build/bin/rclone"

cd "$APP_DIR"

echo "==> Building Linux binary"
wails build -m -nosyncgomod -tags webkit2_41
cp build/bin/shogun2sync "$DIST_DIR/shogun2sync-linux-amd64"
chmod +x "$DIST_DIR/shogun2sync-linux-amd64"

echo "==> Building recommended Linux bundle (app + private rclone runtime)"
LINUX_BUNDLE_DIR="$RCLONE_WORK_DIR/shogun2sync-linux-amd64"
mkdir -p "$LINUX_BUNDLE_DIR"
install -m755 build/bin/shogun2sync "$LINUX_BUNDLE_DIR/shogun2sync"
install -m755 build/bin/rclone "$LINUX_BUNDLE_DIR/rclone"
install -m644 packaging/rclone-COPYING "$LINUX_BUNDLE_DIR/rclone-COPYING"
SOURCE_DATE_EPOCH="$(git -C "$REPO_ROOT" show -s --format=%ct HEAD)"
tar -C "$RCLONE_WORK_DIR" --sort=name --mtime="@$SOURCE_DATE_EPOCH" \
  --owner=0 --group=0 --numeric-owner \
  -czf "$DIST_DIR/shogun2sync-linux-amd64.tar.gz" \
  shogun2sync-linux-amd64

echo "==> Building Windows binary (portable .exe, no installer)"
wails build -m -nosyncgomod -platform windows/amd64
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
# which is what the README tells them they can do. The checked-in file is a
# pre-release template; replace its first SKIP with the exact source archive
# hash so makepkg verifies both downloads.
source_checksum="$(sha256sum "$DIST_DIR/shogun2-sync-$VERSION-src.tar.gz" | awk '{print $1}')"
sed "0,/'SKIP'/s//'$source_checksum'/" "$SCRIPT_DIR/PKGBUILD" > "$DIST_DIR/PKGBUILD"
grep -Fq "'$source_checksum'" "$DIST_DIR/PKGBUILD"

echo "==> Writing checksums"
# SHA256SUMS is excluded by name: the redirection creates it before find
# runs, so it would otherwise list a checksum of its own empty self and
# fail `sha256sum -c`.
( cd "$DIST_DIR" \
  && find . -maxdepth 1 -type f ! -name SHA256SUMS -printf '%P\n' \
  | sort | xargs sha256sum -- > SHA256SUMS )

echo "==> Done. Artifacts in $DIST_DIR:"
ls -la "$DIST_DIR"
