#!/usr/bin/env bash
# Builds every distributable artifact: a self-contained Windows installer,
# Linux .deb/.rpm/Arch packages, a one-file automatic Linux installer,
# and a source tarball. Run from the repo root or from app/.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$APP_DIR/.." && pwd)"
DIST_DIR="$REPO_ROOT/dist"
RCLONE_VERSION="1.74.4"
RCLONE_ARCHIVE="rclone-v${RCLONE_VERSION}-linux-amd64.zip"
RCLONE_ARCHIVE_SHA256="fe435e0c36228e7c2f116a8701f01127bb1f694005fc11d1f27186c8bca4115d"
# Windows ships the same rclone version from the same signed manifest. Its
# own pinned digest exists so a swapped or truncated download fails here
# instead of producing an installer that cannot sync on a clean machine.
RCLONE_WINDOWS_ARCHIVE="rclone-v${RCLONE_VERSION}-windows-amd64.zip"
RCLONE_WINDOWS_ARCHIVE_SHA256="ef097ef9de37a57feb7d9f9c7afb34148ad3c65be8025f1d8f7f521554a701ea"
RCLONE_COPYING_SHA256="8cd2e9e750b90a04b7d82dbbca3930c696ae0309d7c10464f90a44f45754cd04"
RCLONE_BASE_URL="https://downloads.rclone.org/v${RCLONE_VERSION}"

if [[ $# -lt 1 ]]; then
  echo "usage: $(basename "$0") <version>   (e.g. 1.0.1)" >&2
  exit 2
fi
VERSION="$1"

if [[ ! "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "version must be a plain semantic version (for example, 1.0.1)" >&2
  exit 2
fi

command -v wails >/dev/null || { echo "wails CLI not found on PATH"; exit 1; }
command -v nfpm >/dev/null || { echo "nfpm not found on PATH"; exit 1; }
command -v curl >/dev/null || { echo "curl not found on PATH"; exit 1; }
command -v npm >/dev/null || { echo "npm not found on PATH"; exit 1; }
command -v unzip >/dev/null || { echo "unzip not found on PATH"; exit 1; }
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

echo "==> Fetching verified rclone $RCLONE_VERSION for Linux and Windows"
curl --fail --location --silent --show-error \
  --output "$RCLONE_WORK_DIR/SHA256SUMS" \
  "$RCLONE_BASE_URL/SHA256SUMS"
# Both platforms take the same two-step proof: the pinned digest must be the
# one rclone published for that exact file name, and the bytes on disk must
# then match the pinned digest. Either check failing stops the release.
fetch_verified_rclone() {
  local archive="$1" expected="$2" manifest_checksum
  curl --fail --location --silent --show-error \
    --output "$RCLONE_WORK_DIR/$archive" \
    "$RCLONE_BASE_URL/$archive"
  manifest_checksum="$(awk -v archive="$archive" '$2 == archive { print $1 }' \
    "$RCLONE_WORK_DIR/SHA256SUMS")"
  if [[ "$manifest_checksum" != "$expected" ]]; then
    echo "rclone checksum manifest did not contain the pinned checksum for $archive" >&2
    exit 1
  fi
  ( cd "$RCLONE_WORK_DIR" \
    && printf '%s  %s\n' "$expected" "$archive" \
    | sha256sum --check --strict - )
  unzip -q "$RCLONE_WORK_DIR/$archive" -d "$RCLONE_WORK_DIR"
}

fetch_verified_rclone "$RCLONE_ARCHIVE" "$RCLONE_ARCHIVE_SHA256"
RCLONE_EXTRACTED_DIR="$RCLONE_WORK_DIR/rclone-v${RCLONE_VERSION}-linux-amd64"
[[ -x "$RCLONE_EXTRACTED_DIR/rclone" ]] \
  || { echo "verified rclone archive did not contain its executable" >&2; exit 1; }
"$RCLONE_EXTRACTED_DIR/rclone" version | grep -q "rclone v${RCLONE_VERSION}"
install -Dm755 "$RCLONE_EXTRACTED_DIR/rclone" "$APP_DIR/build/bin/rclone"

fetch_verified_rclone "$RCLONE_WINDOWS_ARCHIVE" "$RCLONE_WINDOWS_ARCHIVE_SHA256"
RCLONE_WINDOWS_EXTRACTED_DIR="$RCLONE_WORK_DIR/rclone-v${RCLONE_VERSION}-windows-amd64"
[[ -s "$RCLONE_WINDOWS_EXTRACTED_DIR/rclone.exe" ]] \
  || { echo "verified rclone archive did not contain rclone.exe" >&2; exit 1; }
# `rclone.exe version` cannot run on the Linux release host, so the Windows
# build's version is established by the verified digest plus the
# version-stamped archive directory, and confirmed once more by the version
# string linked into the executable itself.
grep -aq "v${RCLONE_VERSION}" "$RCLONE_WINDOWS_EXTRACTED_DIR/rclone.exe" \
  || { echo "verified rclone.exe does not carry the pinned version string" >&2; exit 1; }
install -Dm755 "$RCLONE_WINDOWS_EXTRACTED_DIR/rclone.exe" "$APP_DIR/build/bin/rclone.exe"

cd "$APP_DIR"

echo "==> Building frontend"
npm ci --prefix frontend
npm run build --prefix frontend

"$SCRIPT_DIR/build-linux-bookworm.sh"
"$SCRIPT_DIR/collect-go-notices.sh" \
  "$APP_DIR/build/bin/GO-THIRD-PARTY-NOTICES.txt" \
  "$APP_DIR/build/bin/shogun2sync"

# The one-file installer carries its own GTK/WebKitGTK, so the runtime has to
# exist before it can be packaged.
"$SCRIPT_DIR/build-linux-runtime.sh"

echo "==> Building one-file Linux installer"
"$SCRIPT_DIR/build-linux-installer.sh" \
  "$DIST_DIR/shogun2sync-linux-amd64.run"

# Never publish a .run that quietly depends on the host's GUI stack. This
# installs and launches it on four distributions with networking disabled.
"$SCRIPT_DIR/verify-linux-offline.sh" \
  "$DIST_DIR/shogun2sync-linux-amd64.run"

"$SCRIPT_DIR/build-windows-installer.sh"
WINDOWS_INSTALLER="build/bin/shogun2sync-amd64-installer.exe"
install -m644 "$WINDOWS_INSTALLER" \
  "$DIST_DIR/shogun2sync-windows-amd64-installer.exe"

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
# Write outside dist first so SHA256SUMS cannot accidentally include itself.
( cd "$DIST_DIR" \
  && find . -maxdepth 1 -type f -printf '%P\n' \
  | sort | xargs sha256sum -- > "$RCLONE_WORK_DIR/SHA256SUMS" )
install -m644 "$RCLONE_WORK_DIR/SHA256SUMS" "$DIST_DIR/SHA256SUMS"

echo "==> Done. Artifacts in $DIST_DIR:"
ls -la "$DIST_DIR"
