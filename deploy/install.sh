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
MANAGER_SOCKET_UNIT="/etc/systemd/system/vpn-pack-manager.socket"
NGINX_SRC="${CONFIG_DIR}/nginx-vpnpack.conf"
NGINX_DEST="/data/unifi-core/config/http/shared-runnable-vpnpack.conf"
NGINX_TOKEN_FILE="${CONFIG_DIR}/nginx-token"
NGINX_TOKEN_PLACEHOLDER="__VPNPACK_NGINX_TOKEN__"
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

# safe_install writes $src to $dst at $mode atomically. If $dst exists
# and is a symlink, the install aborts — this prevents an attacker who
# can pre-create a symlink at a known config path from steering writes
# to an arbitrary file. Staging happens in the same directory as $dst
# so the final mv is an atomic rename on the same filesystem.
safe_install() {
    local src=$1 dst=$2 mode=$3
    if [[ -L "$dst" ]]; then
        echo "FATAL: refusing to write through symlink at $dst" >&2
        exit 1
    fi
    local dir tmp
    dir=$(dirname "$dst")
    tmp=$(mktemp "${dir}/.install.XXXXXX")
    chmod "$mode" "$tmp"
    cp -f --no-dereference "$src" "$tmp"
    mv -f "$tmp" "$dst"
}

# ensure_nginx_token provisions the per-install shared secret used for the
# X-VpnPack-Token app-layer factor (M1). Idempotent: an existing non-empty
# token file is preserved across upgrades so the running nginx config and
# the manager stay in agreement. The token is hex (sed-safe for later
# substitution) with 256 bits of entropy. Perms are 0640 root:nginx so the
# nginx worker can read it to inject the header while other local uids
# cannot. Writes atomically via a temp file in the same dir.
ensure_nginx_token() {
    if [ -f "${NGINX_TOKEN_FILE}" ] && [ -s "${NGINX_TOKEN_FILE}" ]; then
        info "Reusing existing nginx token (upgrade)"
        return
    fi
    info "Generating per-install nginx token..."
    local tok tmp
    if command -v openssl >/dev/null 2>&1; then
        tok="$(openssl rand -hex 32)"
    else
        tok="$(head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    fi
    [ -n "$tok" ] || die "failed to generate nginx token"
    tmp="$(mktemp "${CONFIG_DIR}/.nginx-token.XXXXXX")"
    printf '%s\n' "$tok" > "$tmp"
    chmod 0640 "$tmp"
    chown root:nginx "$tmp" 2>/dev/null || warn "could not chown token to root:nginx (nginx group missing?) — token factor may not reach nginx"
    mv -f "$tmp" "${NGINX_TOKEN_FILE}"
}

check_network_version() {
    local raw major minor rest pkg
    raw=$(dpkg-query -W -f='${Version}' unifi 2>/dev/null) || true
    pkg="unifi"
    if [ -z "$raw" ]; then
        raw=$(dpkg-query -W -f='${Version}' unifi-native 2>/dev/null) || true
        pkg="unifi-native"
    fi
    if [ -z "$raw" ]; then
        die "UniFi Network Application not found. A working UniFi Network 10.1+ installation is required."
    fi
    major="${raw%%.*}"
    rest="${raw#*.}"
    minor="${rest%%.*}"
    if ! [ "$major" -eq "$major" ] 2>/dev/null || ! [ "$minor" -eq "$minor" ] 2>/dev/null; then
        die "Cannot parse UniFi Network version: ${raw}"
    fi
    if [ "$major" -gt 10 ] || { [ "$major" -eq 10 ] && [ "$minor" -ge 1 ]; }; then
        info "UniFi Network: ${BOLD}${major}.${minor}${NC} (${pkg} ${raw})"
        return 0
    fi
    die "UniFi Network 10.1 or later is required (found: ${major}.${minor}). Please update via Settings > System > Updates in the UniFi console."
}

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

check_network_version

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
    # Stop socket BEFORE service. If service stops first, the socket
    # keeps listening; the next inbound connection (nginx worker,
    # healthcheck, browser tab on /vpn-pack/) socket-activates the
    # service again — with the STILL-OLD on-disk binary, because
    # binary replacement happens later in this script. The upgrade
    # then fails with "Socket service vpn-pack-manager.service
    # already active, refusing." when we try to start the new socket.
    systemctl stop vpn-pack-manager.socket 2>/dev/null || true
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

# Record manager binary sha256 so uninstall.sh can refuse to run --cleanup
# against a tampered binary (SEC-C4).
sha256sum "${BIN_DIR}/vpn-pack-manager" | awk '{print $1}' > "${BIN_DIR}/.expected-sha256"
chmod 0644 "${BIN_DIR}/.expected-sha256"

# -n: treat an existing symlink-to-directory as a normal file to replace,
# instead of dereferencing it and creating the link *inside* that directory.
ln -sfn "${BIN_DIR}/tailscale" /usr/local/bin/tailscale
ln -sfn "${BIN_DIR}/tailscaled" /usr/local/bin/tailscaled

# Install defaults only if not present (preserve user customization on upgrade)
if [ ! -f "${INSTALL_DIR}/tailscaled.defaults" ]; then
    info "Installing default configuration..."
    cp "${SCRIPT_DIR}/systemd/tailscaled.defaults" "${INSTALL_DIR}/tailscaled.defaults"
