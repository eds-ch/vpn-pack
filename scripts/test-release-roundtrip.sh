#!/usr/bin/env bash
#
# Cosign sign+verify roundtrip — local regression guard for the
# verify_signature code path shipped in get.sh and install.sh
# (SEC-B4 follow-up Pp3).
#
# The shipped verify_signature uses keyless OIDC pinned to the
# GitHub Actions release workflow URL @ tag ref, issuer
# https://token.actions.githubusercontent.com. A keyless cert is
# only minted at signing time by the production release pipeline,
# so this test cannot reproduce that exact identity locally.
# Instead it exercises the cosign verify-blob *mechanics*
# (signature + bundle binding) with an ephemeral asymmetric keypair,
# and pairs that with a static guard that asserts the shipped
# installers still embed the keyless call pattern. The end-to-end
# production identity trip is gated manually per
# docs/RELEASE-CHECKLIST.md.
#
# Asserts:
#   1. Good sign+verify roundtrip over a mock HTTP endpoint succeeds.
#   2. Tampered artifact verify-blob fails (no false-positive).
#   3. Wrong public key verify-blob fails (no key confusion).
#   4. get.sh and install.sh still call
#      cosign verify-blob --certificate-identity-regexp
#      --certificate-oidc-issuer --bundle.
#
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)

if ! command -v cosign >/dev/null 2>&1; then
    echo "SKIP: cosign not installed — install per https://docs.sigstore.dev/cosign/installation" >&2
    exit 0
fi
if ! command -v python3 >/dev/null 2>&1; then
    echo "SKIP: python3 required for the mock HTTP server" >&2
    exit 0
fi

