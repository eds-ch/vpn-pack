#!/usr/bin/env bash
#
# Unit tests for the verify_signature function embedded in get.sh and
# install.sh. Asserts:
#   1. Missing cosign        → exit non-zero (SEC-C1: no silent fallback).
#   2. cosign verify fails   → exit non-zero (tampered bundle).
#   3. cosign verify passes  → exit zero.
#
# The function is extracted from each installer via sed so we test the
# real shipped implementation, not a copy. PATH is controlled per case
# so cosign behaviour is deterministic.
#
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
SANDBOX=$(mktemp -d -t vpn-pack-installer-test.XXXXXX)
trap 'rm -rf "$SANDBOX"' EXIT

# Strip any directory that already contains cosign so the test does not
# silently pick up a real cosign when exercising the "missing" case.
SYSTEM_PATH=""
IFS=":" read -ra _path_parts <<< "$PATH"
for _p in "${_path_parts[@]}"; do
    if [[ -z "$_p" || -x "$_p/cosign" ]]; then continue; fi
    SYSTEM_PATH="${SYSTEM_PATH:+$SYSTEM_PATH:}$_p"
done

FAIL=0
PASS=0

red()   { printf '\033[0;31mFAIL\033[0m %s\n' "$*" >&2; FAIL=$((FAIL+1)); }
green() { printf '\033[0;32mPASS\033[0m %s\n' "$*"; PASS=$((PASS+1)); }

extract_verify() {
    local src=$1 dst=$2
    if ! sed -n '/^verify_signature() {/,/^}/p' "$src" > "$dst"; then
        echo "extract failed for $src" >&2
        exit 2
    fi
    if [[ ! -s "$dst" ]]; then
        echo "verify_signature not found in $src" >&2
        exit 2
    fi
}

run_case() {
    local label=$1 src=$2 cosign_mode=$3 expect_exit=$4

    local case_dir
    case_dir=$(mktemp -d "$SANDBOX/case-XXXX")

    local fn_file="$case_dir/fn.sh"
    extract_verify "$src" "$fn_file"

    # Build sandbox PATH: only contains the fake cosign (or nothing).
    case "$cosign_mode" in
        missing) ;;
        fail)
            cat > "$case_dir/cosign" <<'EOS'
#!/usr/bin/env bash
echo "cosign: verification failed" >&2
exit 1
EOS
            chmod +x "$case_dir/cosign"
            ;;
        ok)
            cat > "$case_dir/cosign" <<'EOS'
#!/usr/bin/env bash
exit 0
EOS
            chmod +x "$case_dir/cosign"
            ;;
        *) echo "bad cosign_mode: $cosign_mode" >&2; exit 2 ;;
    esac

    # Always provide a command shim so command -v can still be used.
    : > "$case_dir/artifact"
    : > "$case_dir/artifact.bundle"

    local out exit_code=0
    out=$(
        PATH="$case_dir:$SYSTEM_PATH" \
        bash -c "
            set -e
            $(cat "$fn_file")
            verify_signature '$case_dir/artifact' '$case_dir/artifact.bundle'
        " 2>&1
    ) || exit_code=$?

    if [[ "$exit_code" -ne "$expect_exit" ]]; then
        red "$label (got exit $exit_code, want $expect_exit; output: $out)"
        return
    fi
    green "$label"
}

for installer in get.sh install.sh; do
    src="$ROOT/$installer"
    if ! grep -q '^verify_signature() {' "$src"; then
        red "$installer: verify_signature function not present"
        continue
    fi
    run_case "$installer: missing cosign exits non-zero" "$src" missing 1
    run_case "$installer: failed cosign exits non-zero"  "$src" fail    1
    run_case "$installer: passing cosign exits zero"     "$src" ok      0
done

# SEC-C2: installers must use mktemp -d (not predictable paths) and
# tighten the resulting directory to 0700 with an EXIT-cleanup trap.
assert_secure_tmp() {
    local installer=$1
    local src="$ROOT/$installer"
    if grep -qE '^[^#]*=[[:space:]]*"?/tmp/vpn-pack-install"?[[:space:]]*$' "$src"; then
        red "$installer: predictable /tmp/vpn-pack-install path present"
        return
    fi
    if ! grep -qE 'mktemp -d' "$src"; then
        red "$installer: mktemp -d missing"
        return
    fi
    if ! grep -qE 'chmod 700' "$src"; then
        red "$installer: chmod 700 on staging dir missing"
        return
    fi
    if ! grep -qE "trap .*rm -rf.* EXIT" "$src"; then
        red "$installer: EXIT-cleanup trap on staging dir missing"
        return
    fi
    green "$installer: staging dir is mktemp+0700+trap"
}

assert_secure_tmp get.sh
assert_secure_tmp install.sh

echo
echo "Results: $PASS passed, $FAIL failed"
exit "$FAIL"
