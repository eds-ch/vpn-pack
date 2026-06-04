#!/usr/bin/env bash
#
# CI guard against persisting the WireGuard private key from any
# *.svelte component (SEC-C6 follow-up Pp5).
#
# Audit confirmed Cache-Control: no-store on the keypair endpoint and
# no current persistence path. This guard catches a future contributor
# who adds localStorage / sessionStorage / IndexedDB / document.cookie
# or any window.* assignment within WINDOW lines of a `keypair` or
# `privateKey` reference. The proximity check keeps the signal high:
# components legitimately using window.matchMedia far from key handling
# do not trip the guard.
#
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
WINDOW=10
PERSIST_RE='localStorage|sessionStorage|indexedDB|document\.cookie|window\.'
KEY_RE='keypair|privateKey'

fail=0
flagged=0

while IFS= read -r -d '' file; do
    rel=${file#"$ROOT/"}
    # Pre-filter: cheap exit when neither token is present.
    if ! grep -qE "$KEY_RE" "$file"; then
        continue
    fi
    # Pull key-line numbers once per file.
    mapfile -t keylines < <(grep -nE "$KEY_RE" "$file" | cut -d: -f1)

    for lineno in "${keylines[@]}"; do
        start=$(( lineno - WINDOW ))
        (( start < 1 )) && start=1
        end=$(( lineno + WINDOW ))

        leak=$(sed -n "${start},${end}p" "$file" \
            | grep -nE "$PERSIST_RE" \
            || true)
        if [[ -n "$leak" ]]; then
            flagged=$((flagged+1))
            echo "LEAK: $rel — persistence API within $WINDOW lines of key reference at line $lineno:" >&2
            # Re-anchor the line numbers from the sed window back into the file.
            while IFS=: read -r relno content; do
                echo "  $(( start + relno - 1 )): $content" >&2
            done <<< "$leak"
            fail=1
        fi
    done
done < <(find "$ROOT/manager/ui/src" -name "*.svelte" -print0)

if [[ "$fail" -eq 0 ]]; then
    echo "WG private-key persistence guard: OK"
else
    echo "WG private-key persistence guard: $flagged proximity hit(s)" >&2
fi
exit "$fail"