else
    info "Keeping existing tailscaled.defaults (upgrade)"
fi

# Provision the token BEFORE the nginx config is rendered/reloaded and
# BEFORE the manager starts, so nginx begins sending X-VpnPack-Token in
# the same run that the manager begins enforcing it — no self-DoS window.
ensure_nginx_token

info "Installing nginx config for /vpn-pack/ path..."
# Render the config template, substituting the real token for the
# placeholder. The token is hex, so the sed '|' delimiter is safe. The
# rendered (secret-bearing) copy is written to the persistent NGINX_SRC
# and then to NGINX_DEST; the manager's self-heal copies NGINX_SRC -> DEST
# verbatim, preserving the token.
NGINX_TOKEN_VALUE="$(cat "${NGINX_TOKEN_FILE}")"
NGINX_RENDERED="$(mktemp "${CONFIG_DIR}/.nginx-render.XXXXXX")"
sed "s|${NGINX_TOKEN_PLACEHOLDER}|${NGINX_TOKEN_VALUE}|g" \
    "${SCRIPT_DIR}/nginx-vpnpack.conf" > "${NGINX_RENDERED}"
if grep -q "${NGINX_TOKEN_PLACEHOLDER}" "${NGINX_RENDERED}"; then
    rm -f "${NGINX_RENDERED}"
    die "nginx token placeholder was not substituted"
fi
# 0640, not 0644: the rendered config embeds the per-install token secret, so
# it must not be world-readable (unlike UniFi's secret-free 0644 configs). The
# nginx master reads it as root and the manager self-heal reads it as root, so
# no non-root reader needs it — matches the 0640 on the token file itself.
safe_install "${NGINX_RENDERED}" "${NGINX_SRC}" 0640
rm -f "${NGINX_RENDERED}"
mkdir -p "$(dirname "${NGINX_DEST}")"
safe_install "${NGINX_SRC}" "${NGINX_DEST}" 0640
if ! nginx_test_output=$(nginx -t 2>&1); then
    error "nginx config test failed; refusing to reload"
    echo "$nginx_test_output" >&2
    exit 1
fi
nginx -s reload 2>/dev/null || warn "nginx reload failed (will be picked up on next restart)"

info "Installing systemd services..."
safe_install "${SCRIPT_DIR}/systemd/tailscaled.service" "${SYSTEMD_UNIT}" 0644
safe_install "${SCRIPT_DIR}/systemd/vpn-pack-manager.service" "${MANAGER_UNIT}" 0644
safe_install "${SCRIPT_DIR}/systemd/vpn-pack-manager.socket" "${MANAGER_SOCKET_UNIT}" 0644

systemctl daemon-reload
systemctl enable tailscaled
# Socket must be enabled before the service so the listen fd is ready
# when systemd activates the manager. .service Requires the .socket
# unit, so a misordered start would be rejected by systemd anyway.
systemctl enable vpn-pack-manager.socket
systemctl enable vpn-pack-manager

info "Starting tailscaled..."
systemctl start tailscaled

info "Starting vpn-pack-manager.socket..."
systemctl start vpn-pack-manager.socket

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

FAIL=false

if systemctl is-active --quiet tailscaled 2>/dev/null; then
    info "tailscaled is ${GREEN}running${NC}"
else
    error "tailscaled failed to start"
    error "Run: journalctl -u tailscaled -e"
    FAIL=true
fi

if systemctl is-active --quiet vpn-pack-manager 2>/dev/null; then
    info "vpn-pack-manager is ${GREEN}running${NC}"
else
    error "vpn-pack-manager failed to start"
    error "Run: journalctl -u vpn-pack-manager -e"
    FAIL=true
fi

[ "$FAIL" = true ] && exit 1

# Install VERSION and uninstall script
if [ -f "${SCRIPT_DIR}/VERSION" ]; then
    cp -f "${SCRIPT_DIR}/VERSION" "${INSTALL_DIR}/VERSION"
fi
if [ -f "${SCRIPT_DIR}/uninstall.sh" ]; then
    cp -f "${SCRIPT_DIR}/uninstall.sh" "${INSTALL_DIR}/uninstall.sh"
    chmod +x "${INSTALL_DIR}/uninstall.sh"
fi

# ── Stage 6: Next steps ──────────────────────────────────────────

LAN_IP=$(ip -4 addr show br0 2>/dev/null | grep -oP 'inet \K[^/]+' | head -1)
[ -z "$LAN_IP" ] && LAN_IP="<device-ip>"

echo ""
echo -e "  ${BOLD}vpn-pack v${VERSION:-unknown} installed${NC}"
echo ""
echo -e "  ${BOLD}Next steps:${NC}"
echo ""
echo -e "  1. Open UniFi console in your browser:"
echo -e "     ${BOLD}https://${LAN_IP}${NC}"
echo ""
echo -e "  2. Go to Settings > API and create an API key"
echo -e "     (needed for vpn-pack to manage firewall zones)"
echo ""
echo -e "  3. Open vpn-pack UI:"
echo -e "     ${BOLD}https://${LAN_IP}/vpn-pack/${NC}"
echo ""
echo -e "  4. Enter the API key and authenticate Tailscale"
echo ""
