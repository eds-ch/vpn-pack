#!/bin/bash
#
# vpn-pack — uninstaller
# Removes vpn-pack from the device. Use --purge to remove everything including state and config.
#
set -euo pipefail

INSTALL_DIR="/persistent/vpn-pack"
BIN_DIR="${INSTALL_DIR}/bin"
STATE_DIR="${INSTALL_DIR}/state"
CONFIG_DIR="${INSTALL_DIR}/config"
SYSTEMD_UNIT="/etc/systemd/system/tailscaled.service"
MANAGER_UNIT="/etc/systemd/system/vpn-pack-manager.service"
MANAGER_SOCKET_UNIT="/etc/systemd/system/vpn-pack-manager.socket"
NGINX_DEST="/data/unifi-core/config/http/shared-runnable-vpnpack.conf"

# Parse flags
PURGE=false
FORCE_CLEANUP=false
for arg in "$@"; do
    case "$arg" in
        --purge) PURGE=true ;;
        --force-cleanup) FORCE_CLEANUP=true ;;
        --help|-h)
            echo "Usage: uninstall.sh [--purge] [--force-cleanup]"
            echo ""
            echo "  --purge          Remove everything including auth state and config"
            echo "                   Without --purge, state and config are preserved"
            echo "  --force-cleanup  Run vpn-pack-manager --cleanup even when the binary's"
            echo "                   sha256 does not match the value recorded by install.sh"
            exit 0
            ;;
    esac
done

INTERACTIVE=false
if [ -t 0 ] && [ "$PURGE" = false ]; then
    INTERACTIVE=true
fi

# Colors
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    NC='\033[0m'
else
    RED='' GREEN='' YELLOW='' NC=''
fi

info()  { echo -e "${GREEN}[+]${NC} $*"; }
warn()  { echo -e "${YELLOW}[!]${NC} $*"; }
error() { echo -e "${RED}[x]${NC} $*"; }

# verify_binary_integrity asserts that $bin matches the sha256 recorded
# in $expected_file at install time. Returns 0 if matched, 1 if the
# expected file is missing, 2 on mismatch. SEC-C4 — without this check
# uninstall.sh would execute "${bin} --cleanup" as root against a
# potentially swapped binary.
verify_binary_integrity() {
    local bin=$1 expected_file=$2
    if [ ! -f "$expected_file" ]; then
        return 1
    fi
    if [ ! -x "$bin" ]; then
        return 2
    fi
    local expected actual
    expected=$(cat "$expected_file" 2>/dev/null | tr -d '[:space:]')
    actual=$(sha256sum "$bin" 2>/dev/null | awk '{print $1}')
    if [ -z "$expected" ] || [ -z "$actual" ]; then
        return 2
    fi
    if [ "$expected" != "$actual" ]; then
        return 2
    fi
    return 0
}

# Root check
[ "$(id -u)" -eq 0 ] || { error "Must be run as root"; exit 1; }

echo "vpn-pack uninstaller"
if [ "$PURGE" = true ]; then
    echo "(purge mode — all data will be removed)"
fi
echo ""

# ── Stop services ─────────────────────────────────────────────────

if systemctl is-active --quiet vpn-pack-manager 2>/dev/null; then
    info "Stopping vpn-pack-manager..."
    systemctl stop vpn-pack-manager
fi

if systemctl is-active --quiet vpn-pack-manager.socket 2>/dev/null; then
    info "Stopping vpn-pack-manager.socket..."
    systemctl stop vpn-pack-manager.socket
fi

if systemctl is-enabled --quiet vpn-pack-manager 2>/dev/null; then
    info "Disabling vpn-pack-manager..."
    systemctl disable vpn-pack-manager
fi

if systemctl is-enabled --quiet vpn-pack-manager.socket 2>/dev/null; then
    info "Disabling vpn-pack-manager.socket..."
    systemctl disable vpn-pack-manager.socket
fi

if systemctl is-active --quiet tailscaled 2>/dev/null; then
    info "Stopping tailscaled..."
    systemctl stop tailscaled
fi

if systemctl is-enabled --quiet tailscaled 2>/dev/null; then
    info "Disabling tailscaled..."
    systemctl disable tailscaled
fi

# ── Network cleanup ───────────────────────────────────────────────

# Manager cleanup: UDAPI firewall rules, WG S2S interfaces, ipset entries.
# SEC-C4: refuse to execute the binary if its sha256 does not match what
# install.sh recorded, unless --force-cleanup is given. A missing
# .expected-sha256 file (legacy install, pre-SEC-C4) is also treated as
# untrusted.
if [ -x "${BIN_DIR}/vpn-pack-manager" ]; then
    INTEGRITY_RC=0
    verify_binary_integrity "${BIN_DIR}/vpn-pack-manager" "${BIN_DIR}/.expected-sha256" || INTEGRITY_RC=$?
    if [ "$INTEGRITY_RC" -eq 0 ]; then
        info "Cleaning up firewall rules, interfaces, and Integration API resources..."
        "${BIN_DIR}/vpn-pack-manager" --cleanup 2>&1 || warn "Manager cleanup had errors (continuing)"
    elif [ "$FORCE_CLEANUP" = true ]; then
        case "$INTEGRITY_RC" in
            1) warn "manager binary integrity record missing — running --cleanup under --force-cleanup" ;;
            2) warn "manager binary sha256 mismatch — running --cleanup under --force-cleanup" ;;
        esac
        "${BIN_DIR}/vpn-pack-manager" --cleanup 2>&1 || warn "Manager cleanup had errors (continuing)"
    else
        case "$INTEGRITY_RC" in
            1) warn "manager binary integrity record missing at ${BIN_DIR}/.expected-sha256; skipping --cleanup. Re-run with --force-cleanup to override." ;;
            2) warn "manager binary sha256 does not match install record; skipping --cleanup. Re-run with --force-cleanup if you really intend to execute the current binary." ;;
        esac
    fi
