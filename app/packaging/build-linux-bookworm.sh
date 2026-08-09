#!/usr/bin/env bash
# Builds the one Linux executable on the oldest supported ABI baseline.
# Every Linux package receives this exact binary, so Debian 12 compatibility
# cannot be accidentally lost when CI's host Ubuntu image moves forward.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
APP_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="$(cd "$APP_DIR/.." && pwd)"
# The image is shared with the runtime bundler: the binary and the GTK/WebKit
# stack that ships beside it must come from one ABI baseline.
# shellcheck source=/dev/null
source "$SCRIPT_DIR/linux-runtime.env"
GO_VERSION="1.25.0"
GO_ARCHIVE="go${GO_VERSION}.linux-amd64.tar.gz"
GO_ARCHIVE_SHA256="2852af0cb20a13139b3448992e69b868e50ed0f8a1e5940ee1de9e19a123b613"

[[ -s "$APP_DIR/frontend/dist/index.html" ]] || {
  echo "frontend/dist is missing; run npm ci and npm run build first" >&2
  exit 1
}

if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  CONTAINER_ENGINE=docker
elif command -v podman >/dev/null 2>&1 && podman info >/dev/null 2>&1; then
  CONTAINER_ENGINE=podman
else
  echo "docker or podman is required for the Debian 12 Linux build" >&2
  exit 1
fi

echo "==> Building Linux binary on the pinned Debian 12 baseline"
# The single-quoted program is deliberately expanded by bash inside the
# container using the environment variables passed above.
# shellcheck disable=SC2016
"$CONTAINER_ENGINE" run --rm \
  -e GO_VERSION="$GO_VERSION" \
  -e GO_ARCHIVE="$GO_ARCHIVE" \
  -e GO_ARCHIVE_SHA256="$GO_ARCHIVE_SHA256" \
  -e HOST_UID="$(id -u)" \
  -e HOST_GID="$(id -g)" \
  -v "$REPO_ROOT:/workspace" \
  -w /workspace/app \
  "$BOOKWORM_IMAGE" \
  bash -euo pipefail -c '
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq --no-install-recommends \
      ca-certificates curl gcc libc6-dev libgtk-3-dev \
      libwebkit2gtk-4.1-dev pkg-config tar
    curl --fail --location --silent --show-error \
      --output "/tmp/$GO_ARCHIVE" "https://go.dev/dl/$GO_ARCHIVE"
    printf "%s  %s\n" "$GO_ARCHIVE_SHA256" "/tmp/$GO_ARCHIVE" \
      | sha256sum --check --strict -
    tar -C /usr/local -xzf "/tmp/$GO_ARCHIVE"
    export PATH="/usr/local/go/bin:$PATH"
    export CGO_ENABLED=1
    go build -trimpath -tags "desktop,production,webkit2_41" \
      -ldflags "-s -w" -o build/bin/shogun2sync .
    ldd build/bin/shogun2sync | tee /tmp/shogun2sync.ldd
    if grep -Fq "not found" /tmp/shogun2sync.ldd; then
      exit 1
    fi
    # go build creates build/bin as root when the directory does not exist yet,
    # which is the normal case on a fresh CI checkout because build/bin is
    # gitignored. Chown the directory too, or the next unprivileged step cannot
    # stage rclone and the notices beside the binary.
    chown "$HOST_UID:$HOST_GID" build/bin build/bin/shogun2sync
  '

[[ -x "$APP_DIR/build/bin/shogun2sync" ]] \
  || { echo "Debian 12 builder did not produce shogun2sync" >&2; exit 1; }
echo "==> Debian 12 Linux binary verified: $APP_DIR/build/bin/shogun2sync"
