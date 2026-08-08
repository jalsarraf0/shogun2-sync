# shogun2-sync — handoff

Repo: `~/git/shogun2-sync` (git initialized, one commit, branch `main`, no remote yet).

## Problem

User's Shogun 2 multiplayer campaign keeps hitting "failed game" desyncs
around turn 90-110. Shogun 2 (2011, Creative Assembly) is unsupported;
the multiplayer campaign netcode is closed-source and has no real fix —
this is a known, unresolved, long-standing community issue. Desync
happens in each player's live game-process memory; nothing outside the
game can prevent it.

## Decision made

Don't try to fix the desync (not feasible — no source access, no
engine hooks, memory-patching a live Steam binary is fragile/risky and
was explicitly ruled out). Instead: make recovery from a desync cheap.
Today, recovery means "someone manually finds their save file, sends it
over Discord, the other person overwrites theirs, rehost" — slow enough
that campaigns get abandoned. This tool automates that.

## Constraints that shaped the design

- The two players are on different machines: user is Linux (Nobara,
  runs Shogun 2 via Steam/Proton), other player is Windows 10 or 11.
- They are **not** on a shared VPN/Tailscale and have no direct network
  access to each other — ruled out Syncthing device-ID pairing and any
  SSH/rsync approach requiring inbound access on either side.
- Other player will share a folder via a consumer cloud service
  (Dropbox confirmed as the one actually being used).
- **Both players have zero CLI/scripting knowledge** — this was the
  last requirement added and caused a full rework: originally built as
  bash/PowerShell scripts needing manual `config.json` editing; now
  fully interactive (asks 2-3 plain-language questions on first run,
  writes its own config) with double-click entry points on both OSes.

## How it works

Game always saves to a fixed local folder
(`save_games_multiplayer` under the Shogun2 AppData path — same path
under Wine/Proton's `drive_c`, or native on Windows). Setup moves that
folder's contents into a folder inside the cloud client's synced tree,
then replaces the original with a symlink (Linux `ln -s`) or directory
junction (Windows `New-Item -ItemType Junction`, no admin needed —
falls back to a real symlink if junction creation fails). The game
keeps writing to the same path as always; the cloud client mirrors the
real folder to the other player automatically. Both players point
setup at the *same* shared folder.

## Research findings baked into the design (verified via 3 parallel
web-research subagents against superuser.com, Reddit, Google/MS docs,
rclone forums — not just training-data recall)

- **Dropbox**: sound approach, junction/symlink-into-Dropbox's-own-tree
  is the correct direction (confirmed working). Must set the shared
  folder to **Local**, not "Online only" (Smart Sync placeholders can
  break small game-save writes). No native locking — simultaneous
  writes from both players can produce "conflicted copy" files; this is
  what `recover.sh`/`recover.ps1` scans for. **Recommended default.**
- **OneDrive**: same junction direction confirmed correct (junction
  *outside* OneDrive's tree pointing *in* — the reverse is documented
  as broken). Must disable **Files On-Demand** for the folder ("Always
  keep on this device"), or placeholder files can silently fail to
  read/write correctly. Linux client: abraunegg/onedrive (solid,
  actively maintained, real Linux daemon).
- **Google Drive**: weakest of the three. Windows "Streaming" mode is
  virtual-drive/placeholder-based and unsafe for this — must force
  **"Mirror files"** mode instead. **Linux has no official client at
  all**; FUSE mounts (google-drive-ocamlfuse, GVFS) are unreliable for
  actively-written files, so the design uses `rclone bisync` on a
  systemd user timer (`linux/setup-gdrive-rclone.sh`) to maintain a
  real local mirror directory, which then gets treated as a normal
  cloud_root by the same `setup.sh`. This is the one piece that still
  requires a terminal (`rclone config` needs one-time interactive
  OAuth) — flagged in the README as "needs a technical friend," steered
  users toward Dropbox instead.

## What's built (all committed, one commit on `main`)

```
config.example.json              reference only now; tools write their own config.json (gitignored)
linux/Setup Shogun2 Sync.desktop  double-click entry point (Terminal=true, hardcoded path to this user's home)
linux/Recover Shogun2 Sync.desktop
linux/setup.sh                   interactive first-run (asks provider/path), then move+symlink, idempotent re-run
linux/recover.sh                 scans shared folder for Dropbox/OneDrive/GDrive "conflicted copy" patterns, plain-language guidance, pauses before closing
linux/setup-gdrive-rclone.sh     Google Drive Linux only: rclone bisync + systemd timer setup
windows/Setup.bat / Recover.bat  wrap the .ps1 files with -ExecutionPolicy Bypass so no PowerShell/policy prompts ever surface
windows/setup.ps1 / recover.ps1  same interactive logic as the Linux scripts, PowerShell-native (junction via New-Item)
README.md                        provider-by-provider setup instructions, written for non-technical readers
```

Tested (dry runs against fake save/cloud dirs in scratchpad, not the
real Shogun2 install): setup.sh's move+symlink logic, idempotent
re-run detection, interactive config-writing flow, recover.sh's
no-conflict path. **Not yet tested**: the actual Windows side (no
Windows machine available in this session — setup.ps1/recover.ps1 are
unexecuted, syntax-reviewed only), and no real-world run against an
actual Shogun2 install or actual Dropbox sync between two real
machines.

## What's not done / open threads

1. **Windows side is unverified in practice.** PowerShell junction
   fallback-to-symlink path, the `$env:OneDrive` auto-detection, and
   the `.bat` wrappers have not been run on a real Windows box.
2. **Not sent to the other player yet.** README suggests zipping the
   `windows/` folder and sending it over; hasn't happened.
3. **No real playtest.** Haven't confirmed this actually survives past
   turn ~100 in a live campaign — the whole point is unproven until
   used.
4. **Google Drive path is the least polished** — deliberately, since
   Dropbox is what's actually being used and research ranked Google
   Drive worst for this use case. Not worth more investment unless the
   other player insists on Drive over Dropbox.
5. No `git remote` configured — this only exists locally on
   `dominus-nobara` right now.

## Relevant standing context (not from this session, from persistent
memory — may be useful if you're a different tool picking this up cold)

- User's convention: all git projects live under `~/git/<project>`.
- User is on Nobara Linux (Fedora-based, KDE/Plasma), 7950X/RTX 3090
  desktop, deep sysadmin comfort — the "zero CLI knowledge" constraint
  in this task is specifically about the *other player*, not about the
  user's own general skill level (worth noting since it drove the
  double-click rework even on the Linux side, for consistency).
