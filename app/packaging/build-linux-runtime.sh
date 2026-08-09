#!/usr/bin/env bash
# Host-side entry point for the private Linux GUI runtime. The bundle has to be
# assembled on the same Debian 12 baseline the binary is built on, so this runs
# linux-runtime-bundle.sh inside the identical pinned image.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$APP_DIR/.." && pwd)"

# shellcheck source=/dev/null
source "$SCRIPT_DIR/linux-runtime.env"

if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  CONTAINER_ENGINE=docker
elif command -v podman >/dev/null 2>&1 && podman info >/dev/null 2>&1; then
  CONTAINER_ENGINE=podman
else
  echo "docker or podman is required to build the Linux runtime" >&2
  exit 1
fi

echo "==> Assembling the private Linux GUI runtime on Debian 12"
"$CONTAINER_ENGINE" run --rm \
  -e RUNTIME_PREFIX="$RUNTIME_PREFIX" \
  -e HOST_UID="$(id -u)" \
  -e HOST_GID="$(id -g)" \
  -v "$REPO_ROOT:/workspace" \
  -w /workspace/app \
  "$BOOKWORM_IMAGE" \
  bash /workspace/app/packaging/linux-runtime-bundle.sh

BUNDLE="$APP_DIR/build/runtime"
for required in \
  "$BUNDLE/shogun2sync.sh" \
  "$BUNDLE/lib/libwebkit2gtk-4.1.so.0" \
  "$BUNDLE/lib/libgtk-3.so.0" \
  "$BUNDLE/lib/webkit2gtk-4.1/WebKitWebProcess" \
  "$BUNDLE/lib/webkit2gtk-4.1/WebKitNetworkProcess" \
  "$BUNDLE/lib/gio/modules/libgiognutls.so" \
  "$BUNDLE/lib/gdk-pixbuf-2.0/2.10.0/loaders.cache" \
  "$BUNDLE/share/glib-2.0/schemas/gschemas.compiled" \
  "$BUNDLE/WEBKITGTK-NOTICE.txt"; do
  [[ -e "$required" ]] \
    || { echo "the runtime bundle is missing $required" >&2; exit 1; }
done

# The notice must inventory nested modules and share data, not only the ldd
# closure. These packages only appear when those trees are recorded.
for package in \
  glib-networking \
  gsettings-desktop-schemas \
  shared-mime-info \
  adwaita-icon-theme \
  ca-certificates; do
  grep -Eq "^  ${package}[[:space:]]" "$BUNDLE/WEBKITGTK-NOTICE.txt" \
    || { echo "WEBKITGTK-NOTICE.txt is missing package: $package" >&2; exit 1; }
done

# A bundle that still points at Debian's own libexec directory would silently
# fall back to whatever the host happens to have, which is exactly how a
# "self-contained" build ships without being self-contained.
if grep -aFq "/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1" \
  "$BUNDLE/lib/libwebkit2gtk-4.1.so.0"; then
  echo "WebKitGTK still refers to Debian's helper directory" >&2
  exit 1
fi
grep -aFq "$RUNTIME_PREFIX/lib/webkit2gtk-4.1" \
  "$BUNDLE/lib/libwebkit2gtk-4.1.so.0" \
  || { echo "WebKitGTK was not repointed at $RUNTIME_PREFIX" >&2; exit 1; }

echo "==> Linux runtime verified: $BUNDLE"
