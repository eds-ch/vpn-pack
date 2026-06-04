#!/usr/bin/env bash
#
# Characterization test for install.sh (Task 8.12 Step 1).
#
# Captures the user-visible stdout+stderr+exit-code of install.sh end-
# to-end against a mocked GitHub release, and compares to a committed
# golden. The point is to lock in the shipped install path *before*
# Task 8.12's refactor (vp_preflight / vp_resolve_release /
# vp_download_and_verify / vp_extract / vp_run_inner_installer), so
# any output regression introduced by the refactor is caught.
#
# Mocked surface (the rest is exercised for real):
#   - /persistent is redirected to a tmpdir (path is hardcoded in
#     install.sh; this substitution is the only source-level deviation).
#   - GITHUB_API points to a local python3 http.server.
#   - cosign is PATH-shimmed to always succeed; the real crypto
#     roundtrip lives in scripts/test-release-roundtrip.sh.
#   - id -u, uname -m, ubnt-device-info, systemctl are PATH-shimmed
#     to satisfy preflight on a non-UniFi dev host.
#   - curl, tar, sha256sum, iptables, ip are the real system binaries.
#
# UPDATE_GOLDEN=1 regenerates scripts/golden/installer-stdout.txt
# (intended for use after the refactor, when output deliberately
# changes — review the diff and commit alongside the refactor).
#
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
GOLDEN="$ROOT/scripts/golden/installer-stdout.txt"

SANDBOX=$(mktemp -d -t vpn-pack-installer-char.XXXXXX)
SERVER_PID=""
cleanup() {
    [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null || true
    rm -rf "$SANDBOX"
}
trap cleanup EXIT

# ── Fake release payload ──────────────────────────────────────────
BUILD="$SANDBOX/build/vpn-pack-0.0.0-test"
mkdir -p "$BUILD"
cat > "$BUILD/install.sh" <<'EOS'
#!/bin/sh
printf '[mock inner installer ran]\n'
exit 0
EOS
chmod +x "$BUILD/install.sh"
printf '0.0.0-test\n' > "$BUILD/VERSION"

WWW="$SANDBOX/www"
mkdir -p "$WWW"
( cd "$SANDBOX/build" && tar czf "$WWW/vpn-pack-0.0.0-test.tar.gz" vpn-pack-0.0.0-test )
( cd "$WWW" && sha256sum vpn-pack-0.0.0-test.tar.gz > checksums.txt )
# Stub cosign bundles — the always-OK cosign stub does not read them.
printf 'stub-bundle' > "$WWW/vpn-pack-0.0.0-test.tar.gz.cosign.bundle"
printf 'stub-bundle' > "$WWW/checksums.txt.cosign.bundle"

# ── Mock HTTP server ──────────────────────────────────────────────
( cd "$WWW" && python3 -u -m http.server 0 >"$SANDBOX/server.log" 2>&1 ) &
SERVER_PID=$!
PORT=""
for _ in $(seq 1 100); do
    if [[ -s "$SANDBOX/server.log" ]]; then
        PORT=$(grep -oE 'port [0-9]+' "$SANDBOX/server.log" | head -1 | awk '{print $2}')
        [[ -n "$PORT" ]] && break
    fi
    sleep 0.05
done
[[ -n "$PORT" ]] || { echo "FAIL: mock server did not start" >&2; exit 1; }
BASE_URL="http://127.0.0.1:$PORT"

# Mock release JSON served at /release.json
cat > "$WWW/release.json" <<EOJ
{
  "tag_name": "v0.0.0-test",
  "assets": [
    {"browser_download_url": "$BASE_URL/vpn-pack-0.0.0-test.tar.gz"},
    {"browser_download_url": "$BASE_URL/vpn-pack-0.0.0-test.tar.gz.cosign.bundle"},
    {"browser_download_url": "$BASE_URL/checksums.txt"},
    {"browser_download_url": "$BASE_URL/checksums.txt.cosign.bundle"}
  ]
}
EOJ

# ── PATH stubs for preflight + cosign ─────────────────────────────
STUBS="$SANDBOX/stubs"
mkdir -p "$STUBS"

cat > "$STUBS/id" <<'EOS'
#!/bin/sh
if [ "${1:-}" = "-u" ]; then echo 0; exit 0; fi
exec /usr/bin/id "$@"
EOS

cat > "$STUBS/uname" <<'EOS'
#!/bin/sh
if [ "${1:-}" = "-m" ]; then echo aarch64; exit 0; fi
exec /usr/bin/uname "$@"
EOS

cat > "$STUBS/ubnt-device-info" <<'EOS'
#!/bin/sh
case "${1:-}" in
  model_short) echo "UDM-SE" ;;
  model)       echo "UniFi Dream Machine SE" ;;
  firmware)    echo "4.0.0" ;;
  *) exit 1 ;;
