#!/usr/bin/env bash
# Proves the generic Linux installer really is self-contained.
#
# This is the gate that matters for the bundled GUI runtime. A bundle that is
# subtly incomplete still works on a machine that happens to have GTK and
# WebKitGTK installed, because the dynamic linker quietly falls back to the
# host's copies. So each distribution is given the base desktop libraries the
# bundle deliberately does not carry -- and nothing else -- and then the install
# and launch run with networking switched off entirely.
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $(basename "$0") <shogun2sync-linux-amd64.run>" >&2
  exit 2
fi
RUN_FILE="$(realpath "$1")"
[[ -s "$RUN_FILE" ]] || { echo "no such installer: $RUN_FILE" >&2; exit 1; }

if command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
  CONTAINER_ENGINE=docker
elif command -v podman >/dev/null 2>&1 && podman info >/dev/null 2>&1; then
  CONTAINER_ENGINE=podman
else
  echo "docker or podman is required to verify the Linux installer" >&2
  exit 1
fi

WORK_DIR="$(mktemp -d)"
cleanup() { rm -rf "$WORK_DIR"; }
trap cleanup EXIT

# The libraries every graphical desktop provides and the bundle intentionally
# leaves to the host: fonts, X11, Wayland, and the graphics driver.
cat > "$WORK_DIR/Dockerfile.debian" <<'EOF'
FROM debian:12
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends \
      xvfb dbus-daemon procps xz-utils ca-certificates \
      libgl1-mesa-dri libglx-mesa0 libegl1 libgles2 fontconfig fonts-dejavu-core \
      libxcb-render0 libxcb-shm0 libwayland-client0 libwayland-cursor0 \
      libwayland-egl1 libwayland-server0 libxtst6 libxcomposite1 libxdamage1 \
      libxrandr2 libxcursor1 libxi6 libxinerama1 \
 && rm -rf /var/lib/apt/lists/*
EOF

cat > "$WORK_DIR/Dockerfile.ubuntu" <<'EOF'
FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -qq && apt-get install -y -qq --no-install-recommends \
      xvfb dbus-daemon procps xz-utils ca-certificates \
      libgl1-mesa-dri libglx-mesa0 libegl1 libgles2 fontconfig fonts-dejavu-core \
      libxcb-render0 libxcb-shm0 libwayland-client0 libwayland-cursor0 \
      libwayland-egl1 libwayland-server0 libxtst6 libxcomposite1 libxdamage1 \
      libxrandr2 libxcursor1 libxi6 libxinerama1 \
 && rm -rf /var/lib/apt/lists/*
EOF

cat > "$WORK_DIR/Dockerfile.fedora" <<'EOF'
FROM fedora:44
RUN dnf install -y -q xorg-x11-server-Xvfb dbus-daemon procps-ng xz \
      mesa-libGL mesa-libEGL mesa-libGLES mesa-dri-drivers fontconfig \
      dejavu-sans-fonts libstdc++ ca-certificates \
      libwayland-client libwayland-cursor libwayland-egl libwayland-server \
      libXtst libXcomposite libXdamage libXrandr libXcursor libXi libXinerama \
 && dnf clean all
EOF

cat > "$WORK_DIR/Dockerfile.arch" <<'EOF'
FROM archlinux:base
RUN pacman -Sy --noconfirm --quiet xorg-server-xvfb dbus procps-ng xz \
      mesa libglvnd fontconfig ttf-dejavu wayland ca-certificates \
      libxtst libxcomposite libxdamage libxrandr libxcursor libxi libxinerama \
 && pacman -Scc --noconfirm >/dev/null
EOF

# Runs inside the offline container.
cat > "$WORK_DIR/check.sh" <<'EOF'
set -euo pipefail

if getent hosts github.com >/dev/null 2>&1; then
  echo "this check must run with networking disabled" >&2
  exit 1
fi

# If the host already had the GUI stack, a broken bundle would still appear to
# work, so refuse to report success from a meaningless environment.
if ls /usr/lib*/libwebkit2gtk-4.1.so.0 /usr/lib/*/libwebkit2gtk-4.1.so.0 \
     >/dev/null 2>&1; then
  echo "the test image must not provide WebKitGTK" >&2
  exit 1
fi

sh /work/installer.run --quiet
test -x /opt/shogun2sync/shogun2sync
test -x /opt/shogun2sync/rclone
test -x /opt/shogun2sync/shogun2sync.sh
test -f /usr/local/share/licenses/shogun2sync/WEBKITGTK-NOTICE.txt
[ "$(readlink /usr/local/bin/shogun2sync)" = /opt/shogun2sync/shogun2sync.sh ]

/opt/shogun2sync/rclone version | grep -Fq "rclone v"

Xvfb :99 -screen 0 1280x800x24 >/dev/null 2>&1 &
sleep 2
export DISPLAY=:99 HOME=/root XDG_RUNTIME_DIR=/tmp/xdg
mkdir -p "$XDG_RUNTIME_DIR"
chmod 700 "$XDG_RUNTIME_DIR"
eval "export $(dbus-daemon --session --fork --print-address=1 \
  | sed 's/^/DBUS_SESSION_BUS_ADDRESS=/')"

# Software rendering because CI has no GPU; this is about library resolution,
# not about the renderer.
WEBKIT_DISABLE_COMPOSITING_MODE=1 LIBGL_ALWAYS_SOFTWARE=1 \
  shogun2sync >/tmp/app.log 2>&1 &
app_pid=$!
sleep 20

if ! kill -0 "$app_pid" 2>/dev/null; then
  echo "the app exited instead of starting:" >&2
  cat /tmp/app.log >&2
  exit 1
fi

# WebKitGTK is multi-process. If only the UI process is alive the helper
# binaries were not found, which is the failure the path rewrite exists to fix.
for helper in WebKitWebProcess WebKitNetworkProcess; do
  pgrep -f "$helper" >/dev/null \
    || { echo "$helper did not start"; cat /tmp/app.log; exit 1; }
done

# The decisive assertion: the running process must have mapped the bundled
# library, not one belonging to the host.
loaded="$(tr '\0' '\n' < "/proc/$app_pid/maps" \
  | grep -o '[^ ]*libwebkit2gtk[^ ]*' | sort -u)"
echo "  loaded: $loaded"
case "$loaded" in
  /opt/shogun2sync/lib/libwebkit2gtk-4.1.so.0) ;;
  *) echo "the app did not use its bundled WebKitGTK" >&2; exit 1 ;;
esac

kill "$app_pid" 2>/dev/null || true

sh /work/installer.run --quiet -- --uninstall
test ! -e /opt/shogun2sync
test ! -e /usr/local/bin/shogun2sync
echo "  offline install, launch and uninstall all passed"
EOF

install -m644 "$RUN_FILE" "$WORK_DIR/installer.run"

for distribution in debian ubuntu fedora arch; do
  echo "==> Verifying the offline Linux installer on $distribution"
  "$CONTAINER_ENGINE" build --quiet \
    --tag "shogun2sync-offline-$distribution" \
    --file "$WORK_DIR/Dockerfile.$distribution" "$WORK_DIR" >/dev/null
  # --network none is what turns this from a smoke test into a guarantee.
  "$CONTAINER_ENGINE" run --rm --network none \
    -v "$WORK_DIR:/work:ro" \
    "shogun2sync-offline-$distribution" \
    bash /work/check.sh
done

echo "==> The Linux installer is self-contained on every supported distribution"
