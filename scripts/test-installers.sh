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

# SEC-C1 (ordering): the installer must call verify_signature BEFORE
# extracting the archive. Catches a future edit that downloads + extracts
# first and verifies after — at which point any malicious payload has
# already been written to disk and any pre-install side-effects run.
assert_verify_before_extract() {
    local installer=$1
    local src="$ROOT/$installer"
    local verify_line extract_line
    verify_line=$(grep -n "verify_signature " "$src" | head -1 | cut -d: -f1)
    extract_line=$(grep -nE "^[^#]*tar (xz|x)f " "$src" | head -1 | cut -d: -f1)
    if [[ -z "$verify_line" ]]; then
        red "$installer: verify_signature not invoked"
        return
    fi
    if [[ -z "$extract_line" ]]; then
        red "$installer: tar extraction not found"
        return
    fi
    if [[ "$verify_line" -ge "$extract_line" ]]; then
        red "$installer: verify_signature (line $verify_line) must precede tar (line $extract_line)"
        return
    fi
    green "$installer: verify_signature precedes tar extraction"
}

assert_verify_before_extract get.sh
assert_verify_before_extract install.sh

# Task 8.12 Step 3: full-flow tamper test. Sources install.sh's vp_*
# functions (post-refactor) and runs vp_download_and_verify against a
# real signed-then-tampered release fixture. Asserts both that the
# function aborts non-zero AND that no archive extraction occurred in
# the staging dir. Catches a future regression where verify_signature
# is moved after extract, removed, or where tar is merged into
# vp_download_and_verify before the verify call. SKIPs without cosign
# or python3.
assert_full_flow_tamper_aborts_before_extract() {
    if ! command -v cosign >/dev/null 2>&1; then
        printf 'SKIP install.sh: full-flow tamper test needs cosign\n' >&2
        return
    fi
    if ! command -v python3 >/dev/null 2>&1; then
        printf 'SKIP install.sh: full-flow tamper test needs python3\n' >&2
        return
    fi

    local case_dir; case_dir=$(mktemp -d "$SANDBOX/tamper-XXXX")
    local www="$case_dir/www"
    local fixed_tmp="$case_dir/install_tmp"
    local snap="$case_dir/snapshot"
    local stubs="$case_dir/stubs"
    local libsh="$case_dir/install.lib.sh"
    mkdir -p "$www" "$fixed_tmp" "$stubs"

    # Real signed fixture: ephemeral keypair, archive + checksums signed.
    export COSIGN_PASSWORD=""
    ( cd "$case_dir" && cosign generate-key-pair >/dev/null 2>&1 ) \
        || { red "install.sh tamper: cosign key generation failed"; return; }

    # Real tar.gz fixture so a regression that moves tar BEFORE
    # verify_signature will actually extract content (and trip the
    # no-extraction assertion). A fake non-tar payload would let tar
    # exit non-zero, hiding the regression.
    local build="$case_dir/build/vpn-pack-0.0.0-test"
    mkdir -p "$build"
    cat > "$build/install.sh" <<'EOS'
#!/bin/sh
exit 0
EOS
    chmod +x "$build/install.sh"
    printf '0.0.0-test\n' > "$build/VERSION"
    ( cd "$case_dir/build" && tar czf "$www/vpn-pack-0.0.0-test.tar.gz" vpn-pack-0.0.0-test )
    ( cd "$www" && sha256sum vpn-pack-0.0.0-test.tar.gz > checksums.txt )
    cosign sign-blob --yes --tlog-upload=false \
        --key "$case_dir/cosign.key" \
        --bundle "$www/vpn-pack-0.0.0-test.tar.gz.cosign.bundle" \
        "$www/vpn-pack-0.0.0-test.tar.gz" >/dev/null 2>&1 \
        || { red "install.sh tamper: sign-blob archive failed"; return; }
    cosign sign-blob --yes --tlog-upload=false \
        --key "$case_dir/cosign.key" \
        --bundle "$www/checksums.txt.cosign.bundle" \
        "$www/checksums.txt" >/dev/null 2>&1 \
        || { red "install.sh tamper: sign-blob checksums failed"; return; }

    # Tamper the archive bundle — last byte flipped.
    local tail_byte
    tail_byte=$(tail -c 1 "$www/vpn-pack-0.0.0-test.tar.gz.cosign.bundle")
    if [[ "$tail_byte" = "x" ]]; then
        printf 'y' >> "$www/vpn-pack-0.0.0-test.tar.gz.cosign.bundle"
    else
        printf 'x' >> "$www/vpn-pack-0.0.0-test.tar.gz.cosign.bundle"
    fi

    # Mock HTTP server.
    local server_log="$case_dir/server.log"
    ( cd "$www" && python3 -u -m http.server 0 >"$server_log" 2>&1 ) &
    local pid=$!
    trap "kill $pid 2>/dev/null || true" RETURN
    local port=""
    for _ in $(seq 1 100); do
        if [[ -s "$server_log" ]]; then
            port=$(grep -oE 'port [0-9]+' "$server_log" | head -1 | awk '{print $2}')
            [[ -n "$port" ]] && break
        fi
        sleep 0.05
    done
    if [[ -z "$port" ]]; then
        red "install.sh tamper: mock server did not start"
        kill "$pid" 2>/dev/null || true
        return
    fi
    local base="http://127.0.0.1:$port"

    # PATH stubs:
    #   - cosign: translate keyless verify-blob → --key mode (real crypto).
    #   - mktemp: return our fixed staging dir so the test can inspect it.
    #   - rm: no-op so the EXIT trap inside vp_download_and_verify leaves
    #     the staging dir intact for post-mortem assertions.
    local pub="$case_dir/cosign.pub"
    # Resolve the real cosign path BEFORE creating the shim. The subshell
    # below runs with PATH starting at $stubs (where cosign IS the shim),
    # so a runtime `command -v cosign` inside the shim would resolve to
    # the shim itself and exec a fork bomb. Pin the absolute path here.
    local real_cosign; real_cosign=$(command -v cosign)
    cat > "$stubs/cosign" <<EOSH
#!/bin/sh
# install.sh's verify_signature uses keyless OIDC. Locally we cannot
# mint a Fulcio cert for the production identity, so translate the
# invocation to --key mode using the ephemeral pub generated above.
real='$real_cosign'
pub='$pub'
verb=\$1; shift
bundle=""; file=""
while [ \$# -gt 0 ]; do
  case "\$1" in
    --certificate-identity|--certificate-oidc-issuer) shift 2 ;;
    --bundle) bundle=\$2; shift 2 ;;
    *) file=\$1; shift ;;
  esac
