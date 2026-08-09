#!/usr/bin/env bash
# Assembles the private GTK/WebKitGTK runtime that lets the generic Linux
# installer work without distro repositories. This runs INSIDE the pinned
# Debian 12 image; build-linux-runtime.sh is the host-side entry point.
#
# Why a bundle at all: Microsoft ships a redistributable WebView2 runtime, so
# the Windows installer can be fully offline. Linux has no redistributable
# WebKitGTK, so "offline" means shipping it ourselves. The native deb/rpm/Arch
# packages deliberately keep using distro WebKit and never see this bundle.
#
# What is deliberately NOT bundled: glibc, the dynamic loader, libstdc++,
# libgcc, the graphics stack, X11, Wayland and fontconfig. Those come from the
# host. This is AppImage's excludelist rule and it is not optional — the host's
# mesa DRI driver is dlopen()ed into this process and is built against the
# host's libstdc++, so forcing an older bundled copy breaks hardware rendering.
set -euo pipefail

: "${RUNTIME_PREFIX:?the install prefix the bundle is pinned to}"
: "${HOST_UID:?}"
: "${HOST_GID:?}"

APP_DIR=/workspace/app
BUNDLE="$APP_DIR/build/runtime"
TRIPLET_LIB=/usr/lib/x86_64-linux-gnu
WEBKIT_LIBEXEC="$TRIPLET_LIB/webkit2gtk-4.1"

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq --no-install-recommends \
  adwaita-icon-theme ca-certificates glib-networking gsettings-desktop-schemas \
  libgdk-pixbuf-2.0-0 libgtk-3-0 libsoup-3.0-0 libwebkit2gtk-4.1-0 \
  libglib2.0-bin shared-mime-info >/dev/null

[[ -x "$APP_DIR/build/bin/shogun2sync" ]] \
  || { echo "the Linux binary must be built before its runtime" >&2; exit 1; }
[[ -x "$WEBKIT_LIBEXEC/WebKitWebProcess" ]] \
  || { echo "Debian's WebKitGTK helper processes are missing" >&2; exit 1; }

rm -rf "$BUNDLE"
mkdir -p "$BUNDLE/lib" "$BUNDLE/share"

# Host-owned objects. Bundling any of these is what makes a relocated GUI stack
# fail on a foreign distro, so the list is enforced rather than advisory.
is_host_owned() {
  case "${1##*/}" in
    ld-linux*|libc.so.6|libm.so.6|libdl.so.2|libpthread.so.0|librt.so.1) return 0 ;;
    libresolv.so.2|libutil.so.1|libnsl.so.1|libcrypt.so.1|libanl.so.1) return 0 ;;
    libstdc++.so.6|libgcc_s.so.1) return 0 ;;
    libGL.so.1|libGLX.so.0|libGLdispatch.so.0|libEGL.so.1|libOpenGL.so.0) return 0 ;;
    libGLESv2.so.2|libdrm.so.2|libgbm.so.1|libglapi.so.0) return 0 ;;
    libX11*|libxcb*|libXext*|libXrender*|libXi.so*|libXfixes*|libXdamage*) return 0 ;;
    libXcomposite*|libXrandr*|libXcursor*|libXinerama*|libXtst*) return 0 ;;
    libXau*|libXdmcp*|libXss*|libXxf86vm*) return 0 ;;
    libfontconfig.so.1|libfreetype.so.6) return 0 ;;
    libwayland*) return 0 ;;
    *) return 1 ;;
  esac
}

echo "==> Copying WebKitGTK helper processes"
# WebKitGTK is multi-process. The UI process execs these by absolute path.
cp -a "$WEBKIT_LIBEXEC" "$BUNDLE/lib/webkit2gtk-4.1"

echo "==> Walking the shared library closure"
bundled=0
host_owned=0
while read -r object; do
  if is_host_owned "$object"; then
    host_owned=$((host_owned + 1))
    continue
  fi
  cp -L "$object" "$BUNDLE/lib/${object##*/}"
  bundled=$((bundled + 1))
done < <(
  {
    ldd "$APP_DIR/build/bin/shogun2sync"
    for helper in "$BUNDLE"/lib/webkit2gtk-4.1/WebKit*Process; do
      ldd "$helper"
    done
  } 2>/dev/null | awk '{for (i = 1; i <= NF; i++) if ($i ~ /^\//) print $i}' | sort -u
)
echo "    bundled $bundled objects, left $host_owned to the host"

