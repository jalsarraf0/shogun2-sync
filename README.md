# Shogun 2 Save Sync

[![CI](https://github.com/jalsarraf0/shogun2-sync/actions/workflows/ci.yml/badge.svg)](https://github.com/jalsarraf0/shogun2-sync/actions/workflows/ci.yml)
[![Latest release](https://img.shields.io/github/v/release/jalsarraf0/shogun2-sync)](https://github.com/jalsarraf0/shogun2-sync/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Turns a Shogun 2 multiplayer campaign desync from "campaign is dead" into
a five-minute fix, by keeping both players' save files mirrored through a
cloud folder you already use — instead of manually hunting down a save
file and sending it over Discord.

**What this does not do:** stop the desync from happening. Shogun 2's
multiplayer netcode is closed-source and the live simulation state lives
in each player's own game process — nothing outside the game can prevent
that. This only removes the friction of recovering from it.

A small native app for 64-bit Windows and Linux, no command line required.

## Download

Grab the latest release for your OS from the
[Releases page](https://github.com/jalsarraf0/shogun2-sync/releases):

| Platform | File |
|---|---|
| Windows 10/11 (x86-64) | `shogun2sync-windows-amd64.zip` — unzip, run `shogun2sync.exe` |
| Debian / Ubuntu | `shogun2sync_<version>_amd64.deb` |
| Fedora / Nobara | `shogun2sync-<version>-1.x86_64.rpm` |
| Arch | `shogun2sync-<version>-1-x86_64.pkg.tar.zst`, or build from the `PKGBUILD` attached to the release |
| Other current Linux (x86-64) | `shogun2sync-linux-amd64.tar.gz` — recommended bundle with the app and its private rclone runtime; install the GTK/WebKit libraries below first |
| Source | `shogun2-sync-<version>-src.tar.gz` |

No installer required for the Windows build — it's a single portable `.exe`.
It needs the Microsoft Edge WebView2 Runtime, which is already present on most
Windows 10 systems and included with Windows 11. This release does not include
a macOS or ARM build.

The Linux packages target distributions with the WebKit2GTK 4.1 ABI: Debian
12 / Ubuntu 24.04 or newer, Fedora 40 or newer (including current Nobara), and
current Arch Linux. The `.deb`, `.rpm`, and Arch packages install GTK 3,
WebKit2GTK 4.1 through the native package manager and include a private,
checksum-verified rclone 1.74.4 binary for Google Drive syncing. If you use the
Linux tarball, install the equivalent GUI libraries yourself:

```bash
# Debian / Ubuntu
# WebKit pulls in the correctly named GTK 3 runtime for your release
sudo apt install libwebkit2gtk-4.1-0

# Fedora / Nobara
sudo dnf install gtk3 webkit2gtk4.1

# Arch Linux
sudo pacman -S gtk3 webkit2gtk-4.1
```

Unpack the recommended raw bundle and run `shogun2sync` from inside its
directory; keep the sibling `rclone` file with it. The older lone
`shogun2sync-linux-amd64` artifact remains available for compatibility, but it
requires rclone 1.71 or newer to be installed separately and available on
`PATH` when using Google Drive.

Every release also ships a `SHA256SUMS` file. To check what you
downloaded, put it beside the artifacts and run
`sha256sum -c --ignore-missing SHA256SUMS`.

## Quick start

1. Both players install a cloud sync client (**Dropbox recommended** —
   simplest and most reliable on both OSes; OneDrive also works;
   Google Drive works but needs one extra step on Linux, see below) and
   share one folder between them.
2. Run Shogun 2 Save Sync. First launch walks you through:
   - which cloud provider you're using
   - confirming the shared folder
   - finding your Shogun 2 save folder (run the game once first if it
     can't find it — that's what creates the folder)
3. Both players do this, pointed at the *same* shared folder. That's it —
   saves now sync automatically.
4. If a desync happens: open the app's **Recover** tab. It finds the
   duplicate files the cloud client leaves behind when both players save
   at the same moment, and gives you a one-click way to resolve them
   (moved to a recoverable trash folder, never deleted outright).

### Provider notes

- **Dropbox** — make sure the shared folder is set to **Local**, not
  "Online only," or small save-file writes can fail silently.
- **OneDrive** — turn off **Files On-Demand** for the shared folder
  ("Always keep on this device"), for the same reason.
- **Google Drive** — on Windows, switch Google Drive's setting from
  "Stream files" to **"Mirror files."** On Linux, Google ships no real
  sync client at all, so the app authorizes against the Drive API
  directly (native OAuth flow, real browser login) and runs
  [`rclone`](https://rclone.org) `bisync` on a background timer to
  maintain a real local mirror — a live network mount would risk the
  exact write failures a streaming client has. The Linux packages and
  recommended tarball bundle rclone 1.74.4, so they also work on Ubuntu and
  Debian releases whose repositories ship an older version. Only the legacy
  lone-binary artifact and source development require a separate rclone 1.71
  or newer. Dropbox and OneDrive do not use `rclone`.
  The wizard asks whether you're the one who received a shared folder
  link, or the one sharing your own Drive — the host path needs no
  link at all and hands you back a shareable link to send afterward.

## Reducing how often desyncs happen in the first place

Standing community advice for Shogun 2 MP campaigns, not specific to
this tool, but worth doing regardless:

- Both players use the **same DirectX version** in game settings.
- **No mods, no Workshop subscriptions** on either side.
- **Verify game file integrity** (Steam → Properties → Local Files) on
  both machines.
- Fully quit the game before handing off whose turn it is, and wait for
  the cloud client to show "up to date" before the other player launches.

## How it works

The game always saves to a fixed local folder. Setup moves that folder's
contents into your cloud-synced tree, then replaces the original with a
symlink (Linux) or directory junction (Windows, no admin rights needed).
The game keeps reading and writing the same path it always has; the
cloud client mirrors the real folder to the other player automatically.

The Google Drive OAuth flow runs entirely in-app: a local Go HTTP server
handles the loopback callback directly (reusing `rclone`'s own
already-Google-verified OAuth client, so there's no lengthy app-review
process), rather than depending on a third-party CLI's interactive flow.

## Building from source

The pinned release toolchain is Go 1.25.0+, Node 22.23.2, npm 10.9.8, and
Wails 2.13.0.
Linux also needs a C compiler, `pkg-config`, GTK 3 development headers, and
WebKit2GTK 4.1 development headers. For example, install
`build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev` on
Debian/Ubuntu, or `gcc pkgconf-pkg-config gtk3-devel webkit2gtk4.1-devel` on
Fedora/Nobara.

```bash
cd app
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
npm ci --prefix frontend
wails build -tags webkit2_41       # Linux
wails build -platform windows/amd64 # Windows (cross-compiles cleanly, no mingw needed)
```

`app/packaging/build-release.sh <version>` builds every release artifact
(Windows zip, `.deb`, `.rpm`, Arch package, source tarball) into `dist/`.
Release builds additionally require nFPM 2.47.0, `curl`, `unzip`, `zip`,
`sha256sum`, network access to `downloads.rclone.org`, and a clean Git checkout;
the supplied version must match both `app/wails.json` and the `PKGBUILD`. The
release builder checks rclone's official `SHA256SUMS` manifest against a pinned
checksum before putting its binary and MIT notice into any artifact.

## Repo layout

```
app/                    the app (Go backend + Wails webview UI)
  internal/gdrive/       in-app Google Drive OAuth
  internal/rcloneutil/   rclone config/remote management
  internal/bisync/       Linux Google Drive systemd-timer mirror
  internal/linkutil/     symlink/junction creation
  internal/conflicts/    cloud-conflict-file scanning
  internal/orchestrate/  ties setup/status/recover together
  frontend/               wizard UI (vanilla JS)
  packaging/              nfpm config, PKGBUILD, release build script
legacy-scripts/          the original bash/PowerShell scripts this app replaced
```

## License

MIT — see [LICENSE](LICENSE).
