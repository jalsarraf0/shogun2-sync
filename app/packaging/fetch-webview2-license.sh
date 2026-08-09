#!/usr/bin/env bash
# Fetches the reviewed English WebView2 terms from Microsoft's official API and
# writes the complete Evergreen Runtime license body as a self-contained HTML
# document for offline inclusion in the Windows distribution.
set -euo pipefail

EULA_URL="https://developer.microsoft.com/microsoft-edge/api/eula/webview2"
EULA_RESPONSE_SHA256="e15b53f476b66f8335c18436998256dc9862b210242a8e4c7f7e14d2de53591d"

if [[ $# -ne 1 ]]; then
  echo "usage: $(basename "$0") <output.html>" >&2
  exit 2
fi
OUTPUT="$1"

for tool in curl jq sha256sum; do
  command -v "$tool" >/dev/null \
    || { echo "$tool is required to fetch the WebView2 license" >&2; exit 1; }
done

WORK_DIR="$(mktemp -d)"
cleanup() { rm -rf "$WORK_DIR"; }
trap cleanup EXIT
RESPONSE="$WORK_DIR/webview2-eula.json"

curl --fail --location --silent --show-error --output "$RESPONSE" "$EULA_URL"
printf '%s  %s\n' "$EULA_RESPONSE_SHA256" "$RESPONSE" \
  | sha256sum --check --strict -
mkdir -p "$(dirname "$OUTPUT")"
{
  printf '%s\n' '<!doctype html><html lang="en"><head><meta charset="utf-8">'
  printf '%s\n' '<title>Microsoft Edge WebView2 Runtime Software License Terms</title></head><body>'
  jq -er '.evergreenHtml' "$RESPONSE"
  printf '%s\n' '</body></html>'
} > "$OUTPUT"

[[ -s "$OUTPUT" ]] || { echo "Microsoft returned an empty WebView2 license" >&2; exit 1; }
echo "Wrote complete offline WebView2 terms to $OUTPUT"