# libsoup resolves TLS through a GIO module. Without it every HTTPS request the
# webview or the Google Drive login makes fails with "TLS support unavailable".
echo "==> Copying GIO modules"
cp -a "$TRIPLET_LIB/gio" "$BUNDLE/lib/gio"
[[ -e "$BUNDLE/lib/gio/modules/libgiognutls.so" ]] \
  || { echo "the GIO TLS backend is missing from the bundle" >&2; exit 1; }

echo "==> Copying dlopen-only module directories"
cp -a "$TRIPLET_LIB/gdk-pixbuf-2.0" "$BUNDLE/lib/gdk-pixbuf-2.0"
[[ -d "$TRIPLET_LIB/gtk-3.0" ]] && cp -a "$TRIPLET_LIB/gtk-3.0" "$BUNDLE/lib/gtk-3.0"

echo "==> Copying schemas, MIME data, icons and CA certificates"
mkdir -p "$BUNDLE/share/glib-2.0" "$BUNDLE/share/ca-certificates"
cp -a /usr/share/glib-2.0/schemas "$BUNDLE/share/glib-2.0/schemas"
cp -a /usr/share/mime "$BUNDLE/share/mime"
cp -a /usr/share/icons "$BUNDLE/share/icons"
cp -L /etc/ssl/certs/ca-certificates.crt "$BUNDLE/share/ca-certificates/"

# WebKitGTK 4.1 compiles PKGLIBEXECDIR in as a literal and, unlike the injected
# bundle path, exposes no environment override: WEBKIT_EXEC_PATH is behind
# ENABLE(DEVELOPER_MODE) and is absent from release builds. Rewriting the string
# in place is therefore the only way a relocated copy can find its own helper
# processes. The replacement is NUL-padded to the original length so every
# offset in the ELF is unchanged.
echo "==> Repointing WebKitGTK's compiled-in helper directory"
OLD_LIBEXEC="$WEBKIT_LIBEXEC" NEW_LIBEXEC="$RUNTIME_PREFIX/lib/webkit2gtk-4.1" \
  perl -0777 -pi -e '
    BEGIN {
      $old = $ENV{OLD_LIBEXEC};
      $new = $ENV{NEW_LIBEXEC};
      die "install prefix is too long to patch into WebKitGTK\n"
        if length($new) > length($old);
      $replacement = $new . ("\0" x (length($old) - length($new)));
    }
    $patched = s/\Q$old\E/$replacement/g;
    die "WebKitGTK did not contain its expected helper path\n" unless $patched;
    print STDERR "    rewrote $patched reference(s) to $new\n";
  ' "$BUNDLE/lib/libwebkit2gtk-4.1.so.0"

echo "==> Regenerating loader caches"
# These caches store absolute paths, so they must describe the install prefix
# rather than the directory they were generated in.
glib-compile-schemas "$BUNDLE/share/glib-2.0/schemas"
GDK_PIXBUF_MODULEDIR="$BUNDLE/lib/gdk-pixbuf-2.0/2.10.0/loaders" \
  "$TRIPLET_LIB/gdk-pixbuf-2.0/gdk-pixbuf-query-loaders" \
  > "$BUNDLE/lib/gdk-pixbuf-2.0/2.10.0/loaders.cache"
sed -i "s|$BUNDLE|$RUNTIME_PREFIX|g" \
  "$BUNDLE/lib/gdk-pixbuf-2.0/2.10.0/loaders.cache"

echo "==> Writing the launcher"
cat > "$BUNDLE/shogun2sync.sh" <<LAUNCHER
#!/bin/sh
# Runs the app against its private GTK/WebKitGTK stack. The prefix is fixed
# because WebKitGTK's helper path is patched into the library at build time.
prefix="$RUNTIME_PREFIX"
export LD_LIBRARY_PATH="\$prefix/lib\${LD_LIBRARY_PATH:+:\$LD_LIBRARY_PATH}"
export WEBKIT_INJECTED_BUNDLE_PATH="\$prefix/lib/webkit2gtk-4.1/injected-bundle"
export GIO_EXTRA_MODULES="\$prefix/lib/gio/modules"
export GDK_PIXBUF_MODULEDIR="\$prefix/lib/gdk-pixbuf-2.0/2.10.0/loaders"
export GDK_PIXBUF_MODULE_FILE="\$prefix/lib/gdk-pixbuf-2.0/2.10.0/loaders.cache"
export GTK_PATH="\$prefix/lib/gtk-3.0"
export GSETTINGS_SCHEMA_DIR="\$prefix/share/glib-2.0/schemas"
export XDG_DATA_DIRS="\$prefix/share:\${XDG_DATA_DIRS:-/usr/local/share:/usr/share}"
export SSL_CERT_FILE="\$prefix/share/ca-certificates/ca-certificates.crt"
exec "\$prefix/shogun2sync" "\$@"
LAUNCHER
chmod 755 "$BUNDLE/shogun2sync.sh"

