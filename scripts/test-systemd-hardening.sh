#!/usr/bin/env bash
#
# Regression guard for SEC-B2: assert vpn-pack-manager.service carries
# the documented hardening directives. Catches accidental reverts during
# unrelated edits to the unit file.
#
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
UNIT="$ROOT/deploy/vpn-pack-manager.service"

FAIL=0
PASS=0
red()   { printf '\033[0;31mFAIL\033[0m %s\n' "$*" >&2; FAIL=$((FAIL+1)); }
green() { printf '\033[0;32mPASS\033[0m %s\n' "$*"; PASS=$((PASS+1)); }

assert_directive() {
    local key=$1 want_value=$2
    if ! grep -qE "^${key}=${want_value}\$" "$UNIT"; then
        red "${key}=${want_value} missing or wrong"
    else
        green "${key}=${want_value}"
    fi
}

assert_present() {
    local key=$1
    if ! grep -qE "^${key}=" "$UNIT"; then
        red "${key}= directive missing"
    else
        green "${key}= present"
    fi
}

assert_directive "CapabilityBoundingSet" "CAP_NET_ADMIN CAP_NET_RAW"
assert_directive "AmbientCapabilities"   "CAP_NET_ADMIN CAP_NET_RAW"
assert_directive "NoNewPrivileges"       "yes"
assert_directive "ProtectSystem"         "strict"
assert_directive "ProtectHome"           "yes"
assert_directive "PrivateTmp"            "yes"
assert_directive "PrivateDevices"        "no"
assert_directive "ProtectKernelTunables" "yes"
assert_directive "ProtectKernelModules"  "yes"
assert_directive "ProtectControlGroups"  "yes"
assert_directive "RestrictNamespaces"    "yes"
assert_directive "LockPersonality"       "yes"
assert_directive "MemoryDenyWriteExecute" "yes"
assert_directive "SystemCallArchitectures" "native"

# These need ReadWritePaths to include the manager state and the proc
# DPI knob the manager writes; allow any list ordering.
if ! grep -qE "^ReadWritePaths=.*/persistent/vpn-pack" "$UNIT"; then
    red "ReadWritePaths missing /persistent/vpn-pack"
else
    green "ReadWritePaths includes /persistent/vpn-pack"
fi
if ! grep -qE "^ReadWritePaths=.*/run/vpn-pack" "$UNIT"; then
    red "ReadWritePaths missing /run/vpn-pack"
else
    green "ReadWritePaths includes /run/vpn-pack"
fi
if ! grep -qE "^ReadWritePaths=.*/proc/nf_dpi" "$UNIT"; then
    red "ReadWritePaths missing /proc/nf_dpi (dpi.go writes there)"
else
    green "ReadWritePaths includes /proc/nf_dpi"
fi

# unifi-core actively rewrites our nginx snippet on restart (verified
# empirically 2026-06-04: restart inode changed, content reverted). The
# manager's NginxManager.EnsureConfig is the only thing keeping our
# snippet authoritative across unifi-core restarts. Under ProtectSystem=
# strict the manager needs write access — and crucially, it must be able
# to *recreate* the file from scratch after unifi-core deletes it.
#
# GAP-004 (2026-06-05): the original file-level bind (-/.../vpnpack.conf)
# allowed in-place rewrites but blocked create-from-scratch under
# ProtectSystem=strict (the parent dir was read-only). Widened to the
# parent dir, which is the narrowest grant that still lets self-heal
# recreate the file. Threat model: manager runs uid 0 and is the only
# thing on this device writing to that dir, so widening is negligible
# vs. the alternatives the manager already has at uid 0.
#
# Match the bare token /data/unifi-core/config/http as a whitespace-
# separated path in the value, regardless of position. The token-stream
# form ([^[:space:]]+[[:space:]]+)* tolerates any path ordering and
# correctly rejects the old file-level revert
# (-/data/unifi-core/config/http/shared-runnable-vpnpack.conf), which
# would appear as a different non-space token.
if ! grep -qE "^ReadWritePaths=([^[:space:]]+[[:space:]]+)*/data/unifi-core/config/http([[:space:]]|$)" "$UNIT"; then
    red "ReadWritePaths missing /data/unifi-core/config/http (parent dir needed for nginx self-heal create-from-scratch — GAP-004)"
else
    green "ReadWritePaths includes /data/unifi-core/config/http (parent-dir grant, GAP-004)"
fi

# Address-family restriction: AF_UNIX needed for UDAPI socket + manager
# listen socket; AF_INET/AF_INET6 for HTTP integration client + tailscaled
# dial; AF_NETLINK for iptables-rule plumbing.
for fam in AF_UNIX AF_INET AF_INET6 AF_NETLINK; do
    if ! grep -qE "^RestrictAddressFamilies=.*${fam}" "$UNIT"; then
        red "RestrictAddressFamilies missing ${fam}"
    else
        green "RestrictAddressFamilies includes ${fam}"
    fi
done

assert_present "SystemCallFilter"

echo
echo "Results: $PASS passed, $FAIL failed"
exit "$FAIL"
