#!/usr/bin/env bash
# Google Drive has no native Linux sync daemon (unlike Dropbox/OneDrive),
# and its FUSE mounts are too unreliable for a game actively writing files.
# This maintains a REAL local directory kept in sync with a Drive folder
# via `rclone bisync` on a systemd timer. Once this is running, point
# config.json's cloud_root at LOCAL_DIR and run setup.sh as normal —
# from setup.sh's perspective this is just another live-synced folder.
set -euo pipefail

REMOTE_NAME="${1:-gdrive}"
REMOTE_SUBFOLDER="${2:-Shogun2SaveSync}"
LOCAL_DIR="${3:-$HOME/GoogleDriveSync}"

if ! command -v rclone >/dev/null 2>&1; then
    echo "rclone not found. Install it first, e.g.:"
    echo "  sudo dnf install rclone"
    exit 1
fi

if ! rclone listremotes | grep -q "^${REMOTE_NAME}:$"; then
    echo "rclone remote '${REMOTE_NAME}' isn't configured yet."
    echo "Run 'rclone config' once, create a Google Drive remote named '${REMOTE_NAME}',"
    echo "and re-run this script."
    exit 1
fi

mkdir -p "$LOCAL_DIR"

echo "Establishing bisync baseline (first run only, may take a minute)..."
rclone bisync "$LOCAL_DIR" "${REMOTE_NAME}:${REMOTE_SUBFOLDER}" --resync --create-empty-src-dirs

SYSTEMD_DIR="$HOME/.config/systemd/user"
mkdir -p "$SYSTEMD_DIR"

cat > "$SYSTEMD_DIR/shogun2-gdrive-sync.service" <<EOF
[Unit]
Description=Bisync Shogun2 save folder with Google Drive

[Service]
Type=oneshot
ExecStart=/usr/bin/rclone bisync "${LOCAL_DIR}" "${REMOTE_NAME}:${REMOTE_SUBFOLDER}" --resilient --recover --create-empty-src-dirs
EOF

cat > "$SYSTEMD_DIR/shogun2-gdrive-sync.timer" <<EOF
[Unit]
Description=Run Shogun2 Google Drive bisync every 2 minutes

[Timer]
OnBootSec=1min
OnUnitActiveSec=2min
Persistent=true

[Install]
WantedBy=timers.target
EOF

systemctl --user daemon-reload
systemctl --user enable --now shogun2-gdrive-sync.timer

PARENT_DIR="$(dirname "$LOCAL_DIR")"
BASE_NAME="$(basename "$LOCAL_DIR")"

echo "Done. Local mirror: $LOCAL_DIR (bisyncs with ${REMOTE_NAME}:${REMOTE_SUBFOLDER} every 2 min)"
echo "Now set config.json to:"
echo "  \"cloud_root\": \"${PARENT_DIR}\","
echo "  \"sync_subfolder\": \"${BASE_NAME}\""
echo "so setup.sh resolves its sync target to exactly \"$LOCAL_DIR\", then run ./setup.sh as normal."
