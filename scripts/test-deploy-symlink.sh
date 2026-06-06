#!/usr/bin/env bash
#
# Unit tests for the safe_install helper in deploy/install.sh.
# Closes SEC-C3 — installing through a destination symlink must abort.
#
# The function is extracted from deploy/install.sh by sed (same pattern
# used in test-installers.sh) so we test the real shipped version, not
# a copy.
#
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
SRC="$ROOT/deploy/install.sh"
SANDBOX=$(mktemp -d -t vpn-pack-deploy-test.XXXXXX)
trap 'rm -rf "$SANDBOX"' EXIT

FAIL=0
PASS=0
red()   { printf '\033[0;31mFAIL\033[0m %s\n' "$*" >&2; FAIL=$((FAIL+1)); }
green() { printf '\033[0;32mPASS\033[0m %s\n' "$*"; PASS=$((PASS+1)); }

# Extract safe_install function definition into a sourceable file.
fn_file="$SANDBOX/safe_install.sh"
sed -n '/^safe_install() {/,/^}/p' "$SRC" > "$fn_file"
if [[ ! -s "$fn_file" ]]; then
    red "safe_install function not present in deploy/install.sh"
    echo "Results: $PASS passed, $FAIL failed"
    exit "$FAIL"
fi

# Case A: regular file destination — overwrite atomically.
case_a() {
    local dir=$(mktemp -d "$SANDBOX/caseA-XXXX")
    echo "old" > "$dir/dst"
    echo "new" > "$dir/src"
    local out exit_code=0
    out=$(
        bash -c "
            set -e
            $(cat "$fn_file")
            safe_install '$dir/src' '$dir/dst' 0644
        " 2>&1
    ) || exit_code=$?
    if [[ "$exit_code" -ne 0 ]]; then
        red "regular-file dst: expected success, got exit $exit_code (out: $out)"
        return
    fi
    if [[ "$(cat "$dir/dst")" != "new" ]]; then
        red "regular-file dst: expected 'new', got '$(cat "$dir/dst")'"
        return
    fi
    if [[ "$(stat -c '%a' "$dir/dst")" != "644" ]]; then
        red "regular-file dst: mode 0644 expected, got $(stat -c '%a' "$dir/dst")"
        return
    fi
    green "regular-file dst is overwritten atomically with correct mode"
}

# Case B: symlink destination — refuse and leave link target untouched.
case_b() {
    local dir=$(mktemp -d "$SANDBOX/caseB-XXXX")
    echo "victim-content" > "$dir/victim"
    ln -s "$dir/victim" "$dir/dst"
    echo "attacker" > "$dir/src"
    local out exit_code=0
    out=$(
        bash -c "
            set -e
            $(cat "$fn_file")
            safe_install '$dir/src' '$dir/dst' 0644
        " 2>&1
    ) || exit_code=$?
    if [[ "$exit_code" -eq 0 ]]; then
        red "symlink dst: expected non-zero exit, got 0 (out: $out)"
        return
    fi
    if [[ "$(cat "$dir/victim")" != "victim-content" ]]; then
        red "symlink dst: link target modified ($(cat "$dir/victim"))"
        return
    fi
    if [[ ! -L "$dir/dst" ]]; then
        red "symlink dst: destination is no longer a symlink"
        return
    fi
    green "symlink dst is refused; link target untouched"
}

# Case C: missing destination — create with correct mode.
case_c() {
    local dir=$(mktemp -d "$SANDBOX/caseC-XXXX")
    echo "new" > "$dir/src"
    local out exit_code=0
    out=$(
        bash -c "
            set -e
            $(cat "$fn_file")
            safe_install '$dir/src' '$dir/dst' 0600
        " 2>&1
    ) || exit_code=$?
    if [[ "$exit_code" -ne 0 ]]; then
        red "missing dst: expected success, got exit $exit_code (out: $out)"
        return
    fi
    if [[ "$(cat "$dir/dst")" != "new" ]]; then
        red "missing dst: expected 'new', got '$(cat "$dir/dst")'"
        return
    fi
    if [[ "$(stat -c '%a' "$dir/dst")" != "600" ]]; then
        red "missing dst: mode 0600 expected, got $(stat -c '%a' "$dir/dst")"
        return
    fi
    green "missing dst is created with requested mode"
}

# Case D: nginx reload is gated on nginx -t — assertion at source level.
case_d() {
    if grep -qE "nginx -s reload" "$SRC" && ! grep -qE "nginx -t" "$SRC"; then
        red "nginx reload not gated on nginx -t"
        return
    fi
    if ! grep -qE "nginx -t" "$SRC"; then
        red "nginx -t check not present"
        return
    fi
    # Verify the -t check appears textually before -s reload (linear scan).
    local t_line reload_line
    t_line=$(grep -n "nginx -t" "$SRC" | head -1 | cut -d: -f1)
    reload_line=$(grep -n "nginx -s reload" "$SRC" | head -1 | cut -d: -f1)
    if [[ -z "$reload_line" ]]; then
        green "nginx -t present; no reload path (already gone)"
        return
    fi
    if [[ "$t_line" -ge "$reload_line" ]]; then
        red "nginx -t appears AFTER nginx -s reload (line $t_line vs $reload_line)"
        return
    fi
    green "nginx -t gates nginx -s reload"
}

case_a
case_b
case_c
case_d

echo
echo "Results: $PASS passed, $FAIL failed"
exit "$FAIL"
