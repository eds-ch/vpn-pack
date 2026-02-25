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
NGINX_DEST="/data/unifi-core/config/http/shared-runnable-vpnpack.conf"

# Parse flags
PURGE=false
for arg in "$@"; do
    case "$arg" in
        --purge) PURGE=true ;;
        --help|-h)
            echo "Usage: uninstall.sh [--purge]"
            echo ""
            echo "  --purge    Remove everything including auth state and config"
            echo "             Without --purge, state and config are preserved"
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

if systemctl is-enabled --quiet vpn-pack-manager 2>/dev/null; then
    info "Disabling vpn-pack-manager..."
    systemctl disable vpn-pack-manager
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

# Manager cleanup: UDAPI firewall rules, WG S2S interfaces, ipset entries
if [ -x "${BIN_DIR}/vpn-pack-manager" ]; then
    info "Cleaning up firewall rules, interfaces, and Integration API resources..."
    "${BIN_DIR}/vpn-pack-manager" --cleanup 2>&1 || warn "Manager cleanup had errors (continuing)"
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

for unit in "${MANAGER_UNIT}" "${SYSTEMD_UNIT}"; do
    if [ -f "$unit" ]; then
        info "Removing $(basename "$unit")..."
        rm -f "$unit"
    fi
done

systemctl daemon-reload

# ── Remove binaries ───────────────────────────────────────────────

if [ -d "${BIN_DIR}" ]; then
    info "Removing binaries from ${BIN_DIR}/..."
    rm -rf "${BIN_DIR}"
fi

# ── Remove defaults and VERSION ───────────────────────────────────

rm -f "${INSTALL_DIR}/tailscaled.defaults"
rm -f "${INSTALL_DIR}/VERSION"

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