esac
EOS

cat > "$STUBS/systemctl" <<'EOS'
#!/bin/sh
exit 0
EOS

# Always-OK cosign stub — characterization is about install.sh's
# orchestration output, not crypto. test-release-roundtrip.sh covers
# the real verify-blob mechanics.
cat > "$STUBS/cosign" <<'EOS'
#!/bin/sh
exit 0
EOS

chmod +x "$STUBS"/*

# ── Patched install.sh ────────────────────────────────────────────
PERSISTENT_ROOT="$SANDBOX/persistent"
mkdir -p "$PERSISTENT_ROOT"

PATCHED="$SANDBOX/install.patched.sh"
# Substitutions:
#   - GITHUB_API line → mock JSON URL
#   - /persistent literal → tmpdir (refactor must preserve same literal)
sed \
    -e "s|^GITHUB_API=.*|GITHUB_API=\"$BASE_URL/release.json\"|" \
    -e "s|/persistent|$PERSISTENT_ROOT|g" \
    "$ROOT/install.sh" > "$PATCHED"

# ── Run ───────────────────────────────────────────────────────────
EXIT=0
OUTPUT=$(
    PATH="$STUBS:/usr/local/bin:/usr/bin:/bin:/usr/sbin" \
    HOME="$SANDBOX/home" \
    sh "$PATCHED" 2>&1
) || EXIT=$?

# ── Normalize ─────────────────────────────────────────────────────
# Strip ANSI, replace tmpdir paths, port, MB-free, archive hashes.
NORMALIZED=$(
    printf '%s\n' "$OUTPUT" | sed \
        -e 's/\x1b\[[0-9;]*m//g' \
        -e "s|$SANDBOX|<TMPDIR>|g" \
        -e "s|http://127.0.0.1:$PORT|<MOCK_API>|g" \
        -e 's/has [0-9][0-9]*MB free/has <N>MB free/g'
)

# ── Compare / write golden ────────────────────────────────────────
mkdir -p "$ROOT/scripts/golden"

if [[ "${UPDATE_GOLDEN:-0}" = "1" ]] || [[ ! -f "$GOLDEN" ]]; then
    {
        printf '%s\n' "$NORMALIZED"
        printf '__EXIT__=%s\n' "$EXIT"
    } > "$GOLDEN"
    echo "Wrote golden: $GOLDEN (exit=$EXIT)"
    exit 0
fi

GOLDEN_BODY=$(sed '/^__EXIT__=/d' "$GOLDEN")
GOLDEN_EXIT=$(grep '^__EXIT__=' "$GOLDEN" | cut -d= -f2)

if [[ "$EXIT" != "$GOLDEN_EXIT" ]] || [[ "$NORMALIZED" != "$GOLDEN_BODY" ]]; then
    echo "FAIL: characterization diverged from golden" >&2
    echo "--- golden (exit=$GOLDEN_EXIT) ---" >&2
    diff -u <(printf '%s\n' "$GOLDEN_BODY") <(printf '%s\n' "$NORMALIZED") >&2 || true
    echo "--- actual exit=$EXIT golden exit=$GOLDEN_EXIT ---" >&2
    exit 1
fi

echo "PASS install.sh output matches golden (exit=$EXIT)"
