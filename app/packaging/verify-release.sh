#!/usr/bin/env bash
# Verifies that a completed release contains only dependency-complete install
# paths and that every native package declares and carries the expected files.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DIST_DIR="${2:-$REPO_ROOT/dist}"

if [[ $# -lt 1 || ! "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "usage: $(basename "$0") <version> [dist-directory]" >&2
  exit 2
fi
VERSION="$1"

for tool in 7z sha256sum tar; do
  command -v "$tool" >/dev/null \
    || { echo "$tool is required to verify release packages" >&2; exit 1; }
done

WINDOWS="$DIST_DIR/shogun2sync-windows-amd64-installer.exe"
LINUX_INSTALLER="$DIST_DIR/shogun2sync-linux-amd64.run"
SOURCE="$DIST_DIR/shogun2-sync-${VERSION}-src.tar.gz"

for artifact in "$WINDOWS" "$LINUX_INSTALLER" "$SOURCE" \
    "$DIST_DIR/PKGBUILD" "$DIST_DIR/SHA256SUMS"; do
  [[ -s "$artifact" ]] || { echo "missing release artifact: $artifact" >&2; exit 1; }
done

echo "==> Verifying the offline Windows installer"
# Listing the NSIS payload proves that the full standalone runtime, rather
# than Wails' small network bootstrapper, is actually inside the final EXE.
7z l "$WINDOWS" | grep -Fq 'MicrosoftEdgeWebView2RuntimeInstallerX64.exe'
7z l "$WINDOWS" | grep -Fq 'LICENSE.txt'
7z l "$WINDOWS" | grep -Fq 'WebView2-LICENSE.html'
7z l "$WINDOWS" | grep -Fq 'GO-THIRD-PARTY-NOTICES.txt'
# Windows has no package manager to supply rclone, so an installer without it
# is not a dependency-complete install path even though it starts fine.
7z l "$WINDOWS" | grep -Fq 'rclone.exe'
7z l "$WINDOWS" | grep -Fq 'rclone-COPYING.txt'
windows_extract="$(mktemp -d)"
trap 'rm -rf "$windows_extract"' EXIT
# `$PLUGINSDIR` is a literal path recorded inside the NSIS archive.
# shellcheck disable=SC2016
7z e -y -o"$windows_extract" "$WINDOWS" \
  '$PLUGINSDIR/webview2bootstrapper/MicrosoftEdgeWebView2RuntimeInstallerX64.exe' \
  >/dev/null
"$SCRIPT_DIR/verify-webview2.sh" \
  "$windows_extract/MicrosoftEdgeWebView2RuntimeInstallerX64.exe"

"$SCRIPT_DIR/verify-linux-packages.sh" "$VERSION" "$DIST_DIR"

echo "==> Verifying the one-file Linux installer"
sh "$LINUX_INSTALLER" --check
linux_extract="$(mktemp -d)"
trap 'rm -rf "$windows_extract" "$linux_extract"' EXIT
sh "$LINUX_INSTALLER" --quiet --target "$linux_extract" --noexec
for file in shogun2sync rclone install.sh LICENSE rclone-COPYING \
    GO-THIRD-PARTY-NOTICES.txt shogun2sync.desktop appicon-512.png; do
  [[ -s "$linux_extract/$file" ]] \
    || { echo "Linux installer file missing: $file" >&2; exit 1; }
done

echo "==> Verifying checksums"
(
  cd "$DIST_DIR"
  sha256sum --check --strict SHA256SUMS
)

echo "All release installers and packages passed dependency verification."
