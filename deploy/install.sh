#!/bin/bash
#
# vpn-pack — installer for Ubiquiti Cloud Gateway devices
# Installs Tailscale + Manager to /persistent/vpn-pack/
#
set -euo pipefail

INSTALL_DIR="/persistent/vpn-pack"
BIN_DIR="${INSTALL_DIR}/bin"
STATE_DIR="${INSTALL_DIR}/state"
CONFIG_DIR="${INSTALL_DIR}/config"
SYSTEMD_UNIT="/etc/systemd/system/tailscaled.service"
MANAGER_UNIT="/etc/systemd/system/vpn-pack-manager.service"
NGINX_SRC="${CONFIG_DIR}/nginx-vpnpack.conf"
NGINX_DEST="/data/unifi-core/config/http/shared-runnable-vpnpack.conf"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Colors (if terminal supports them)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    BOLD='\033[1m'
    NC='\033[0m'
else
    RED='' GREEN='' YELLOW='' BOLD='' NC=''
fi

info()  { echo -e "${GREEN}[+]${NC} $*"; }
warn()  { echo -e "${YELLOW}[!]${NC} $*"; }
error() { echo -e "${RED}[x]${NC} $*"; }
die()   { error "$*"; exit 1; }

# ── Stage 1: Environment checks ───────────────────────────────────

info "vpn-pack installer"
echo ""

# Root check
[ "$(id -u)" -eq 0 ] || die "Must be run as root"

# Architecture check
ARCH="$(uname -m)"
[ "$ARCH" = "aarch64" ] || die "Unsupported architecture: $ARCH (need aarch64)"

# UniFi device check
[ -x /usr/bin/ubnt-device-info ] || die "Not a UniFi device (/usr/bin/ubnt-device-info not found)"

DEVICE_MODEL="$(ubnt-device-info model_short 2>/dev/null || echo 'unknown')"
DEVICE_FULL="$(ubnt-device-info model 2>/dev/null || echo 'unknown')"
FIRMWARE="$(ubnt-device-info firmware 2>/dev/null || echo 'unknown')"
info "Device: ${BOLD}${DEVICE_FULL}${NC} (${DEVICE_MODEL})"
info "Firmware: ${FIRMWARE}"

# UniFi controller check
if ! systemctl is-active --quiet unifi-core 2>/dev/null; then
    die "unifi-core is not running. A working UniFi OS controller is required."
fi

# Version info
if [ -f "${SCRIPT_DIR}/VERSION" ]; then
    VERSION="$(head -1 "${SCRIPT_DIR}/VERSION")"
    info "Package version: ${VERSION}"
fi

echo ""

# ── Stage 2: Resource checks ──────────────────────────────────────

# Check /persistent/ exists and has space
[ -d /persistent ] || die "/persistent/ directory not found"
AVAIL_KB=$(df -k /persistent | awk 'NR==2 {print $4}')
AVAIL_MB=$((AVAIL_KB / 1024))
[ "$AVAIL_MB" -ge 50 ] || die "/persistent/ has only ${AVAIL_MB}MB free (need 50MB)"
info "/persistent/ has ${AVAIL_MB}MB free"

# Check /etc/systemd/system/ is writable
touch /etc/systemd/system/.write-test 2>/dev/null && rm -f /etc/systemd/system/.write-test \
    || die "/etc/systemd/system/ is not writable"

# ── Stage 3: Conflict checks ──────────────────────────────────────

UPGRADE=false

# Check for running tailscaled
if systemctl is-active --quiet tailscaled 2>/dev/null; then
    warn "tailscaled is currently running"
    UPGRADE=true
fi

# Check for existing installation
if [ -f "${BIN_DIR}/tailscaled" ]; then
    warn "Existing installation found at ${INSTALL_DIR}"
    if [ -f "${STATE_DIR}/tailscaled.state" ]; then
        info "Auth state will be preserved (upgrade mode)"
    fi
    UPGRADE=true
fi

# Check for stock tailscaled in system paths
if [ -x /usr/sbin/tailscaled ] && [ ! -L /usr/sbin/tailscaled ]; then
    warn "Stock tailscaled found at /usr/sbin/tailscaled"
    warn "It may conflict with this installation"
fi

echo ""

# ── Stage 4: Installation ─────────────────────────────────────────

if [ "$UPGRADE" = true ]; then
    info "Stopping existing services..."
    systemctl stop vpn-pack-manager 2>/dev/null || true
    systemctl stop tailscaled 2>/dev/null || true
fi

info "Creating directory structure..."
mkdir -p "${BIN_DIR}"
mkdir -p "${STATE_DIR}"
mkdir -p "${CONFIG_DIR}"
chmod 700 "${STATE_DIR}"

