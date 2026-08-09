# Packaging notes

How Shogun 2 Save Sync is built for release, and why each choice was made. The
goal these decisions serve: one download per platform, one click, no missing
runtime dependency, and no network fetch of dependencies at install time.

## Supported scope

x86-64 Windows 10/11, plus Debian 12, Ubuntu 24.04+, current Fedora/Nobara, and
current Arch. Not macOS, not ARM. Cloud-account login, choosing a shared folder,
and installing the game or a Dropbox/OneDrive client are user choices, not
redistributable runtime dependencies.

## Windows

One offline NSIS EXE, per-user, roughly 205 MB. It embeds the **full** Evergreen
WebView2 standalone runtime installer — the ~200 MB offline package, not the
2 MB bootstrapper — along with the app, rclone, uninstaller, licences and
notices. The payload URL, SHA-256, Microsoft root, and signer leaf are pinned
and verified with `osslsigncode` at build time.

Three details are load-bearing:

- The app is built with `-webview2 error`. Wails' default `download` strategy
  links a live `http.Get` of the Microsoft bootstrapper into the binary and runs
  it whenever the startup runtime check fails. The build asserts the fwlink URL
  is absent from the shipped `.exe`, so this cannot silently regress.
- The runtime check treats the **registry** as authoritative, per Microsoft's
  documentation, and polls it for a minute after setup exits. Microsoft's
  standalone installer is known to return before it has finished
  (WebView2Feedback #1349), so a single immediate check fails installs that
  would have succeeded.
- Failure paths use `MessageBox` only when not silent. NSIS does not suppress
  `MessageBox` under `/S`, so an unattended install would otherwise hang on a
  dialog nobody can click.

## Linux

Two shapes, deliberately different:

**Native `.deb` / `.rpm` / Arch packages** declare every distro-owned runtime
(certificates, glibc, GTK 3, WebKitGTK 4.1, systemd, xdg-utils) under each
distribution's real package names, so the package manager resolves everything in
one transaction. WebKit stays on distribution security updates. This is the
better path whenever the user has repository access.

**The generic `.run`** carries its own GTK 3 / WebKitGTK 4.1 stack so it installs
with no repositories and no network. It installs to a fixed `/opt/shogun2sync`.

### Why the `.run` bundles the GUI stack

Windows can be fully offline because Microsoft ships a redistributable WebView2
runtime. Linux has no redistributable WebKitGTK, so offline means becoming its
distributor. That is a real cost, taken deliberately — see "Obligations" below.

AppImage and Flatpak were both rejected. Flatpak cannot install a systemd
**user** timer at all (flatpak#2787), and the workarounds — `--filesystem=host`
plus `--talk-name=org.freedesktop.Flatpak` — are a full sandbox escape that
buys a ~1 GB runtime for zero isolation. AppImage is not sandboxed and would
work, but its relocation mechanism is weaker than ours: it rewrites `/usr` to
`././` and depends on the process CWD, which is the source of two open Tauri
bugs. A fixed install prefix gives an absolute path instead.

### How the bundle works

`packaging/linux-runtime-bundle.sh` runs inside the same digest-pinned Debian 12
image the binary is built in, and:

1. Walks the `ldd` closure of the app and the WebKit helper processes.
2. Copies everything **except** the host-owned set: glibc, the dynamic loader,
   libstdc++, libgcc, the graphics stack, X11, Wayland, and fontconfig. This is
   AppImage's excludelist rule and it is not optional — the host's mesa DRI
   driver is `dlopen`ed into this process and built against the host's
   libstdc++, so forcing an older bundled copy breaks hardware rendering.
3. Copies the WebKit helper binaries, the GIO modules (`libgiognutls.so` — without
   it every HTTPS request fails), the gdk-pixbuf loaders, schemas, MIME and icon
   data, and CA certificates.
4. **Rewrites WebKitGTK's compiled-in helper path.** WebKitGTK 4.1 bakes
   `PKGLIBEXECDIR` in as a literal and exposes no override: `WEBKIT_EXEC_PATH` is
   behind `ENABLE(DEVELOPER_MODE)` and absent from release builds. The string is
   replaced in place, NUL-padded to the original length so every ELF offset is
   unchanged. Without this the app finds no helper processes and dies at startup.
   (`WEBKIT_INJECTED_BUNDLE_PATH`, by contrast, *is* honoured in release builds,
   so the injected bundle needs no patching.)
5. Regenerates the loader caches, whose absolute paths must describe the install
   prefix rather than the build directory.
6. Writes a launcher exporting `LD_LIBRARY_PATH`, `GIO_EXTRA_MODULES`,
   `GDK_PIXBUF_MODULE_FILE`, `GTK_PATH`, `GSETTINGS_SCHEMA_DIR`, `XDG_DATA_DIRS`
   and `SSL_CERT_FILE`. Both the menu entry and the `/usr/local/bin` symlink point
   at the launcher, never the raw binary.

`RUNTIME_PREFIX` in `packaging/linux-runtime.env` is baked into the library, so
the `.run` cannot be relocated. `install-linux.sh` must agree with it.

### The gate that matters

A subtly incomplete bundle still works on any machine that happens to have GTK
and WebKitGTK installed, because the linker quietly falls back to the host's
copies. That is how AppImages shipped "self-contained" for years without being
so. `packaging/verify-linux-offline.sh` therefore builds a Debian, Ubuntu,
Fedora and Arch image containing only the base desktop libraries the bundle
does not carry, runs the installer with `--network none`, launches the app,
asserts both WebKit helper processes started, and reads `/proc/PID/maps` to
confirm the mapped library is `/opt/shogun2sync/lib/libwebkit2gtk-4.1.so.0`.
Anything less is not evidence.

## Obligations this creates

- **WebKitGTK security servicing is now ours** for the `.run`. Upstream ships
  advisories roughly monthly. This needs a standing rebuild-and-release process;
  it is not a one-time build change. The native packages are unaffected.
- **LGPL redistribution.** The bundle ships GTK, WebKitGTK and friends, and
  modifies one of them. `WEBKITGTK-NOTICE.txt` is generated at build time from
  the actual packages, records the modification and its purpose, lists every
  bundled package with its exact Debian version, points at the corresponding
  source, and notes that relinking works by replacing a file under `lib/`.

## Still open

1. **Windows code signing.** Not solvable from source. On a freshly imaged
   Windows 11, Smart App Control defaults on and hard-blocks unsigned installers
   with no click-through, so this is required for "one click" there. Options:
   Azure Artifact Signing (~$10/mo, individuals US/Canada only), an OV
   certificate (~$150–300/yr), or SignPath Foundation for qualifying OSS. Sign
   `shogun2sync.exe`, `rclone.exe`, `uninstall.exe` and the installer, all
   timestamped — Smart App Control checks every binary, not just the entry
   point. Signing does not remove the first-run SmartScreen prompt; it prevents
   the hard block and lets reputation accrue to the certificate across releases
   instead of resetting on every new file hash. Do not add `-upx`; Wails
   documents antivirus false positives on UPX-compressed binaries.
2. **The missing-WebView2 branch needs a real clean VM.** GitHub's Windows
   images ship WebView2, so CI exercises the installer lifecycle but structurally
   cannot reach that branch.
3. **Rotate the pinned WebView2 runtime, signer leaf, and EULA hash
   deliberately** when Microsoft updates them. A changed upstream object must
   fail the build until reviewed. Note the pinned URL carries a CDN GUID and the
   EULA pin hashes a whole JSON response, so an upstream rotation turns every
   unrelated PR red — consider scoping the strict pin to release builds.
4. `verify-webview2.sh` passes `-ignore-timestamp` and no `-CRLfile`, so no CRL
   or timestamp check happens. The leaf-hash pin makes this moot in practice, but
   do not describe it as CRL verification.

## Release

`packaging/build-release.sh` refuses a dirty or untracked-file-carrying tree, so
its source archive always matches its binaries. The supported release set is one
Windows installer, one Linux `.run`, deb, rpm, Arch package, source archive,
PKGBUILD, and SHA256SUMS. Release CI reruns the verifier and creates
GitHub/Sigstore artifact attestations before publishing.
