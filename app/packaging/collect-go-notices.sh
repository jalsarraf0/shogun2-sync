#!/usr/bin/env bash
# Collects the complete root license/notice files for every Go module linked
# into a finished binary. `go version -m` is used rather than go.mod so the
# output follows the actual target-specific payload.
set -euo pipefail

if [[ $# -ne 2 || ! -f "$2" ]]; then
  echo "usage: $(basename "$0") <output.txt> <go-binary>" >&2
  exit 2
fi
OUTPUT="$1"
BINARY="$2"

for tool in awk find go sed sort; do
  command -v "$tool" >/dev/null \
    || { echo "$tool is required to collect Go notices" >&2; exit 1; }
done

WORK_DIR="$(mktemp -d)"
cleanup() { rm -rf "$WORK_DIR"; }
trap cleanup EXIT
MODULES="$WORK_DIR/modules.txt"

go version -m "$BINARY" \
  | awk -F '\t' '$2 == "dep" { print $3 "|" $4 }' \
  | sort -u > "$MODULES"
[[ -s "$MODULES" ]] || { echo "no Go modules found in $BINARY" >&2; exit 1; }

mkdir -p "$(dirname "$OUTPUT")"
TEMP_OUTPUT="$WORK_DIR/GO-THIRD-PARTY-NOTICES.txt"
{
  printf '%s\n\n' 'GO AND WAILS THIRD-PARTY LICENSE NOTICES'
  printf '%s\n' 'The application binary contains the Go standard library and the modules listed below.'
  printf '%s\n\n' 'Their license texts are reproduced here; the Shogun 2 Save Sync MIT license is supplied separately.'

  go_license="$(go env GOROOT)/LICENSE"
  if [[ ! -f "$go_license" && -f /usr/share/licenses/golang/LICENSE ]]; then
    go_license=/usr/share/licenses/golang/LICENSE
  fi
  [[ -f "$go_license" ]] \
    || { echo "could not locate the Go standard library LICENSE" >&2; exit 1; }
  printf '%s\n' '=============================================================================='
  printf '%s\n\n' 'Go standard library'
  sed -n '1,$p' "$go_license"
  printf '\n'

  while IFS='|' read -r module version; do
    module_json="$(go mod download -json "$module@$version")"
    module_dir="$(sed -n 's/^[[:space:]]*"Dir": "\([^"]*\)".*/\1/p' <<<"$module_json")"
    [[ -d "$module_dir" ]] \
      || { echo "could not locate downloaded module $module@$version" >&2; exit 1; }
    mapfile -t license_files < <(
      find "$module_dir" -maxdepth 1 -type f \
        \( -iname 'LICENSE' -o -iname 'LICENSE.*' \
        -o -iname 'COPYING' -o -iname 'COPYING.*' \
        -o -iname 'NOTICE' -o -iname 'NOTICE.*' \) \
        -print | sort
    )
    [[ ${#license_files[@]} -gt 0 ]] \
      || { echo "no root license found for $module@$version" >&2; exit 1; }

    printf '%s\n' '=============================================================================='
    printf '%s %s\n\n' "$module" "$version"
    for license_file in "${license_files[@]}"; do
      printf '%s\n' "--- $(basename "$license_file") ---"
      sed -n '1,$p' "$license_file"
      printf '\n'
    done
  done < "$MODULES"
} > "$TEMP_OUTPUT"

install -m644 "$TEMP_OUTPUT" "$OUTPUT"
echo "Wrote Go dependency notices to $OUTPUT"
