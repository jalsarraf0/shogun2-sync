#!/usr/bin/env bash
# Builds no state; inspects finished deb/rpm/Arch artifacts for their complete
# dependency metadata and the exact runnable payload shared by all three.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
DIST_DIR="${2:-$REPO_ROOT/dist}"

if [[ $# -lt 1 || ! "$1" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "usage: $(basename "$0") <version> [package-directory]" >&2
  exit 2
fi
VERSION="$1"
RCLONE_VERSION="$(sed -n 's/^RCLONE_VERSION="\([^"]*\)"/\1/p' "$SCRIPT_DIR/build-release.sh")"

for tool in cpio dpkg-deb rpm rpm2cpio sha256sum tar; do
  command -v "$tool" >/dev/null \
    || { echo "$tool is required to verify Linux packages" >&2; exit 1; }
done

DEB="$DIST_DIR/shogun2sync_${VERSION}_amd64.deb"
RPM="$DIST_DIR/shogun2sync-${VERSION}-1.x86_64.rpm"
ARCH="$DIST_DIR/shogun2sync-${VERSION}-1-x86_64.pkg.tar.zst"
for artifact in "$DEB" "$RPM" "$ARCH"; do
  [[ -s "$artifact" ]] || { echo "missing Linux package: $artifact" >&2; exit 1; }
done

required_dependencies=(ca-certificates systemd xdg-utils)
required_paths=(
  usr/bin/shogun2sync
  usr/lib/shogun2sync/rclone
  usr/share/licenses/shogun2sync/LICENSE
  usr/share/licenses/shogun2sync/GO-THIRD-PARTY-NOTICES.txt
  usr/share/licenses/shogun2sync/rclone-COPYING
  usr/share/applications/shogun2sync.desktop
  usr/share/icons/hicolor/512x512/apps/shogun2sync.png
)

# Match a package name as a whole token. A bare substring search is too weak to
# be a gate: rpm's auto-generated requires contain "libsystemd.so.0()(64bit)"
# and "glibc(x86-64)", so grep -F "systemd" and grep -F "glibc" pass even if the
# explicit Requires were deleted from nfpm.yaml.
# Match at the start of a dependency token. Debian's list is deliberately
# checked by prefix, because the real names carry an ABI suffix the alternation
# has to tolerate (libgtk-3 -> "libgtk-3-0t64 | libgtk-3-0"). Anchoring the
# start is still what makes this a gate: an unanchored search would let
# "libsystemd.so.0()(64bit)" satisfy a check for systemd.
#
# Package names here contain only letters, digits, '-' and '.', so escaping the
# dot is enough to keep the name a literal.
requires_token() {
  local dependency="$1" haystack="$2"
  grep -Eq "(^|[[:space:],|])${dependency//./\\.}" <<<"$haystack"
}

echo "==> Verifying the Debian package"
deb_dependencies="$(dpkg-deb -f "$DEB" Depends)"
for dependency in "${required_dependencies[@]}" libc6 libgtk-3 libwebkit2gtk-4.1; do
  requires_token "$dependency" "$deb_dependencies" \
    || { echo "Debian dependency missing: $dependency" >&2; exit 1; }
done
deb_contents="$(dpkg-deb -c "$DEB")"
for path in "${required_paths[@]}"; do
  grep -Fq "$path" <<<"$deb_contents" \
    || { echo "Debian package file missing: $path" >&2; exit 1; }
done

echo "==> Verifying the RPM package"
rpm_dependencies="$(rpm -qp --requires "$RPM")"
for dependency in "${required_dependencies[@]}" glibc gtk3 webkit2gtk4.1; do
  grep -Eq "^${dependency//./\\.}([[:space:]]|\$)" <<<"$rpm_dependencies" \
    || { echo "RPM dependency missing: $dependency" >&2; exit 1; }
done
rpm_contents="$(rpm -qlp "$RPM")"
for path in "${required_paths[@]}"; do
  grep -Fxq "/$path" <<<"$rpm_contents" \
    || { echo "RPM package file missing: $path" >&2; exit 1; }
done

echo "==> Verifying the Arch package"
arch_metadata="$(tar --zstd -xOf "$ARCH" .PKGINFO)"
for dependency in "${required_dependencies[@]}" glibc gtk3 webkit2gtk-4.1; do
  grep -Fxq "depend = $dependency" <<<"$arch_metadata" \
    || { echo "Arch dependency missing: $dependency" >&2; exit 1; }
done
arch_contents="$(tar --zstd -tf "$ARCH")"
for path in "${required_paths[@]}"; do
  grep -Fq "$path" <<<"$arch_contents" \
    || { echo "Arch package file missing: $path" >&2; exit 1; }
done

echo "==> Extracting and comparing installed payloads"
extract_dir="$(mktemp -d)"
cleanup() { rm -rf "$extract_dir"; }
trap cleanup EXIT
mkdir -p "$extract_dir/deb" "$extract_dir/rpm" "$extract_dir/arch"
dpkg-deb -x "$DEB" "$extract_dir/deb"
(
  cd "$extract_dir/rpm"
  # Two portability traps here. rpm2cpio on Debian and Ubuntu exits non-zero
  # even after writing the whole payload, which under pipefail would abort this
  # script with no diagnostic, so its status is deliberately ignored and the
  # extraction is judged by cpio and by the file checks below. And GNU cpio
  # honours absolute paths from the archive in copy-in mode, so without
  # --no-absolute-filenames it writes the package into the real /usr.
  rpm2cpio "$RPM" > payload.cpio || true
  [[ -s payload.cpio ]] || { echo "rpm2cpio produced no payload" >&2; exit 1; }
  cpio -idmu --quiet --no-absolute-filenames < payload.cpio
  rm -f payload.cpio
)
for path in usr/bin/shogun2sync usr/lib/shogun2sync/rclone; do
  [[ -f "$extract_dir/rpm/$path" ]] \
    || { echo "the RPM payload did not extract $path" >&2; exit 1; }
done
tar --zstd -xf "$ARCH" -C "$extract_dir/arch"

for package in deb rpm arch; do
  "$extract_dir/$package/usr/lib/shogun2sync/rclone" version \
    | grep -Fq "rclone v$RCLONE_VERSION" \
    || { echo "$package does not contain rclone $RCLONE_VERSION" >&2; exit 1; }
  if ldd "$extract_dir/$package/usr/bin/shogun2sync" | grep -Fq 'not found'; then
    echo "$package app has an unresolved shared library on the CI host" >&2
    exit 1
  fi
done

for path in usr/bin/shogun2sync usr/lib/shogun2sync/rclone; do
  expected="$(sha256sum "$extract_dir/deb/$path" | awk '{print $1}')"
  for package in rpm arch; do
    actual="$(sha256sum "$extract_dir/$package/$path" | awk '{print $1}')"
    [[ "$actual" == "$expected" ]] \
      || { echo "$path differs between deb and $package" >&2; exit 1; }
  done
done

echo "All deb, rpm, and Arch dependencies and payloads are present."
