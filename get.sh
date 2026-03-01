#!/bin/bash
#
# vpn-pack remote installer
# Downloads latest release from GitHub and runs the local installer.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/eds-ch/vpn-pack/main/get.sh | bash
#
set -euo pipefail

REPO="eds-ch/vpn-pack"
API_URL="https://api.github.com/repos/${REPO}/releases/latest"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

if [ -t 1 ]; then
    GREEN='\033[0;32m' RED='\033[0;31m' YELLOW='\033[1;33m' BOLD='\033[1m' NC='\033[0m'
else
    GREEN='' RED='' YELLOW='' BOLD='' NC=''
fi

info()  { echo -e "${GREEN}[+]${NC} $*"; }
error() { echo -e "${RED}[x]${NC} $*"; }
die()   { error "$*"; exit 1; }

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

[ "$(id -u)" -eq 0 ] || die "Must be run as root"
[ "$(uname -m)" = "aarch64" ] || die "Unsupported architecture: $(uname -m) (need aarch64)"
command -v curl >/dev/null || die "curl is required but not found"
check_network_version

INSTALLED_VERSION=""
if [ -f /persistent/vpn-pack/VERSION ]; then
    INSTALLED_VERSION=$(head -1 /persistent/vpn-pack/VERSION)
    info "Installed version: ${BOLD}v${INSTALLED_VERSION}${NC}"
fi

info "Fetching latest release info..."
RELEASE_JSON=$(curl -fsSL "$API_URL") || die "Failed to reach GitHub API"

VERSION=$(echo "$RELEASE_JSON" | grep -o '"tag_name" *: *"[^"]*"' | head -1 | grep -o '"v[^"]*"' | tr -d '"')
[ -n "$VERSION" ] || die "Could not determine latest version"
SEMVER=${VERSION#v}
info "Latest version: ${BOLD}${VERSION}${NC}"

if [ "$INSTALLED_VERSION" = "$SEMVER" ]; then
    NEEDS_REINSTALL=false
    if [ ! -f /etc/systemd/system/tailscaled.service ] || [ ! -f /etc/systemd/system/vpn-pack-manager.service ]; then
        NEEDS_REINSTALL=true
    elif ! systemctl is-active --quiet tailscaled 2>/dev/null || ! systemctl is-active --quiet vpn-pack-manager 2>/dev/null; then
        NEEDS_REINSTALL=true
    fi
    if [ "$NEEDS_REINSTALL" = false ]; then
        info "Already up to date."
        exit 0
    fi
    info "${YELLOW}Version v${SEMVER} data found but services are not installed${NC}"
    info "Reinstalling (this can happen after a factory reset)..."
fi

ARCHIVE="vpn-pack-${SEMVER}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

if [ -n "$INSTALLED_VERSION" ]; then
    info "Updating v${INSTALLED_VERSION} -> ${VERSION}..."
else
    info "Installing ${VERSION}..."
fi
info "Downloading ${ARCHIVE}..."
curl -fSL --progress-bar -o "${TMPDIR}/${ARCHIVE}" "${BASE_URL}/${ARCHIVE}" || die "Download failed"

info "Downloading checksums..."
curl -fsSL -o "${TMPDIR}/checksums.txt" "${BASE_URL}/checksums.txt" || die "Checksums download failed"

info "Verifying SHA256 checksum..."
(cd "$TMPDIR" && sha256sum -c checksums.txt) || die "Checksum verification failed! Archive may be corrupted."

info "Extracting..."
tar xzf "${TMPDIR}/${ARCHIVE}" -C "$TMPDIR"

info "Running installer..."
echo ""
bash "${TMPDIR}/vpn-pack/install.sh"
