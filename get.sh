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

[ "$(id -u)" -eq 0 ] || die "Must be run as root"
[ "$(uname -m)" = "aarch64" ] || die "Unsupported architecture: $(uname -m) (need aarch64)"
command -v curl >/dev/null || die "curl is required but not found"

info "Fetching latest release info..."
RELEASE_JSON=$(curl -fsSL "$API_URL") || die "Failed to reach GitHub API"

VERSION=$(echo "$RELEASE_JSON" | grep -o '"tag_name" *: *"[^"]*"' | head -1 | grep -o '"v[^"]*"' | tr -d '"')
[ -n "$VERSION" ] || die "Could not determine latest version"
SEMVER=${VERSION#v}
info "Latest version: ${BOLD}${VERSION}${NC}"

ARCHIVE="vpn-pack-${SEMVER}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"

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
