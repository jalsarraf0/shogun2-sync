#!/usr/bin/env bash
# Symlinks the Shogun2 multiplayer save folder into a cloud-synced folder
# so a save shared/synced with another player propagates automatically.
# Double-click "Shogun2 Sync Setup.desktop" to run this with no terminal
# knowledge needed -- it will ask plain-language questions if this is the
# first run.
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CONFIG_FILE="${REPO_ROOT}/config.json"

read_json() {
    python3 -c "import json,sys; print(json.load(open('${CONFIG_FILE}'))['$1'])"
}

# ---- First run: ask plain questions instead of requiring a hand-edited file ----
if [[ ! -f "$CONFIG_FILE" ]]; then
    echo "=== Shogun 2 Save Sync - First Time Setup ==="
    echo
    echo "Which cloud service is the shared save folder in?"
    echo "  1) Dropbox"
    echo "  2) OneDrive"
    echo "  3) Google Drive (advanced - needs extra setup, see README)"
    read -r -p "Enter 1, 2, or 3: " CHOICE

    case "$CHOICE" in
        1)
            PROVIDER="dropbox"
            DEFAULT_ROOT="$HOME/Dropbox"
            ;;
        2)
            PROVIDER="onedrive"
            DEFAULT_ROOT="$HOME/OneDrive"
            ;;
        3)
            echo
            echo "Google Drive on Linux needs the rclone helper first. Run:"
            echo "  ./linux/setup-gdrive-rclone.sh"
            echo "and follow its instructions, then run this script again."
            exit 1
            ;;
        *)
            echo "Didn't understand that, please run this again and enter 1, 2, or 3."
            exit 1
            ;;
    esac

    echo
    echo "The folder your friend shared with you should already be syncing to your computer."
    read -r -p "Press Enter to use the usual location [${DEFAULT_ROOT}], or type a different folder path: " CLOUD_ROOT_INPUT
    CLOUD_ROOT="${CLOUD_ROOT_INPUT:-$DEFAULT_ROOT}"
    CLOUD_ROOT="${CLOUD_ROOT/#\~/$HOME}"

    if [[ ! -d "$CLOUD_ROOT" ]]; then
        echo
        echo "Can't find '${CLOUD_ROOT}'. Make sure ${PROVIDER} is installed, has finished"
        echo "its first sync, and that folder actually exists, then run this again."
        exit 1
    fi

    read -r -p "Name of the shared save folder inside it [Shogun2SaveSync]: " SUBFOLDER_INPUT
    SYNC_SUBFOLDER="${SUBFOLDER_INPUT:-Shogun2SaveSync}"

    python3 -c "
import json
json.dump({
    'cloud_provider': '${PROVIDER}',
    'cloud_root': '${CLOUD_ROOT}',
    'sync_subfolder': '${SYNC_SUBFOLDER}',
}, open('${CONFIG_FILE}', 'w'), indent=2)
"
    echo "Saved settings to config.json (won't ask again next time)."
    echo
fi

CLOUD_ROOT_RAW="$(read_json cloud_root)"
CLOUD_ROOT="${CLOUD_ROOT_RAW/#\~/$HOME}"
SYNC_SUBFOLDER="$(read_json sync_subfolder)"
SYNC_TARGET="${CLOUD_ROOT}/${SYNC_SUBFOLDER}"

if [[ ! -d "$CLOUD_ROOT" ]]; then
    echo "Cloud folder '${CLOUD_ROOT}' doesn't exist. Make sure the cloud client is installed"
    echo "and has finished its first sync, then run this again."
    read -r -p "Press Enter to close..."
    exit 1
fi

# Known locations where a Steam/Proton install of Shogun2 keeps its MP saves.
CANDIDATES=(
    "$HOME/.steam/steam/steamapps/compatdata/34330/pfx/drive_c/users/steamuser/AppData/Roaming/The Creative Assembly/Shogun2/save_games_multiplayer"
    "$HOME/.local/share/Steam/steamapps/compatdata/34330/pfx/drive_c/users/steamuser/AppData/Roaming/The Creative Assembly/Shogun2/save_games_multiplayer"
    "$HOME/.var/app/com.valvesoftware.Steam/.steam/steam/steamapps/compatdata/34330/pfx/drive_c/users/steamuser/AppData/Roaming/The Creative Assembly/Shogun2/save_games_multiplayer"
)

SAVE_PATH=""
for c in "${CANDIDATES[@]}"; do
    if [[ -d "$c" ]]; then
        SAVE_PATH="$c"
        break
    fi
done

if [[ -z "$SAVE_PATH" ]]; then
    if [[ $# -ge 1 ]]; then
        SAVE_PATH="$1"
    else
        echo "Couldn't find your Shogun2 save folder automatically."
        echo "It's normally inside Steam's hidden game data. If you know it, type the full"
        echo "path below. If not, ask for help finding it -- don't guess."
        read -r -p "Save folder path (or leave blank to cancel): " SAVE_PATH
        if [[ -z "$SAVE_PATH" ]]; then
            echo "Cancelled."
            read -r -p "Press Enter to close..."
            exit 1
        fi
    fi
fi

echo "Save folder: $SAVE_PATH"
echo "Sync target: $SYNC_TARGET"

if [[ -L "$SAVE_PATH" ]]; then
    CURRENT_TARGET="$(readlink -f "$SAVE_PATH")"
    if [[ "$CURRENT_TARGET" == "$(readlink -f "$SYNC_TARGET" 2>/dev/null || echo "$SYNC_TARGET")" ]]; then
        echo
        echo "All set up already -- nothing to do. You're good to play."
        read -r -p "Press Enter to close..."
        exit 0
    else
        echo "Save folder already points somewhere else ($CURRENT_TARGET)."
        echo "Ask for help before continuing -- something's already been set up differently."
        read -r -p "Press Enter to close..."
        exit 1
    fi
fi

mkdir -p "$SYNC_TARGET"

if [[ -d "$SAVE_PATH" ]]; then
    echo "Moving your existing saves into the synced folder..."
    shopt -s dotglob nullglob
    for f in "$SAVE_PATH"/*; do
        mv "$f" "$SYNC_TARGET/"
    done
    shopt -u dotglob nullglob
    rmdir "$SAVE_PATH"
fi

ln -s "$SYNC_TARGET" "$SAVE_PATH"
echo
echo "All done! Your saves will now sync automatically. You're good to play."
read -r -p "Press Enter to close..."
