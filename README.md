# shogun2-sync

Turns a Shogun 2 multiplayer campaign desync from "campaign is dead" into
a five-minute hiccup, by keeping both players' save files mirrored through
a cloud-synced folder instead of manually emailing/Discord-ing save files
back and forth.

**What this does not do:** stop the desync from happening. Shogun 2's
multiplayer netcode is closed-source and the actual live simulation state
lives in each player's game process — nothing outside the game can fix
that. This only removes the friction of recovering from it.

## How it works

The game always saves to a fixed local folder. This tool moves that
folder's contents into a folder inside your cloud-sync client's tree
(Dropbox/OneDrive/Google Drive), then replaces the original folder with a
symlink (Linux) or directory junction (Windows) pointing at the new
location. The game keeps reading/writing the same path as always; the
cloud client mirrors the real folder to the other player automatically.

Both players point their setup at the **same shared folder** (one player
shares it with the other via their cloud provider's folder-sharing
feature).

## No command-line needed

Both setup and recovery are double-click tools — no terminal, no editing
config files by hand. First run asks two or three plain questions (which
cloud service, and confirming the folder path); after that it remembers
and just works.

## Prerequisites

- Both players run setup against the **same shared cloud folder** (one
  player shares it with the other using their cloud provider's normal
  "share this folder" feature — the same one you'd use to share any
  folder with a friend).
- The cloud app itself (Dropbox, OneDrive, or Google Drive) must already
  be installed and finished its first sync before running setup.

### Recommended: Dropbox

Simplest and most reliable option on both OSes — use this unless you
have a strong reason not to.

**Windows:** double-click `windows\Setup.bat`. It asks which cloud
service (pick Dropbox), confirms the folder, and finishes. Done.

**Linux:** double-click `linux/Setup Shogun2 Sync.desktop` (first time,
right-click it → Properties → Permissions → check "Allow executing file
as program," or "Trust and Launch" if your file manager asks). Same
questions, same result.

⚠️ One setting to check either way: right-click the shared folder in
Dropbox → make sure it's set to **Local**, not "Online only" — otherwise
small save files can fail to write correctly.

### OneDrive

Same double-click steps as above, just choose OneDrive when asked.

⚠️ **Important:** right-click the shared folder in OneDrive → **"Always
keep on this device."** OneDrive's default "Files On-Demand" behavior
can turn save files into placeholders that aren't actually downloaded,
which can make the game fail to read or write them correctly.

### Google Drive (more setup work — Dropbox is easier)

**Windows:** Google Drive defaults to a mode that doesn't keep real
files on your computer, which breaks this. Open Google Drive's settings
→ **switch to "Mirror files"** (not "Stream files") before running
Setup.bat.

**Linux:** Google has no real sync app for Linux, so this needs one
extra one-time step from someone comfortable with a terminal:
```bash
rclone config          # create a remote named 'gdrive', one-time login
./linux/setup-gdrive-rclone.sh gdrive Shogun2SaveSync
```
Follow what it prints, then double-click the normal setup launcher.

## When a desync happens

Stop playing immediately — continuing on drifted state compounds it.

Double-click `linux/Recover Shogun2 Sync.desktop` or
`windows\Recover.bat`. It lists any files that look like sync conflicts
(both players saved at the same moment) and walks you through, in plain
language, which one to keep.

## Reducing how often this happens in the first place

These don't come from this tool, they're just the standing community
advice for Shogun 2 MP campaigns — worth doing regardless:

- Both players use the **same DirectX version** in game settings (9 or
  11 — pick one).
- **No mods, no Workshop subscriptions** on either side.
- **Verify game file integrity** (Steam → Properties → Local Files) on
  both machines.
- Fully quit the game before handing off whose turn it is, and wait for
  the cloud client's "up to date" checkmark before the other player
  launches.

## Sending this to the other player

Zip the `windows` folder (it's self-contained) and send it to them
however's easiest — Discord, email, the shared cloud folder itself.
They extract it anywhere and double-click `Setup.bat`.

## Repo layout

```
config.example.json           # reference only; the double-click tools write config.json themselves
linux/Setup Shogun2 Sync.desktop   # double-click entry point
linux/Recover Shogun2 Sync.desktop # double-click entry point
linux/setup.sh                 # does the actual work (called by the .desktop file)
linux/recover.sh               # does the actual work (called by the .desktop file)
linux/setup-gdrive-rclone.sh   # Linux-only, Google Drive: maintains a real local Drive mirror
windows/Setup.bat              # double-click entry point
windows/Recover.bat            # double-click entry point
windows/setup.ps1              # does the actual work (called by Setup.bat)
windows/recover.ps1            # does the actual work (called by Recover.bat)
```
