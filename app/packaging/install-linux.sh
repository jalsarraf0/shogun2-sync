#!/usr/bin/env bash
# Installs Shogun 2 Save Sync and its private GTK/WebKitGTK runtime. This script
# is embedded in the release `.run` file.
#
# Nothing here contacts a package manager or the network: the bundle carries the
# whole GUI stack, so the install works on a machine with no repository access.
# The only things taken from the host are the pieces that must be the host's --
# glibc, the graphics driver, X11/Wayland and fontconfig.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_ROOT="${SHOGUN2SYNC_INSTALL_ROOT:-}"

# Must match RUNTIME_PREFIX in packaging/linux-runtime.env: WebKitGTK's helper
# path is patched into the shipped library, so this location is not negotiable.
RUNTIME_PREFIX="/opt/shogun2sync"

APP_DIR="$INSTALL_ROOT$RUNTIME_PREFIX"
LAUNCHER="$APP_DIR/shogun2sync.sh"
BIN_PATH="$INSTALL_ROOT/usr/local/bin/shogun2sync"
DESKTOP_PATH="$INSTALL_ROOT/usr/local/share/applications/shogun2sync.desktop"
ICON_PATH="$INSTALL_ROOT/usr/local/share/icons/hicolor/512x512/apps/shogun2sync.png"
LICENSE_DIR="$INSTALL_ROOT/usr/local/share/licenses/shogun2sync"

usage() {
  cat <<'EOF'
Usage: ./install.sh [--uninstall]

With no option, installs Shogun 2 Save Sync and its bundled GUI runtime. No
package manager or network access is used. Use --uninstall to remove the
application later. Player saves, cloud files, and app settings are never
removed.
EOF
}

run_as_root() {
  if [[ -n "$INSTALL_ROOT" || ${EUID:-$(id -u)} -eq 0 ]]; then
    "$@"
    return
  fi
  if ! command -v sudo >/dev/null 2>&1; then
    echo "Administrator access is required, but sudo is not installed." >&2
    echo "Run this installer as root or install the native package for your distribution." >&2
    exit 1
  fi
  sudo "$@"
}

