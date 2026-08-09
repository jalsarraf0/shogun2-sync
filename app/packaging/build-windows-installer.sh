#!/usr/bin/env bash
# Builds the x86-64 Windows installer with Microsoft's complete offline
# WebView2 runtime inside it. Used by both PR CI and the release builder so the
# dependency-complete path is tested before a tag is published.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
# shellcheck source=webview2-runtime.env
# shellcheck disable=SC1091
source "$SCRIPT_DIR/webview2-runtime.env"
NSIS_DIR="$APP_DIR/build/windows/installer"
NSIS_TOOLS_FILE="$NSIS_DIR/wails_tools.nsh"

for tool in 7z curl makensis openssl osslsigncode sha256sum wails; do
  command -v "$tool" >/dev/null \
    || { echo "$tool is required to build the offline Windows installer" >&2; exit 1; }
done

# rclone is a hard runtime dependency of the sync engine and Windows has no
# package manager to fall back on, so the installer must carry it. NSIS would
# otherwise fail deep inside makensis, or worse, a future refactor could drop
# the File line and ship an installer that silently cannot sync. The release
# builder fetches these into place from a checksum-verified upstream archive.
for input in "$APP_DIR/build/bin/rclone.exe" "$SCRIPT_DIR/rclone-COPYING"; do
  [[ -s "$input" ]] \
    || { echo "missing required Windows installer input: $input" >&2; exit 1; }
done

WORK_DIR="$(mktemp -d)"
cp "$NSIS_TOOLS_FILE" "$WORK_DIR/wails_tools.nsh.original"
cleanup() {
  # Wails resolves its template into this tracked file while generating NSIS.
  install -m644 "$WORK_DIR/wails_tools.nsh.original" "$NSIS_TOOLS_FILE"
  rm -f "$NSIS_DIR/tmp/MicrosoftEdgeWebview2Setup.exe"
  rm -f "$NSIS_DIR/tmp/$WEBVIEW2_RUNTIME_FILE"
  rm -f "$NSIS_DIR/tmp/WebView2-LICENSE.html"
  rm -f "$NSIS_DIR/tmp/GO-THIRD-PARTY-NOTICES.txt"
  rmdir "$NSIS_DIR/tmp" 2>/dev/null || true
  rm -rf "$WORK_DIR"
}
trap cleanup EXIT

echo "==> Fetching verified WebView2 $WEBVIEW2_RUNTIME_VERSION offline runtime"
curl --fail --location --silent --show-error \
  --output "$WORK_DIR/$WEBVIEW2_RUNTIME_FILE" \
  "$WEBVIEW2_RUNTIME_URL"
"$SCRIPT_DIR/verify-webview2.sh" "$WORK_DIR/$WEBVIEW2_RUNTIME_FILE"
install -Dm644 "$WORK_DIR/$WEBVIEW2_RUNTIME_FILE" \
  "$NSIS_DIR/tmp/$WEBVIEW2_RUNTIME_FILE"
"$SCRIPT_DIR/fetch-webview2-license.sh" "$NSIS_DIR/tmp/WebView2-LICENSE.html"

echo "==> Building Windows binary and fully offline one-step installer"
cd "$APP_DIR"
# -webview2 error is what makes this build offline. Wails' default strategy is
# "download": if the runtime check ever fails at startup the app fetches the
# Microsoft bootstrapper over the network. The installer already guarantees the
# runtime, so trade that fallback for a local error dialog and no network path.
# Both builds carry the flag because the build tag changes the linked module
# set, and the notices must describe the binary that actually ships.
WAILS_BUILD_FLAGS=(-m -nosyncgomod -platform windows/amd64 -webview2 error)

# Build once to obtain the exact target-specific linked-module list, collect
# its notices, then package the same target into NSIS.
wails build "${WAILS_BUILD_FLAGS[@]}"
"$SCRIPT_DIR/collect-go-notices.sh" \
  "$NSIS_DIR/tmp/GO-THIRD-PARTY-NOTICES.txt" \
  "$APP_DIR/build/bin/shogun2sync.exe"
wails build "${WAILS_BUILD_FLAGS[@]}" -nsis -installscope user

# Prove the tag took effect rather than trusting the flag. The download strategy
# is the only thing that links the fwlink bootstrapper URL into the binary.
if grep -aFq 'go.microsoft.com/fwlink' "$APP_DIR/build/bin/shogun2sync.exe"; then
  echo "the Windows binary still carries the WebView2 download fallback" >&2
  exit 1
fi

INSTALLER="$APP_DIR/build/bin/shogun2sync-amd64-installer.exe"
[[ -s "$INSTALLER" ]] \
  || { echo "Wails did not produce the expected Windows installer" >&2; exit 1; }
7z l "$INSTALLER" | grep -Fq "$WEBVIEW2_RUNTIME_FILE" \
  || { echo "the final Windows installer does not contain WebView2" >&2; exit 1; }
7z l "$INSTALLER" | grep -Fq 'LICENSE.txt' \
  || { echo "the final Windows installer does not contain the app license" >&2; exit 1; }
7z l "$INSTALLER" | grep -Fq 'WebView2-LICENSE.html' \
  || { echo "the final Windows installer does not contain WebView2 terms" >&2; exit 1; }
7z l "$INSTALLER" | grep -Fq 'GO-THIRD-PARTY-NOTICES.txt' \
  || { echo "the final Windows installer does not contain Go notices" >&2; exit 1; }
7z l "$INSTALLER" | grep -Fq 'rclone.exe' \
  || { echo "the final Windows installer does not contain rclone" >&2; exit 1; }
7z l "$INSTALLER" | grep -Fq 'rclone-COPYING.txt' \
  || { echo "the final Windows installer does not contain rclone's license" >&2; exit 1; }

echo "==> Offline Windows installer verified: $INSTALLER"