# Shipping GTK and WebKitGTK makes this a redistribution of LGPL libraries, and
# the helper-path rewrite above is a modification of one of them. Record exactly
# what is carried, that it was modified, and where the corresponding source is.
echo "==> Generating the bundled-runtime notice"
{
  cat <<'NOTICE'
Bundled Linux GUI runtime
=========================

The generic Linux installer for Shogun 2 Save Sync carries a private copy of
the GTK 3 and WebKitGTK 4.1 stack so it can install without distribution
repositories. These libraries are the work of their respective authors and are
distributed here under their own licences, most of them the GNU Lesser General
Public License version 2.1 or later.

MODIFICATION NOTICE
-------------------
libwebkit2gtk-4.1.so.0 in this bundle has been modified. WebKitGTK compiles the
absolute path of its helper processes (PKGLIBEXECDIR) into the library and
offers no runtime override in release builds, so that path string was rewritten
in place from

NOTICE
  printf '    %s\n' "$WEBKIT_LIBEXEC"
  echo "to"
  echo
  printf '    %s\n' "$RUNTIME_PREFIX/lib/webkit2gtk-4.1"
  cat <<'NOTICE'

No other change was made to any bundled library.

CORRESPONDING SOURCE
--------------------
Every library below is an unmodified Debian 12 (bookworm) binary except as
noted above. The corresponding source for each is available from Debian at

    https://sources.debian.org/

and can be retrieved for a given package with:

    apt-get source <package>

on a Debian 12 system. The LGPL permits you to relink this application against
your own build of these libraries: the bundle is a plain directory tree, so
replacing a shared object under lib/ is sufficient.

BUNDLED PACKAGES
----------------
NOTICE
  # Report the providing package and exact version of everything shipped, so
  # the source above can be matched to the binary that actually went out.
  # dpkg -S exits non-zero for anything it does not own, which must not abort
  # the notice, so every lookup is explicitly tolerant.
  for object in "$BUNDLE"/lib/*.so* "$BUNDLE"/lib/webkit2gtk-4.1/*; do
    [[ -f "$object" ]] || continue
    dpkg -S "$TRIPLET_LIB/${object##*/}" 2>/dev/null | cut -d: -f1 || true
  done | sort -u > /tmp/bundled-packages.txt
  while read -r package; do
    [[ -n "$package" ]] || continue
    printf '  %-40s %s\n' \
      "$package" "$(dpkg-query -W -f='${Version}' "$package" 2>/dev/null || echo unknown)"
  done < /tmp/bundled-packages.txt
  echo
  echo "FULL LICENCE TEXTS"
  echo "------------------"
  echo "The complete licence text for each package as shipped by Debian follows."
  echo
  for copyright in /usr/share/doc/libwebkit2gtk-4.1-0/copyright \
                   /usr/share/doc/libgtk-3-0/copyright \
                   /usr/share/doc/libsoup-3.0-0/copyright; do
    [[ -r "$copyright" ]] || continue
    echo "=============================================================="
    echo "$copyright"
    echo "=============================================================="
    cat "$copyright"
    echo
  done
} > "$BUNDLE/WEBKITGTK-NOTICE.txt"

echo "==> Verifying nothing host-owned was bundled"
for object in "$BUNDLE"/lib/*.so*; do
  if is_host_owned "$object"; then
    echo "host-owned object must not be bundled: ${object##*/}" >&2
    exit 1
  fi
done

chown -R "$HOST_UID:$HOST_GID" "$BUNDLE"
echo "==> Runtime bundle assembled: $(du -sh "$BUNDLE" | cut -f1)"