# The bundle deliberately does not carry glibc, the graphics stack, X11 or
# fontconfig. Any graphical desktop already has them, but say so plainly rather
# than letting the app fail later with a dynamic-linker error.
check_host_prerequisites() {
  local missing=()
  local probe
  for probe in libfontconfig.so.1 libfreetype.so.6 libX11.so.6 libEGL.so.1; do
    ldconfig -p 2>/dev/null | grep -Fq "$probe" || missing+=("$probe")
  done
  if [[ ${#missing[@]} -gt 0 ]]; then
    cat >&2 <<EOF
This machine is missing basic desktop libraries that every graphical Linux
install provides: ${missing[*]}

Shogun 2 Save Sync bundles GTK and WebKitGTK, but it deliberately uses the
host's fonts and graphics driver. Install your distribution's desktop base
packages, then run this installer again.
EOF
    exit 1
  fi
}

install_app() {
  local required
  for required in shogun2sync rclone rclone-COPYING LICENSE \
      GO-THIRD-PARTY-NOTICES.txt \
      shogun2sync.desktop appicon-512.png runtime; do
    if [[ ! -e "$SCRIPT_DIR/$required" ]]; then
      echo "This release bundle is incomplete: $required is missing." >&2
      exit 1
    fi
  done

  case "$(uname -s)-$(uname -m)" in
    Linux-x86_64|Linux-amd64) ;;
    *)
      echo "This release supports 64-bit x86 Linux only; detected $(uname -s)-$(uname -m)." >&2
      exit 1
      ;;
  esac

  [[ -n "$INSTALL_ROOT" ]] || check_host_prerequisites

  echo "==> Installing Shogun 2 Save Sync and its bundled runtime"
  # Replace any previous runtime wholesale. Leaving stale libraries behind is
  # how a half-upgraded bundle ends up mixing two WebKit versions.
  run_as_root rm -rf "$APP_DIR/lib" "$APP_DIR/share"
  run_as_root install -d "$APP_DIR"
  run_as_root cp -a "$SCRIPT_DIR/runtime/lib" "$SCRIPT_DIR/runtime/share" "$APP_DIR/"
  run_as_root install -Dm755 "$SCRIPT_DIR/runtime/shogun2sync.sh" "$LAUNCHER"

  run_as_root install -Dm755 "$SCRIPT_DIR/shogun2sync" "$APP_DIR/shogun2sync"
  run_as_root install -Dm755 "$SCRIPT_DIR/rclone" "$APP_DIR/rclone"
  run_as_root install -Dm644 "$SCRIPT_DIR/LICENSE" "$LICENSE_DIR/LICENSE"
  run_as_root install -Dm644 "$SCRIPT_DIR/rclone-COPYING" "$LICENSE_DIR/rclone-COPYING"
  run_as_root install -Dm644 "$SCRIPT_DIR/GO-THIRD-PARTY-NOTICES.txt" \
    "$LICENSE_DIR/GO-THIRD-PARTY-NOTICES.txt"
  run_as_root install -Dm644 "$SCRIPT_DIR/runtime/WEBKITGTK-NOTICE.txt" \
    "$LICENSE_DIR/WEBKITGTK-NOTICE.txt"
  run_as_root install -Dm644 "$SCRIPT_DIR/appicon-512.png" "$ICON_PATH"
  run_as_root install -d "$(dirname "$BIN_PATH")" "$(dirname "$DESKTOP_PATH")"

  # Both entry points must go through the launcher: running the raw binary would
  # pick up whatever GTK/WebKit the host happens to have, or none at all.
  run_as_root ln -sfn "$RUNTIME_PREFIX/shogun2sync.sh" "$BIN_PATH"

  local desktop_tmp
  desktop_tmp="$(mktemp)"
  trap 'rm -f "$desktop_tmp"' RETURN
  sed "s|^Exec=.*|Exec=$RUNTIME_PREFIX/shogun2sync.sh|" \
    "$SCRIPT_DIR/shogun2sync.desktop" > "$desktop_tmp"
  run_as_root install -m644 "$desktop_tmp" "$DESKTOP_PATH"
  rm -f "$desktop_tmp"
  trap - RETURN

  if [[ -z "$INSTALL_ROOT" ]] && command -v update-desktop-database >/dev/null 2>&1; then
    run_as_root update-desktop-database /usr/local/share/applications >/dev/null 2>&1 || true
  fi

  cat <<'EOF'

Installation complete. Open "Shogun 2 Save Sync" from your applications menu,
or run: shogun2sync
EOF
}

uninstall_app() {
  if [[ -z "$INSTALL_ROOT" ]] && command -v systemctl >/dev/null 2>&1; then
    # The Google Drive integration is a per-user timer. Stop it before its
    # bundled rclone target disappears; keep user data and unit files so a
    # later reinstall can resume only when the player explicitly enables it.
    #
    # Installing needs root, so players reasonably uninstall with sudo too. The
    # timer lives in the *player's* systemd manager, not root's, so running
    # `systemctl --user` as root would silently disable nothing and leave the
    # timer firing every two minutes at a deleted rclone.
    if [[ "$(id -u)" -eq 0 && -n "${SUDO_USER:-}" && "$SUDO_USER" != "root" ]]; then
      local sudo_uid
      sudo_uid="$(id -u "$SUDO_USER" 2>/dev/null || true)"
      if [[ -n "$sudo_uid" ]] && command -v runuser >/dev/null 2>&1; then
        local unit
        for unit in shogun2sync-gdrive-bisync.timer shogun2sync-gdrive-bisync.service; do
          runuser -u "$SUDO_USER" -- \
            env "XDG_RUNTIME_DIR=/run/user/$sudo_uid" \
            systemctl --user disable --now "$unit" >/dev/null 2>&1 || true
        done
      else
        echo "Warning: could not reach $SUDO_USER's systemd session." >&2
        echo "Run this as $SUDO_USER to stop the sync timer:" >&2
        echo "  systemctl --user disable --now shogun2sync-gdrive-bisync.timer" >&2
      fi
    elif [[ "$(id -u)" -eq 0 ]]; then
      echo "Warning: running as root, so no per-user sync timer was stopped." >&2
      echo "As your desktop user, run:" >&2
      echo "  systemctl --user disable --now shogun2sync-gdrive-bisync.timer" >&2
    else
      systemctl --user disable --now shogun2sync-gdrive-bisync.timer \
        >/dev/null 2>&1 || true
      systemctl --user stop shogun2sync-gdrive-bisync.service \
        >/dev/null 2>&1 || true
    fi
  fi

  echo "==> Removing Shogun 2 Save Sync application files"
  run_as_root rm -f \
    "$BIN_PATH" \
    "$DESKTOP_PATH" \
    "$ICON_PATH" \
    "$LICENSE_DIR/LICENSE" \
    "$LICENSE_DIR/GO-THIRD-PARTY-NOTICES.txt" \
    "$LICENSE_DIR/WEBKITGTK-NOTICE.txt" \
    "$LICENSE_DIR/rclone-COPYING"
  # The bundle owns this whole prefix, so removing it is safe and is the only
  # way to be sure no stale library is left behind.
  run_as_root rm -rf "$APP_DIR"
  run_as_root rmdir "$LICENSE_DIR" 2>/dev/null || true

  if [[ -z "$INSTALL_ROOT" ]] && command -v update-desktop-database >/dev/null 2>&1; then
    run_as_root update-desktop-database /usr/local/share/applications >/dev/null 2>&1 || true
  fi

  cat <<'EOF'

Shogun 2 Save Sync has been removed. Your saves, cloud files, and settings were
left untouched.
EOF
}

case "${1:-}" in
  "") install_app ;;
  --uninstall) uninstall_app ;;
  -h|--help) usage ;;
  *)
    usage >&2
    exit 2
    ;;
esac
