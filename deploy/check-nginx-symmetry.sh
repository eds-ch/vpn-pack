#!/usr/bin/env bash
# Verifies that the two location blocks in deploy/nginx-vpnpack.conf
# include the same set of files. Drift between blocks was the root cause
# of SEC-A1d: /vpn-pack/ lacked the proxy include applied to /vpn-pack/api/,
# which let arbitrary forwarding behavior diverge between matched paths.
set -euo pipefail

file="${NGINX_CONF:-deploy/nginx-vpnpack.conf}"
if [[ ! -f "$file" ]]; then
    echo "missing $file" >&2
    exit 2
fi

extract() {
    awk -v re="$1" '
        $0 ~ re {inside=1; next}
        inside && /^[[:space:]]*\}/ {inside=0}
        inside && /^[[:space:]]*include[[:space:]]/ {print}
    ' "$file" | sed -E 's/^[[:space:]]+//;s/[[:space:]]+$//' | sort -u
}

a=$(extract '^[[:space:]]*location[[:space:]]+/vpn-pack/[[:space:]]*[{]')
b=$(extract '^[[:space:]]*location[[:space:]]+/vpn-pack/api/[[:space:]]*[{]')

if [[ "$a" != "$b" ]]; then
    echo "nginx location include sets diverged between /vpn-pack/ and /vpn-pack/api/:" >&2
    diff <(echo "$a") <(echo "$b") >&2 || true
    exit 1
fi
echo "nginx location include sets symmetric ($(echo "$a" | wc -l | tr -d ' ') entries)"
