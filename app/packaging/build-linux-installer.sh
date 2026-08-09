#!/usr/bin/env bash
# Builds the generic x86-64 Linux fallback as one self-extracting installer.
# The app and private rclone payload are embedded; the startup script resolves
# distro-owned GTK/WebKit dependencies through apt, dnf, or pacman.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$APP_DIR/.." && pwd)"
MAKESELF_VERSION="2.7.1"
MAKESELF_URL="https://github.com/megastep/makeself/releases/download/release-${MAKESELF_VERSION}/makeself-${MAKESELF_VERSION}.run"
MAKESELF_SHA256="42f51a114ff671623e689ac4b74c444e9fc5bf8906dd88c82dc9e04e0b3938d1"

if [[ $# -ne 1 ]]; then
  echo "usage: $(basename "$0") <output.run>" >&2
  exit 2
fi
OUTPUT="$(realpath -m "$1")"

for tool in curl install realpath sha256sum; do
  command -v "$tool" >/dev/null \
    || { echo "$tool is required to build the Linux installer" >&2; exit 1; }
done

required_files=(
  "$APP_DIR/build/bin/shogun2sync"
  "$APP_DIR/build/bin/rclone"
  "$APP_DIR/build/bin/GO-THIRD-PARTY-NOTICES.txt"
  "$APP_DIR/build/runtime/shogun2sync.sh"
  "$APP_DIR/build/runtime/lib/libwebkit2gtk-4.1.so.0"
  "$APP_DIR/build/runtime/WEBKITGTK-NOTICE.txt"
  "$SCRIPT_DIR/install-linux.sh"
  "$SCRIPT_DIR/rclone-COPYING"
  "$SCRIPT_DIR/shogun2sync.desktop"
  "$SCRIPT_DIR/appicon-512.png"
  "$REPO_ROOT/LICENSE"
)
for required in "${required_files[@]}"; do
  [[ -s "$required" ]] \
    || { echo "Linux installer input is missing: $required" >&2; exit 1; }
done

WORK_DIR="$(mktemp -d)"
cleanup() { rm -rf "$WORK_DIR"; }
trap cleanup EXIT
BUNDLE_DIR="$WORK_DIR/shogun2sync-linux-amd64"
MAKESELF_RUN="$WORK_DIR/makeself.run"
MAKESELF_DIR="$WORK_DIR/makeself"

mkdir -p "$BUNDLE_DIR" "$(dirname "$OUTPUT")"
install -m755 "$APP_DIR/build/bin/shogun2sync" "$BUNDLE_DIR/shogun2sync"
install -m755 "$APP_DIR/build/bin/rclone" "$BUNDLE_DIR/rclone"
install -m755 "$SCRIPT_DIR/install-linux.sh" "$BUNDLE_DIR/install.sh"
install -m644 "$APP_DIR/build/bin/GO-THIRD-PARTY-NOTICES.txt" \
  "$BUNDLE_DIR/GO-THIRD-PARTY-NOTICES.txt"
install -m644 "$SCRIPT_DIR/rclone-COPYING" "$BUNDLE_DIR/rclone-COPYING"
install -m644 "$SCRIPT_DIR/shogun2sync.desktop" "$BUNDLE_DIR/shogun2sync.desktop"
install -m644 "$SCRIPT_DIR/appicon-512.png" "$BUNDLE_DIR/appicon-512.png"
install -m644 "$REPO_ROOT/LICENSE" "$BUNDLE_DIR/LICENSE"

# The private GTK/WebKitGTK stack is what makes this installer work without
# distribution repositories, so carry it verbatim.
echo "==> Adding the bundled GUI runtime"
cp -a "$APP_DIR/build/runtime" "$BUNDLE_DIR/runtime"
[[ -x "$BUNDLE_DIR/runtime/shogun2sync.sh" ]] \
  || { echo "the runtime bundle has no launcher" >&2; exit 1; }

echo "==> Fetching verified Makeself $MAKESELF_VERSION"
curl --fail --location --silent --show-error \
  --output "$MAKESELF_RUN" "$MAKESELF_URL"
printf '%s  %s\n' "$MAKESELF_SHA256" "$MAKESELF_RUN" \
  | sha256sum --check --strict -
sh "$MAKESELF_RUN" --quiet --target "$MAKESELF_DIR" --noexec

echo "==> Building one-file Linux installer"
# xz rather than gzip: the payload is now a whole GUI stack, and xz-utils is
# present on every supported distribution.
sh "$MAKESELF_DIR/makeself.sh" --quiet --xz --sha256 --nox11 \
  "$BUNDLE_DIR" "$OUTPUT" "Shogun 2 Save Sync" ./install.sh
chmod 0755 "$OUTPUT"
sh "$OUTPUT" --check
echo "==> Linux installer verified: $OUTPUT"