done
exec "\$real" "\$verb" --insecure-ignore-tlog --key "\$pub" --bundle "\$bundle" "\$file"
EOSH
    cat > "$stubs/mktemp" <<EOSH
#!/bin/sh
case "\$*" in
  *vpn-pack-install*) echo "$fixed_tmp"; exit 0 ;;
esac
exec /usr/bin/mktemp "\$@"
EOSH
    cat > "$stubs/rm" <<EOSH
#!/bin/sh
# Snapshot the staging dir before any cleanup so a regression where tar
# runs before verify still leaves evidence on disk for the test.
for arg in "\$@"; do
  case "\$arg" in
    "$fixed_tmp")
      cp -a "$fixed_tmp" "$snap" 2>/dev/null || true
      ;;
  esac
done
exit 0
EOSH
    chmod +x "$stubs"/*

    # Source vp_* functions only — strip the orchestration tail.
    sed '/^vp_preflight$/,$ d' "$ROOT/install.sh" > "$libsh"

    # Drive vp_download_and_verify in a subshell with the test globals
    # and PATH stubs.
    local exit_code=0
    (
        export PATH="$stubs:/usr/local/bin:/usr/bin:/bin:/usr/sbin"
        # shellcheck source=/dev/null
        . "$libsh"
        ASSET_URL="$base/vpn-pack-0.0.0-test.tar.gz"
        ARCHIVE_BUNDLE_URL="$base/vpn-pack-0.0.0-test.tar.gz.cosign.bundle"
        CHECKSUMS_URL="$base/checksums.txt"
        CHECKSUMS_BUNDLE_URL="$base/checksums.txt.cosign.bundle"
        vp_download_and_verify
    ) >/dev/null 2>&1 || exit_code=$?

    kill "$pid" 2>/dev/null || true

    if [[ "$exit_code" -eq 0 ]]; then
        red "install.sh tamper: vp_download_and_verify must abort on tampered bundle (got exit 0)"
        return
    fi

    # Sanity: ensure the test actually exercised the download phase.
    # Without this guard, a script that aborts before download would
    # pass the no-extraction assertion vacuously.
    if ! ls "$fixed_tmp"/vpn-pack-*.tar.gz >/dev/null 2>&1; then
        red "install.sh tamper: archive missing from staging — vp_download_and_verify aborted before download"
        return
    fi

    # The real assertion: no extracted vpn-pack-*/install.sh inside the
    # staging dir. If a future refactor moves tar before verify_signature,
    # this directory will appear and the test fails.
    if ls "$fixed_tmp"/vpn-pack-*/install.sh >/dev/null 2>&1; then
        red "install.sh tamper: tar extracted before verify (vpn-pack-*/install.sh found in staging)"
        return
    fi
    # Belt-and-braces: also check the pre-rm snapshot, in case a future
    # change adds a cleanup step that wipes extracted content before
    # trap fires.
    if ls "$snap"/vpn-pack-*/install.sh >/dev/null 2>&1; then
        red "install.sh tamper: tar extracted before verify (vpn-pack-*/install.sh found in pre-rm snapshot)"
        return
    fi

    green "install.sh: tampered bundle aborts vp_download_and_verify before extract"
}

assert_full_flow_tamper_aborts_before_extract

echo
echo "Results: $PASS passed, $FAIL failed"
exit "$FAIL"
