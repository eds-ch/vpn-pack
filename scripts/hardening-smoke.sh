#!/usr/bin/env bash
#
# hardening-smoke — verify systemd hardening (SEC-B2) on a real UDM-SE
# without breaking the manager × tailscaled × unifi-core interplay.
#
# This is the device-side gate for Task 8.6 Step 2 of the security
# remediation plan. It is user-triggered (one command), reproducible,
# and probes the specific regressions hardening can introduce:
#
#   1. Services do not start under the new sandbox.
#   2. Manager logs "Operation not permitted" or "Permission denied"
#      because a syscall / address-family / capability is filtered.
#   3. nginx self-healing breaks under file-level ReadWritePaths.
#
# Probe #3 is the most important and load-bearing: empirical testing on
# 2026-06-04 showed unifi-core actively reverts our shared-runnable-
# vpnpack.conf snippet on its own restart (inode change, content
# reverted). The manager's NginxManager.EnsureConfig is what keeps the
# snippet authoritative. After hardening, ReadWritePaths must include
# the snippet path for that healing to keep working. This probe
# restarts unifi-core, waits a poll cycle plus slack, and asserts the
# snippet content matches the install-time copy in /persistent.
#
# Usage:
#   scripts/hardening-smoke.sh <host>
#   make hardening-smoke HOST=<ip>
#
set -euo pipefail

