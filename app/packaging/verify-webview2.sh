#!/usr/bin/env bash
# Verifies the exact offline WebView2 payload and its primary Microsoft
# Authenticode signer. A hash alone is not enough when rotating the pin.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=webview2-runtime.env
# shellcheck disable=SC1091
source "$SCRIPT_DIR/webview2-runtime.env"

if [[ $# -ne 1 || ! -f "$1" ]]; then
  echo "usage: $(basename "$0") <$WEBVIEW2_RUNTIME_FILE>" >&2
  exit 2
fi
RUNTIME="$1"

for tool in curl openssl osslsigncode sha256sum; do
  command -v "$tool" >/dev/null \
    || { echo "$tool is required to verify WebView2" >&2; exit 1; }
done

printf '%s  %s\n' "$WEBVIEW2_RUNTIME_SHA256" "$RUNTIME" \
  | sha256sum --check --strict -

VERIFY_DIR="$(mktemp -d)"
cleanup() { rm -rf "$VERIFY_DIR"; }
trap cleanup EXIT

ROOT_DER="$VERIFY_DIR/MicrosoftRootCertificateAuthority2011.cer"
ROOT_PEM="$VERIFY_DIR/MicrosoftRootCertificateAuthority2011.pem"
curl --fail --location --silent --show-error \
  --output "$ROOT_DER" "$MICROSOFT_ROOT_CERT_URL"
printf '%s  %s\n' "$MICROSOFT_ROOT_CERT_SHA256" "$ROOT_DER" \
  | sha256sum --check --strict -
openssl x509 -inform DER -in "$ROOT_DER" -out "$ROOT_PEM"

# Index 0 is Microsoft's public code-signing chain. Index 1 is an intentionally
# nested, self-signed EdgeBuild signature and must not replace the public signer
# identity checked here.
osslsigncode verify -index 0 \
  -CAfile "$ROOT_PEM" \
  -ignore-timestamp \
  -require-leaf-hash "sha256:$WEBVIEW2_SIGNER_LEAF_SHA256" \
  -in "$RUNTIME"

echo "Verified WebView2 $WEBVIEW2_RUNTIME_VERSION from Microsoft."
