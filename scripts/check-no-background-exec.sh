#!/usr/bin/env bash
# BUG-L1/L2/M7 guard: every subprocess invocation in service/ internal/ udapi/
# must carry a propagated context.Context. Bare `exec.Command(...)` or
# `exec.CommandContext(context.Background(), ...)` are treated as offenders.
#
# Opt-out: add `// nolint:no-ctx-exec` on the same line as the call or on the
# line immediately above. Use sparingly and with a justifying comment.
set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

fail=0
while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    file=${line%%:*}
    lineno=$(echo "$line" | awk -F: '{print $2}')
    # allow if the line itself or the previous line has the opt-out marker
    body=$(awk -v n="$lineno" 'NR==n-1||NR==n' "$file")
    if [[ "$body" == *"nolint:no-ctx-exec"* ]]; then
        continue
    fi
    echo "$line" >&2
    fail=1
done < <(grep -rn 'exec\.Command(\|exec\.CommandContext.*context\.Background' \
            manager/service manager/internal manager/udapi || true)

if [[ "$fail" != 0 ]]; then
    echo "FATAL: subprocess calls without propagated context (use exec.CommandContext or // nolint:no-ctx-exec)" >&2
    exit 1
fi
