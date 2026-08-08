#!/usr/bin/env bash
# Finds cloud-sync "conflicted copy" duplicates in the Shogun2 save folder
# (created when both players' games write at the same moment) and helps
# you pick which one to keep. Run this the moment a desync is noticed,
# before playing further turns.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_FILE="${REPO_ROOT}/config.json"

if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "Setup hasn't been run yet. Double-click the setup script first."
    read -r -p "Press Enter to close..."
    exit 1
fi

read_json() {
    python3 -c "import json,sys; print(json.load(open('${CONFIG_FILE}'))['$1'])"
}

CLOUD_ROOT_RAW="$(read_json cloud_root)"
CLOUD_ROOT="${CLOUD_ROOT_RAW/#\~/$HOME}"
SYNC_SUBFOLDER="$(read_json sync_subfolder)"
SYNC_TARGET="${CLOUD_ROOT}/${SYNC_SUBFOLDER}"

if [[ ! -d "$SYNC_TARGET" ]]; then
    echo "Sync folder not found at $SYNC_TARGET. Double-click the setup script first."
    read -r -p "Press Enter to close..."
    exit 1
fi

# Patterns used by Dropbox, OneDrive, and Google Drive for conflict duplicates.
CONFLICTS=$(find "$SYNC_TARGET" -maxdepth 1 -type f \( \
    -iname "*conflicted copy*" -o \
    -iname "*'s conflicted copy*" -o \
    -iregex ".*([0-9]).*" \
\) 2>/dev/null | grep -Ei "conflict" || true)

if [[ -z "$CONFLICTS" ]]; then
    echo "No problem files found -- looks clean."
    echo "Most recent saves in the shared folder:"
    ls -lt "$SYNC_TARGET" | head -10
    read -r -p "Press Enter to close..."
    exit 0
fi

echo "Found files that look like sync conflicts (both players saved at the same moment):"
echo "$CONFLICTS" | while read -r f; do
    printf '  %s  (saved: %s)\n' "$f" "$(date -r "$f" '+%Y-%m-%d %H:%M:%S')"
done

echo
echo "What to do:"
echo "  1. Open your file manager and go to the folder listed above."
echo "  2. Talk to the other player -- figure out together which save is the"
echo "     right one to keep (usually whichever one matches what you both"
echo "     last actually saw in-game)."
echo "  3. Delete the other (wrong) file(s) listed above, using your normal"
echo "     file manager -- just move them to the trash."
echo "  4. Both players fully quit Shogun2, confirm only one save remains"
echo "     for that turn, then relaunch and reload the campaign together."
read -r -p "Press Enter to close..."
