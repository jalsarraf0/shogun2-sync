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
| Windows 10/11 (x86-64) | `shogun2sync-windows-amd64-installer.exe` — recommended one-step setup |
| Debian / Ubuntu | `shogun2sync_<version>_amd64.deb` |
| Fedora / Nobara | `shogun2sync-<version>-1.x86_64.rpm` |
| Arch | `shogun2sync-<version>-1-x86_64.pkg.tar.zst`, or build from the `PKGBUILD` attached to the release |
| Other supported Linux (x86-64) | `shogun2sync-linux-amd64.run` — single-file automatic installer |
| Source | `shogun2-sync-<version>-src.tar.gz` |

On Windows, run the setup `.exe`. It installs the app for the current user,
adds Start menu and desktop shortcuts, and carries both a private,
checksum-verified rclone 1.74.4 binary and Microsoft's complete x64
WebView2 Evergreen Standalone Runtime, so a missing runtime is installed even
when the computer is offline. No separate dependency download is required.
The uninstaller removes everything it installed, including that private
rclone. This release does not include a macOS or ARM build.

### If Windows blocks the download or installer

The Windows build is **not code-signed** (no Microsoft tax). That does not
mean the package is incomplete — only that Windows has not been paid to trust
it yet. On Windows 10 you can almost always proceed:

1. If SmartScreen says “Windows protected your PC”: click **More info**, then
   **Run anyway**.
2. If double-click does nothing useful: right-click the `.exe` →
   **Properties** → tick **Unblock** at the bottom → **OK**, then run it again.
3. Optional integrity check in PowerShell (from the folder with both files):

```powershell
Get-FileHash .\shogun2sync-windows-amd64-installer.exe -Algorithm SHA256
Select-String shogun2sync-windows-amd64-installer.exe .\SHA256SUMS
```

The hashes must match. After install you can also run a headless proof that
rclone and the config directory are ready:

```text
"%LOCALAPPDATA%\Programs\Shogun 2 Save Sync\shogun2sync.exe" --self-check
```

Exit code 0 and “ready to use” means the install is good.

The Linux artifacts target Debian 12, Ubuntu 24.04 or newer, currently
supported Fedora/Nobara releases, and current Arch Linux. There are two shapes,
and they make opposite trade-offs on purpose. Installing the `.deb`, `.rpm`, or
Arch package through the native software installer resolves GTK 3, WebKit2GTK
4.1, certificates, systemd, and desktop-browser integration in the same
transaction, which keeps WebKit on your distribution's security updates — this
is the better choice whenever you have repository access. The generic `.run`
instead bundles that GUI stack so it can install with no repositories at all.
Every Linux artifact carries the app-owned pieces itself: a private,
checksum-verified rclone 1.74.4 binary, menu entry, icon, and all applicable
license notices.

Use the dependency-resolving installer for your distribution (double-clicking
the package in its graphical software installer does the same thing):

```bash
sudo apt install ./shogun2sync_<version>_amd64.deb
sudo dnf install ./shogun2sync-<version>-1.x86_64.rpm
sudo pacman -U ./shogun2sync-<version>-1-x86_64.pkg.tar.zst
```

For the generic fallback, run the one file. It needs no package manager, no
repository access, and no network connection: it carries its own GTK 3 and
WebKitGTK 4.1 stack alongside the app, menu entry, icon, notices, and rclone,
and installs them under `/opt/shogun2sync` in one pass:

```bash
sh shogun2sync-linux-amd64.run
```

The bundled runtime deliberately stops short of the pieces that must belong to
your machine — glibc, the graphics driver, X11/Wayland, and fontconfig — so it
uses your own GPU acceleration and fonts. Every graphical desktop already has
these; the installer checks for them and says so plainly if any are missing.

The bundled copy of `libwebkit2gtk-4.1.so.0` has one modification: WebKitGTK
compiles the path of its helper processes into the library with no runtime
override, so that path is rewritten to point inside the install prefix. This is
recorded, with the corresponding-source information the LGPL requires, in
`/usr/local/share/licenses/shogun2sync/WEBKITGTK-NOTICE.txt`.

The installer asks for `sudo` only when copying application files; it never
runs the app as root. To uninstall, run the `.run` file with `-- --uninstall`.
It stops your Google sync timer, removes only application files, and leaves
saves, cloud data, and settings alone:

```bash
sh shogun2sync-linux-amd64.run -- --uninstall
```

Every release also ships a `SHA256SUMS` file. To check what you
downloaded, put it beside the artifacts and run
`sha256sum -c --ignore-missing SHA256SUMS`.
The release workflow also publishes GitHub/Sigstore provenance attestations;
`gh attestation verify <downloaded-file> -R jalsarraf0/shogun2-sync` confirms
which repository, commit, and workflow produced an installer.

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
  exact write failures a streaming client has. Every installer — the Windows
  setup `.exe`, the Linux packages, and the one-file installer — bundles
  rclone 1.74.4 and runs that private copy, so they also work on Ubuntu and
  Debian releases whose repositories ship an older version, and on a Windows
  machine with nothing else installed. Only source
  development requires a separate rclone 1.71 or newer. Dropbox and OneDrive
  do not use `rclone`.
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
(offline Windows installer, Linux installer bundle, `.deb`, `.rpm`, Arch
package, and source tarball) into `dist/`.
Release builds additionally require Docker or Podman, nFPM 2.47.0, NSIS
(`makensis`), `curl`, `jq`, `unzip`, `sha256sum`, OpenSSL, `osslsigncode`,
network access to the official Go, Makeself, rclone, and Microsoft download
hosts, and a clean Git checkout; release
verification also uses `7z`, `dpkg-deb`, `rpm`, and Zstandard-enabled `tar`.
The supplied version must match both `app/wails.json` and the `PKGBUILD`. The
builder verifies pinned SHA-256 checksums for both rclone builds, the complete offline
WebView2 runtime, and Microsoft's root certificate, then verifies WebView2's
primary Authenticode signer. A second gate inspects every finished
installer/package for its runtime declarations, bundled executables, licenses,
desktop integration, and checksums before the release is published. Pull-request
CI also performs real package-manager installs in clean Debian, Ubuntu, Fedora,
and Arch containers, and installs, launches, and uninstalls the final setup on
a Windows runner. The shared Linux executable is built in a digest-pinned Debian
12 container—the oldest supported ABI baseline—before that identical executable
is placed into every Linux format.

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

The Shogun 2 Save Sync source is MIT — see [LICENSE](LICENSE). Distributed
installers also carry the separate terms and notices for their bundled rclone,
Go/Wails modules, and Microsoft WebView2 runtime.