SANDBOX=$(mktemp -d -t vpn-pack-release-roundtrip.XXXXXX)
SERVER_PID=""
cleanup() {
    [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null || true
    rm -rf "$SANDBOX"
}
trap cleanup EXIT

FAIL=0
PASS=0
red()   { printf '\033[0;31mFAIL\033[0m %s\n' "$*" >&2; FAIL=$((FAIL+1)); }
green() { printf '\033[0;32mPASS\033[0m %s\n' "$*"; PASS=$((PASS+1)); }

# Step 1: artifact set under a mimicked GitHub release path layout.
DIST="$SANDBOX/repos/eds-ch/vpn-pack/releases/download/v0.0.0-test"
mkdir -p "$DIST"
printf 'fake archive contents %s\n' "$(date)" > "$DIST/vpn-pack-0.0.0-test.tar.gz"
( cd "$DIST" && sha256sum vpn-pack-0.0.0-test.tar.gz > checksums.txt )

# Step 2: ephemeral keypair. Empty password keeps the test
# non-interactive; --tlog-upload=false / --insecure-ignore-tlog keep
# everything offline so the test does not touch the public Rekor log.
export COSIGN_PASSWORD=""
( cd "$SANDBOX" && cosign generate-key-pair >/dev/null 2>&1 ) \
    || { red "cosign generate-key-pair failed"; exit "$FAIL"; }

sign_blob() {
    local file=$1
    cosign sign-blob --yes --tlog-upload=false \
        --key "$SANDBOX/cosign.key" \
        --bundle "${file}.cosign.bundle" \
        "$file" >/dev/null 2>&1
}
sign_blob "$DIST/vpn-pack-0.0.0-test.tar.gz" \
    || { red "cosign sign-blob (archive) failed"; exit "$FAIL"; }
sign_blob "$DIST/checksums.txt" \
    || { red "cosign sign-blob (checksums) failed"; exit "$FAIL"; }

# Step 3: local HTTP server mimicking the GitHub release host.
( cd "$SANDBOX" && python3 -u -m http.server 0 >"$SANDBOX/server.log" 2>&1 ) &
SERVER_PID=$!
PORT=""
for _ in $(seq 1 100); do
    if [[ -s "$SANDBOX/server.log" ]]; then
        PORT=$(grep -oE 'port [0-9]+' "$SANDBOX/server.log" | head -1 | awk '{print $2}')
        [[ -n "$PORT" ]] && break
    fi
    sleep 0.05
done
[[ -n "$PORT" ]] || { red "could not detect mock server port"; exit "$FAIL"; }
BASE="http://127.0.0.1:$PORT/repos/eds-ch/vpn-pack/releases/download/v0.0.0-test"

curl -fsS "$BASE/vpn-pack-0.0.0-test.tar.gz" -o "$SANDBOX/dl.tar.gz" \
    || { red "mock server unreachable for archive"; exit "$FAIL"; }
curl -fsS "$BASE/vpn-pack-0.0.0-test.tar.gz.cosign.bundle" -o "$SANDBOX/dl.tar.gz.cosign.bundle" \
    || { red "mock server unreachable for bundle"; exit "$FAIL"; }

# Case 1: good sign+verify roundtrip succeeds.
if cosign verify-blob --insecure-ignore-tlog \
    --key "$SANDBOX/cosign.pub" \
    --bundle "$SANDBOX/dl.tar.gz.cosign.bundle" \
    "$SANDBOX/dl.tar.gz" >/dev/null 2>&1; then
    green "good sign+verify roundtrip succeeds"
else
    red "good sign+verify roundtrip should succeed but failed"
fi

# Case 2: tampered artifact verify-blob fails.
cp "$SANDBOX/dl.tar.gz" "$SANDBOX/tampered.tar.gz"
printf 'x' >> "$SANDBOX/tampered.tar.gz"
if cosign verify-blob --insecure-ignore-tlog \
    --key "$SANDBOX/cosign.pub" \
    --bundle "$SANDBOX/dl.tar.gz.cosign.bundle" \
    "$SANDBOX/tampered.tar.gz" >/dev/null 2>&1; then
    red "tampered artifact must NOT verify"
else
    green "tampered artifact verify-blob fails"
fi

# Case 3: wrong public key verify-blob fails. Stands in for the
# production 'wrong --certificate-identity-regexp' check: different trust
# anchor, must reject.
mkdir -p "$SANDBOX/other"
( cd "$SANDBOX/other" && cosign generate-key-pair >/dev/null 2>&1 ) \
    || { red "second keypair generation failed"; exit "$FAIL"; }
if cosign verify-blob --insecure-ignore-tlog \
    --key "$SANDBOX/other/cosign.pub" \
    --bundle "$SANDBOX/dl.tar.gz.cosign.bundle" \
    "$SANDBOX/dl.tar.gz" >/dev/null 2>&1; then
    red "wrong public key must NOT verify"
else
    green "wrong-key verify-blob fails"
fi

# Case 4: call-pattern guard against silent regression to --key mode
# (or a missing --certificate-identity-regexp / --certificate-oidc-issuer)
# in the shipped installers. Without this the roundtrip above would
# still pass even if production stopped binding to a Fulcio identity.
assert_call_pattern() {
    local installer=$1
    local src="$ROOT/$installer"
    local missing=""
    grep -Eq '(\$COSIGN_BIN["]*[[:space:]]+|cosign[[:space:]]+)verify-blob' "$src" || missing+=" verify-blob"
    grep -q -- '--certificate-identity-regexp' "$src" || missing+=" --certificate-identity-regexp"
    grep -q -- '--certificate-oidc-issuer'     "$src" || missing+=" --certificate-oidc-issuer"
    grep -q -- '--bundle'                      "$src" || missing+=" --bundle"
    if [[ -n "$missing" ]]; then
        red "$installer: production verify missing:$missing"
    else
        green "$installer: keyless verify-blob call pattern present"
    fi
}
assert_call_pattern get.sh
assert_call_pattern install.sh

echo
echo "Results: $PASS passed, $FAIL failed"
exit "$FAIL"