if [[ $# -lt 1 ]]; then
    echo "usage: $0 <host>" >&2
    exit 2
fi

HOST=$1
SSH="ssh -o ConnectTimeout=10 root@${HOST}"

FAIL=0
PASS=0
if [[ -t 1 ]]; then
    R='\033[0;31m'; G='\033[0;32m'; N='\033[0m'
else
    R=''; G=''; N=''
fi
red()   { printf "${R}FAIL${N} %s\n" "$*" >&2; FAIL=$((FAIL+1)); }
green() { printf "${G}PASS${N} %s\n" "$*"; PASS=$((PASS+1)); }
info()  { printf "==> %s\n" "$*"; }

run_ssh() {
    # Wrap ssh so we capture stdout + stderr but never let an SSH-level
    # error bring the whole script down via set -e — probes report PASS/
    # FAIL explicitly and we want to attempt all of them.
    $SSH "$@" 2>&1
}

info "host: ${HOST}"

# ── Probe 1: services are active ─────────────────────────────────────
info "probe 1: tailscaled + vpn-pack-manager are active"
for svc in tailscaled vpn-pack-manager; do
    if run_ssh "systemctl is-active --quiet ${svc}"; then
        green "${svc} is active"
    else
        red "${svc} is not active"
    fi
done

# ── Probe 2: no permission-denied entries in the manager journal ────
# Hardening regressions (syscall filter / address family / cap) surface
# as "Operation not permitted" or "Permission denied" in the manager
# journal soon after start. Limit the window to the current boot so we
# only see post-deploy behavior.
info "probe 2: vpn-pack-manager journal clean of permission denials"
deny_log=$(run_ssh "journalctl -u vpn-pack-manager --since '5 minutes ago' --no-pager 2>/dev/null | grep -iE 'operation not permitted|permission denied' | head -20" || true)
if [[ -n "$deny_log" ]]; then
    red "permission-denied entries in manager journal:"
    printf '  %s\n' "$deny_log" >&2
else
    green "no permission denials in last 5 minutes of manager journal"
fi

# ── Probe 7: socket activation produced root:nginx 0660 sock ────────
# GAP-002 regression guard. Run BEFORE the unifi-core-restart probes
# because manager bounces in response to unifi-core restart, and the
# socket file is briefly absent each bounce. Measuring here gives a
# clean steady-state read.
info "probe 7: /run/vpn-pack/manager.sock owner=root:nginx mode=660 and nginx-readable"
sock_stat=$(run_ssh "stat -c '%U:%G %a' /run/vpn-pack/manager.sock 2>/dev/null" || echo "")
if [[ "$sock_stat" != "root:nginx 660" ]]; then
    red "socket owner/mode wrong: got '$sock_stat', want 'root:nginx 660'"
else
    nginx_connect=$(run_ssh "sudo -u nginx curl -sf --max-time 3 --unix-socket /run/vpn-pack/manager.sock http://x/api/status >/dev/null 2>&1 && echo ok || echo FAIL" || echo FAIL)
    if [[ "$nginx_connect" == "ok" ]]; then
        green "socket owner+mode correct and nginx user can connect"
    else
        red "socket owner/mode look correct but nginx user cannot connect — investigate"
    fi
fi

# ── Probe 8: post-reboot simulation (GAP-002b regression guard) ─────
# /run is tmpfs; after reboot /run/vpn-pack/ is empty. Run BEFORE the
# unifi-core restart probes for the same reason as probe 7 — these are
# steady-state cold-boot guarantees.
info "probe 8: post-reboot simulation — socket unit recreates /run/vpn-pack on cold start"
probe8_state=$(run_ssh '
    systemctl stop vpn-pack-manager.service 2>/dev/null || true
    systemctl stop vpn-pack-manager.socket   2>/dev/null || true
    rm -rf /run/vpn-pack
    systemctl start vpn-pack-manager.socket  2>&1 >/dev/null || true
    dir_stat=$(stat -c "%U:%G %a" /run/vpn-pack 2>/dev/null || echo MISSING)
    sock_active=$(systemctl is-active vpn-pack-manager.socket 2>/dev/null || echo inactive)
    systemctl start vpn-pack-manager.service 2>&1 >/dev/null || true
    svc_active=$(systemctl is-active vpn-pack-manager.service 2>/dev/null || echo inactive)
    echo "dir=$dir_stat sock=$sock_active svc=$svc_active"
' 2>&1 | tail -1 || echo "")
case "$probe8_state" in
    "dir=root:root 755 sock=active svc=active")
        green "cold start re-created /run/vpn-pack root:root 0755 and both units came up active"
        ;;
    *)
        red "post-reboot cold start failed: $probe8_state"
        ;;
esac

# ── Probe 3: nginx snippet survives unifi-core restart ──────────────
# This is the SEC-B2 × self-healing regression test. See header.
info "probe 3: nginx snippet self-heals after unifi-core restart"

source_sha=$(run_ssh "sha256sum /persistent/vpn-pack/config/nginx-vpnpack.conf 2>/dev/null | awk '{print \$1}'")
if [[ -z "$source_sha" ]]; then
    red "cannot read /persistent/vpn-pack/config/nginx-vpnpack.conf — skipping probe 3"
else
    info "  baseline snippet sha (from /persistent): $source_sha"
    info "  restarting unifi-core (will revert the deployed snippet)..."
    run_ssh "systemctl restart unifi-core" >/dev/null

    info "  waiting for unifi-core to come back up..."
    for i in $(seq 1 30); do
        if run_ssh "systemctl is-active --quiet unifi-core"; then
            info "  unifi-core active after ${i}s"
            break
        fi
        sleep 1
    done

    # PollInterval=5s + EnsureConfig + nginx reload + jitter. 20s is
    # safe margin and short enough to keep this script under a minute.
    info "  waiting 20s for manager nginx watcher to detect & heal..."
    sleep 20

    deployed_sha=$(run_ssh "sha256sum /data/unifi-core/config/http/shared-runnable-vpnpack.conf 2>/dev/null | awk '{print \$1}'")
    info "  deployed snippet sha (after revert+heal): $deployed_sha"

    if [[ -z "$deployed_sha" ]]; then
        red "snippet file disappeared from /data and was not restored"
    elif [[ "$source_sha" != "$deployed_sha" ]]; then
        red "snippet content drift after unifi-core restart: self-healing did not restore. source=$source_sha deployed=$deployed_sha"
    else
        green "snippet content matches /persistent source after unifi-core restart (self-healing OK under hardening)"
    fi
fi

# ── Probe 4: manager survives its own restart under hardening ───────
# A unit that crashes on start due to filter denials would show this
# as a Restart=on-failure loop. Trigger an explicit restart and verify
# the service comes back active.
info "probe 4: vpn-pack-manager comes back up after explicit restart"
run_ssh "systemctl restart vpn-pack-manager" >/dev/null
for i in $(seq 1 30); do
    if run_ssh "systemctl is-active --quiet vpn-pack-manager"; then
        green "vpn-pack-manager active after ${i}s following restart"
        break
    fi
    sleep 1
done
if ! run_ssh "systemctl is-active --quiet vpn-pack-manager"; then
    red "vpn-pack-manager did not come back up within 30s after restart"
fi

# ── Probe 9: socket file survives a service restart (GAP-005 guard) ─
# systemd's RuntimeDirectory= directive wipes its target on unit start,
# even if pre-existing. When both .socket and .service declared
# RuntimeDirectory=vpn-pack, `systemctl restart vpn-pack-manager.service`
# re-created /run/vpn-pack with a fresh inode and the socket file the
# .socket unit had bound was gone. systemd still reported both units
# active (manager kept the inherited listening fd) but every nginx
# connect(2) from then on hit ENOENT → 502 Bad Gateway in the browser.
#
# Probe 4 above only checks is-active, which would still PASS in that
# state. This probe captures the .sock inode before and after probe 4's
# restart and asserts both that the file still exists and that the
# inode is unchanged. Same-inode is the precise check: an inode change
# means systemd rm-rf'd and recreated the parent dir, which is exactly
# the regression we're guarding against.
info "probe 9: /run/vpn-pack/manager.sock survives a service restart (GAP-005)"
sock_before=$(run_ssh "stat -c '%i' /run/vpn-pack/manager.sock 2>/dev/null" || echo "")
run_ssh "systemctl restart vpn-pack-manager.service" >/dev/null
sleep 2
sock_after=$(run_ssh "stat -c '%i' /run/vpn-pack/manager.sock 2>/dev/null" || echo "")
if [[ -z "$sock_after" ]]; then
    red "socket file gone after service restart (GAP-005 regression: RuntimeDirectory= conflict between .socket and .service)"
elif [[ "$sock_before" != "$sock_after" ]]; then
    red "socket inode changed across service restart: before=$sock_before after=$sock_after (GAP-005 regression)"
else
    # Confirm the path is actually reachable, not just present-but-stale.
    nginx_reach=$(run_ssh "sudo -u nginx curl -sf --max-time 3 --unix-socket /run/vpn-pack/manager.sock http://x/api/status >/dev/null 2>&1 && echo ok || echo FAIL" || echo FAIL)
    if [[ "$nginx_reach" == "ok" ]]; then
        green "socket file survived service restart (inode preserved: $sock_after) and remains nginx-reachable"
    else
        red "socket file present (inode $sock_after) but nginx user cannot connect — investigate"
    fi
fi

# ── Probe 6: drift-induced self-heal proves the write path works ────
# Probe 3 only checks that content matches AFTER unifi-core's revert;
# when /persistent and /data already hold identical content (the common
# case) EnsureConfig short-circuits and never exercises the write path,
# so a broken ReadWritePaths bind would still PASS probe 3.
#
# Probe 6 introduces real drift: prepend a benign comment to the
# /persistent source, restart unifi-core (reverts /data to its cached
# copy, now stale relative to /persistent), wait for the manager to
# heal, then assert /data matches the drifted /persistent content. A
# hardening regression that blocks the write surfaces here.
#
# Cleanup is mandatory — restore the original /persistent file and
# trigger one more revert+heal so /data ends at known-good state.
info "probe 6: drift-induced self-heal exercises the write path"
backup=$(run_ssh "mktemp /tmp/vpn-pack-smoke-backup.XXXXXX")
if [[ -z "$backup" ]]; then
    red "probe 6: cannot create remote backup file — skipping"
else
    probe6_cleanup() {
        info "  probe 6 cleanup: restoring /persistent and re-healing /data"
        run_ssh "cp -f '$backup' /persistent/vpn-pack/config/nginx-vpnpack.conf 2>/dev/null; rm -f '$backup' 2>/dev/null; systemctl restart unifi-core 2>/dev/null" >/dev/null || true
        sleep 15
    }
    # Trap fires only on abnormal exit while probe 6 is mid-flight; we
    # call probe6_cleanup explicitly on the success path below so the
    # final journal sweep runs against the post-cleanup state.
    trap probe6_cleanup EXIT

    run_ssh "cp /persistent/vpn-pack/config/nginx-vpnpack.conf '$backup'" >/dev/null
    drift_marker="# hardening-smoke probe 6 drift $(date +%s)"
    run_ssh "{ echo '$drift_marker'; cat '$backup'; } > /persistent/vpn-pack/config/nginx-vpnpack.conf" >/dev/null
    drifted_sha=$(run_ssh "sha256sum /persistent/vpn-pack/config/nginx-vpnpack.conf 2>/dev/null | awk '{print \$1}'")
    info "  drifted source sha: $drifted_sha"

    info "  restarting unifi-core to revert /data to its cached (non-drifted) copy..."
    run_ssh "systemctl restart unifi-core" >/dev/null
    for i in $(seq 1 30); do
        if run_ssh "systemctl is-active --quiet unifi-core"; then break; fi
        sleep 1
    done

    info "  waiting 20s for manager to detect drift and heal /data..."
    sleep 20

    healed_sha=$(run_ssh "sha256sum /data/unifi-core/config/http/shared-runnable-vpnpack.conf 2>/dev/null | awk '{print \$1}'")
    info "  /data sha after heal: $healed_sha"

    if [[ "$drifted_sha" = "$healed_sha" ]]; then
        green "manager healed /data to match drifted /persistent — write path works under hardening"
    else
        red "manager did NOT heal /data after drift: source=$drifted_sha deployed=$healed_sha (ReadWritePaths likely missing the snippet path)"
    fi

    probe6_cleanup
    trap - EXIT
fi

# ── Probe 5: no new permission denials introduced by the restarts ──
# Re-check the manager journal after both restarts above. Catches the
# case where a denial happens only during a specific code path that
# only runs after start (e.g., during firewall watcher boot).
info "probe 5: no permission denials after the restart probes"
deny_log=$(run_ssh "journalctl -u vpn-pack-manager --since '2 minutes ago' --no-pager 2>/dev/null | grep -iE 'operation not permitted|permission denied' | head -20" || true)
if [[ -n "$deny_log" ]]; then
    red "permission-denied entries after restart probes:"
    printf '  %s\n' "$deny_log" >&2
else
    green "no permission denials after restart probes"
fi

echo
echo "Results: $PASS passed, $FAIL failed"
exit "$FAIL"