info "Installing binaries to ${BIN_DIR}..."
cp -f "${SCRIPT_DIR}/bin/tailscale" "${BIN_DIR}/tailscale"
cp -f "${SCRIPT_DIR}/bin/tailscaled" "${BIN_DIR}/tailscaled"
cp -f "${SCRIPT_DIR}/bin/vpn-pack-manager" "${BIN_DIR}/vpn-pack-manager"
chmod 755 "${BIN_DIR}/tailscale" "${BIN_DIR}/tailscaled" "${BIN_DIR}/vpn-pack-manager"

# Install defaults only if not present (preserve user customization on upgrade)
if [ ! -f "${INSTALL_DIR}/tailscaled.defaults" ]; then
    info "Installing default configuration..."
    cp "${SCRIPT_DIR}/systemd/tailscaled.defaults" "${INSTALL_DIR}/tailscaled.defaults"
else
    info "Keeping existing tailscaled.defaults (upgrade)"
fi

info "Installing nginx config for /vpn-pack/ path..."
cp -f "${SCRIPT_DIR}/nginx-vpnpack.conf" "${NGINX_SRC}"
mkdir -p "$(dirname "${NGINX_DEST}")"
cp -f "${NGINX_SRC}" "${NGINX_DEST}"
nginx -s reload 2>/dev/null || warn "nginx reload failed (will be picked up on next restart)"

info "Installing systemd services..."
cp -f "${SCRIPT_DIR}/systemd/tailscaled.service" "${SYSTEMD_UNIT}"
cp -f "${SCRIPT_DIR}/systemd/vpn-pack-manager.service" "${MANAGER_UNIT}"

systemctl daemon-reload
systemctl enable tailscaled
systemctl enable vpn-pack-manager

info "Starting tailscaled..."
systemctl start tailscaled

info "Starting vpn-pack-manager..."
systemctl start vpn-pack-manager

# ── Stage 5: Verification ─────────────────────────────────────────

echo ""

# Wait for tailscaled to be ready (up to 10 seconds)
for i in $(seq 1 10); do
    if systemctl is-active --quiet tailscaled 2>/dev/null; then
        break
    fi
    sleep 1
done

if systemctl is-active --quiet tailscaled 2>/dev/null; then
    info "tailscaled is ${GREEN}running${NC}"

    # Brief delay for socket to be ready
    sleep 2

    # Show status or auth URL
    STATUS=$("${BIN_DIR}/tailscale" status 2>&1) || true
    if echo "$STATUS" | grep -q "Log in at"; then
        echo ""
        warn "Tailscale needs authentication. Visit the URL below:"
        echo "$STATUS" | grep "https://"
    elif echo "$STATUS" | grep -q "NeedsLogin"; then
        echo ""
        warn "Run: ${BIN_DIR}/tailscale up"
    else
        echo ""
        echo "$STATUS" | head -5
    fi
else
    error "tailscaled failed to start. Check: journalctl -u tailscaled"
    exit 1
fi

if systemctl is-active --quiet vpn-pack-manager 2>/dev/null; then
    info "vpn-pack-manager is ${GREEN}running${NC} (behind nginx at /vpn-pack/)"
else
    warn "vpn-pack-manager failed to start. Check: journalctl -u vpn-pack-manager"
fi

# Install VERSION and uninstall script
if [ -f "${SCRIPT_DIR}/VERSION" ]; then
    cp -f "${SCRIPT_DIR}/VERSION" "${INSTALL_DIR}/VERSION"
fi
if [ -f "${SCRIPT_DIR}/uninstall.sh" ]; then
    cp -f "${SCRIPT_DIR}/uninstall.sh" "${INSTALL_DIR}/uninstall.sh"
    chmod +x "${INSTALL_DIR}/uninstall.sh"
fi

echo ""
info "Installation complete!"
info "Binaries: ${BIN_DIR}/"
info "State:    ${STATE_DIR}/"
info "Config:   ${CONFIG_DIR}/"
info "Nginx:    ${NGINX_DEST}"
info "Services: ${SYSTEMD_UNIT}"
info "          ${MANAGER_UNIT}"
echo ""
info "Useful commands:"
echo "  ${BIN_DIR}/tailscale status      — show connection status"
echo "  ${BIN_DIR}/tailscale up          — authenticate / connect"
echo "  systemctl restart tailscaled     — restart daemon"
echo "  journalctl -u tailscaled -f      — follow logs"
echo "  https://<device-ip>/vpn-pack/    — Web UI (behind UniFi auth)"
echo "  journalctl -u vpn-pack-manager -f — manager logs"
