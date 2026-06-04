#!/usr/bin/env bash
#
# Unit tests for verify_binary_integrity in deploy/uninstall.sh.
# Closes SEC-C4 — uninstall.sh must refuse to execute the manager
# binary if its sha256 does not match the value recorded by install.sh.
#
# Contract (assertions):
#   - matching sha256 → exit 0
#   - mismatched sha256 → non-zero (rc 2)
#   - missing .expected-sha256 file → non-zero (rc 1)
#   - missing binary → non-zero
#
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
SRC="$ROOT/deploy/uninstall.sh"
SANDBOX=$(mktemp -d -t vpn-pack-uninstall-test.XXXXXX)
trap 'rm -rf "$SANDBOX"' EXIT

FAIL=0
PASS=0
red()   { printf '\033[0;31mFAIL\033[0m %s\n' "$*" >&2; FAIL=$((FAIL+1)); }
green() { printf '\033[0;32mPASS\033[0m %s\n' "$*"; PASS=$((PASS+1)); }

fn_file="$SANDBOX/fn.sh"
sed -n '/^verify_binary_integrity() {/,/^}/p' "$SRC" > "$fn_file"
if [[ ! -s "$fn_file" ]]; then
    red "verify_binary_integrity function not present in deploy/uninstall.sh"
    echo "Results: $PASS passed, $FAIL failed"
    exit "$FAIL"
fi

run_case() {
    local label=$1 bin=$2 expected_file=$3 want_rc=$4
    local out rc=0
    out=$(
        bash -c "
            set -e
            $(cat "$fn_file")
            verify_binary_integrity '$bin' '$expected_file'
        " 2>&1
    ) || rc=$?
    if [[ "$rc" -ne "$want_rc" ]]; then
        red "$label (got rc $rc, want $want_rc; out: $out)"
        return
    fi
    green "$label"
}

# Case A: matching sha256 → rc 0.
caseA_dir=$(mktemp -d "$SANDBOX/A-XXXX")
printf '#!/bin/sh\n' > "$caseA_dir/bin"
chmod +x "$caseA_dir/bin"
sha256sum "$caseA_dir/bin" | awk '{print $1}' > "$caseA_dir/expected"
run_case "matching sha256 → rc 0" "$caseA_dir/bin" "$caseA_dir/expected" 0

# Case B: mismatched sha256 → rc 2.
caseB_dir=$(mktemp -d "$SANDBOX/B-XXXX")
printf '#!/bin/sh\nexit 0\n' > "$caseB_dir/bin"
chmod +x "$caseB_dir/bin"
echo "0000000000000000000000000000000000000000000000000000000000000000" > "$caseB_dir/expected"
run_case "mismatched sha256 → rc 2" "$caseB_dir/bin" "$caseB_dir/expected" 2

# Case C: missing expected-sha256 file → rc 1.
caseC_dir=$(mktemp -d "$SANDBOX/C-XXXX")
printf '#!/bin/sh\n' > "$caseC_dir/bin"
chmod +x "$caseC_dir/bin"
run_case "missing expected-sha file → rc 1" "$caseC_dir/bin" "$caseC_dir/missing" 1

# Case D: missing binary → rc 2.
caseD_dir=$(mktemp -d "$SANDBOX/D-XXXX")
echo "0000" > "$caseD_dir/expected"
run_case "missing binary → rc 2" "$caseD_dir/nonexistent" "$caseD_dir/expected" 2

# Case E: source-level — install.sh records .expected-sha256
if ! grep -qE 'sha256sum.*\${?BIN_DIR.*vpn-pack-manager.*>' "$ROOT/deploy/install.sh"; then
    red "deploy/install.sh does not record .expected-sha256 for the manager binary"
else
    green "deploy/install.sh records .expected-sha256"
fi

# Case F: source-level — uninstall.sh refuses --cleanup on integrity failure
# unless --force-cleanup is passed.
if ! grep -qE "FORCE_CLEANUP" "$SRC"; then
    red "deploy/uninstall.sh lacks --force-cleanup override"
elif ! grep -qE "verify_binary_integrity " "$SRC"; then
    red "deploy/uninstall.sh does not call verify_binary_integrity before --cleanup"
else
    green "deploy/uninstall.sh gates --cleanup on integrity check"
fi

echo
echo "Results: $PASS passed, $FAIL failed"
exit "$FAIL"