fi

# Tailscaled network cleanup (ts-* iptables chains, ip rules, TUN)
if [ -x "${BIN_DIR}/tailscaled" ]; then
    info "Cleaning up Tailscale network rules..."
    "${BIN_DIR}/tailscaled" --cleanup 2>/dev/null || true
fi

# ── Remove nginx config ──────────────────────────────────────────

if [ -f "${NGINX_DEST}" ]; then
    info "Removing nginx config..."
    rm -f "${NGINX_DEST}"
    nginx -s reload 2>/dev/null || true
fi

# ── Remove systemd units ──────────────────────────────────────────

for unit in "${MANAGER_UNIT}" "${MANAGER_SOCKET_UNIT}" "${SYSTEMD_UNIT}"; do
    if [ -f "$unit" ]; then
        info "Removing $(basename "$unit")..."
        rm -f "$unit"
    fi
done

systemctl daemon-reload

# ── Remove symlinks ──────────────────────────────────────────────

for link in /usr/local/bin/tailscale /usr/local/bin/tailscaled; do
    if [ -L "$link" ]; then
        info "Removing symlink $(basename "$link")..."
        rm -f "$link"
    fi
done

# ── Remove binaries ───────────────────────────────────────────────

if [ -d "${BIN_DIR}" ]; then
    info "Removing binaries from ${BIN_DIR}/..."
    rm -rf "${BIN_DIR}"
fi

# ── Remove defaults and VERSION ───────────────────────────────────

rm -f "${INSTALL_DIR}/tailscaled.defaults"
rm -f "${INSTALL_DIR}/VERSION"
rm -f "${INSTALL_DIR}/uninstall.sh"

# ── Config handling ───────────────────────────────────────────────
# Config includes: api-key, manifest.json, nginx-vpnpack.conf, wg-s2s/

if [ -d "${CONFIG_DIR}" ]; then
    if [ "$PURGE" = true ]; then
        info "Removing all config (--purge)..."
        rm -rf "${CONFIG_DIR}"
    elif [ "$INTERACTIVE" = true ]; then
        echo ""
        warn "Config directory exists: ${CONFIG_DIR}/"
        warn "Contains: API key, firewall manifest, WG S2S tunnel configs"
        read -rp "Delete all config? [y/N] " answer
        case "$answer" in
            [yY]|[yY][eE][sS])
                info "Removing config..."
                rm -rf "${CONFIG_DIR}"
                ;;
            *)
                info "Config preserved at ${CONFIG_DIR}/"
                ;;
        esac
    else
        info "Config preserved at ${CONFIG_DIR}/ (non-interactive mode)"
    fi
fi

# ── State handling ────────────────────────────────────────────────

if [ -d "${STATE_DIR}" ] && [ -f "${STATE_DIR}/tailscaled.state" ]; then
    if [ "$PURGE" = true ]; then
        info "Removing auth state (--purge)..."
        rm -rf "${STATE_DIR}"
    elif [ "$INTERACTIVE" = true ]; then
        echo ""
        warn "Auth state exists at ${STATE_DIR}/tailscaled.state"
        warn "This contains your Tailscale authentication keys."
        read -rp "Delete auth state? This will require re-authentication. [y/N] " answer
        case "$answer" in
            [yY]|[yY][eE][sS])
                info "Removing auth state..."
                rm -rf "${STATE_DIR}"
                ;;
            *)
                info "Auth state preserved at ${STATE_DIR}/"
                info "If you reinstall, authentication will be automatic."
                ;;
        esac
    else
        info "Auth state preserved at ${STATE_DIR}/ (non-interactive mode)"
    fi
fi

# ── Cleanup empty dirs ────────────────────────────────────────────

rmdir "${INSTALL_DIR}" 2>/dev/null || true

# ── Summary ───────────────────────────────────────────────────────

echo ""
info "Uninstall complete."

PRESERVED=""
if [ -d "${STATE_DIR}" ]; then
    PRESERVED="${PRESERVED} auth-state"
fi
if [ -d "${CONFIG_DIR}" ]; then
    PRESERVED="${PRESERVED} config"
fi

if [ -n "$PRESERVED" ]; then
    info "Preserved:${PRESERVED} (at ${INSTALL_DIR}/)"
    info "To remove everything: uninstall.sh --purge"
fi
