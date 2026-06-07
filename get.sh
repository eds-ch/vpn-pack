#!/bin/bash
#
# vpn-pack remote installer
# Downloads latest release from GitHub and runs the local installer.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/eds-ch/vpn-pack/main/get.sh | bash
#
# Pin a specific version (e.g. beta):
#   VERSION_PIN=1.4.0-beta.1 bash <(curl -fsSL https://raw.githubusercontent.com/eds-ch/vpn-pack/main/get.sh)
#
set -euo pipefail

REPO="eds-ch/vpn-pack"
# Releases are signed in GitHub Actions via keyless OIDC. The
# certificate identity is the workflow URL @ tag ref; the OIDC issuer
# is GitHub's Actions token endpoint. No override flag — verification
# either succeeds against these pins or the install aborts.
COSIGN_IDENTITY_REGEXP='^https://github\.com/eds-ch/vpn-pack/\.github/workflows/release\.yml@refs/tags/v[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?$'
COSIGN_OIDC_ISSUER="https://token.actions.githubusercontent.com"

# Pinned cosign for bootstrap. Refreshing this pin: download the new
# cosign-linux-arm64 from
# https://github.com/sigstore/cosign/releases/download/<ver>/cosign-linux-arm64,
# verify its sha256 against the upstream cosign_checksums.txt at the
# same release, then update both lines. CI guard (.github/workflows)
# should also fail if these drift from the pinned upstream.
COSIGN_VERSION="v2.4.1"
COSIGN_SHA256_ARM64="3b2e2e3854d0356c45fe6607047526ccd04742d20bd44afb5be91fa2a6e7cb4a"

# COSIGN_BIN is the path to a verified cosign binary. It is set after
# either find-existing or bootstrap, and used by verify_signature.
COSIGN_BIN=""

ensure_cosign() {
    if command -v cosign >/dev/null 2>&1; then
        COSIGN_BIN=$(command -v cosign)
        return 0
    fi
    if [ "$(uname -m)" != "aarch64" ]; then
        echo "FATAL: cosign bootstrap is currently arm64-only; install cosign manually." >&2
        exit 1
    fi
    local cosign_url="https://github.com/sigstore/cosign/releases/download/${COSIGN_VERSION}/cosign-linux-arm64"
    local tmp_bin="${TMPDIR}/cosign"
    info "Bootstrapping cosign ${COSIGN_VERSION} (no persistent install)..."
    curl -fSL --progress-bar -o "$tmp_bin" "$cosign_url" \
        || die "Failed to download cosign from ${cosign_url}"
    local got
    got=$(sha256sum "$tmp_bin" | awk '{print $1}')
    if [ "$got" != "$COSIGN_SHA256_ARM64" ]; then
        die "cosign sha256 mismatch (got ${got}, want ${COSIGN_SHA256_ARM64}); refusing to use untrusted binary"
    fi
    chmod 0700 "$tmp_bin"
    COSIGN_BIN="$tmp_bin"
}

verify_signature() {
    local file=$1
    local bundle=$2
    if [ -z "$COSIGN_BIN" ]; then
        echo "FATAL: ensure_cosign() must run before verify_signature()" >&2
        exit 1
    fi
    if ! "$COSIGN_BIN" verify-blob \
        --certificate-identity-regexp "$COSIGN_IDENTITY_REGEXP" \
        --certificate-oidc-issuer "$COSIGN_OIDC_ISSUER" \
        --bundle "$bundle" \
        "$file" >/dev/null 2>&1; then
        echo "FATAL: signature verification failed for $file" >&2
        exit 1
    fi
}


if [ -n "${VERSION_PIN:-}" ]; then
    PIN_TAG="${VERSION_PIN#v}"
    API_URL="https://api.github.com/repos/${REPO}/releases/tags/v${PIN_TAG}"
else
    API_URL="https://api.github.com/repos/${REPO}/releases/latest"
fi
TMPDIR=$(mktemp -d -t vpn-pack-install.XXXXXX)
chmod 700 "$TMPDIR"
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

if [ -n "${VERSION_PIN:-}" ]; then
    info "Fetching pinned release v${PIN_TAG}..."
else
    info "Fetching latest release info..."
fi
RELEASE_JSON=$(curl -fsSL "$API_URL") || die "Failed to reach GitHub API"

VERSION=$(echo "$RELEASE_JSON" | grep -o '"tag_name" *: *"[^"]*"' | head -1 | grep -o '"v[^"]*"' | tr -d '"')
[ -n "$VERSION" ] || die "Could not determine version"
SEMVER=${VERSION#v}
if [ -n "${VERSION_PIN:-}" ]; then
    info "Pinned version: ${BOLD}${VERSION}${NC}"
else
    info "Latest version: ${BOLD}${VERSION}${NC}"
fi

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

info "Downloading cosign bundles..."
curl -fsSL -o "${TMPDIR}/${ARCHIVE}.cosign.bundle" "${BASE_URL}/${ARCHIVE}.cosign.bundle" \
    || die "cosign bundle for archive missing — refusing to install unsigned release"
curl -fsSL -o "${TMPDIR}/checksums.txt.cosign.bundle" "${BASE_URL}/checksums.txt.cosign.bundle" \
    || die "cosign bundle for checksums missing — refusing to install unsigned release"

ensure_cosign

info "Verifying cosign signature on archive..."
verify_signature "${TMPDIR}/${ARCHIVE}" "${TMPDIR}/${ARCHIVE}.cosign.bundle"
info "Verifying cosign signature on checksums..."
verify_signature "${TMPDIR}/checksums.txt" "${TMPDIR}/checksums.txt.cosign.bundle"

info "Verifying SHA256 checksum..."
(cd "$TMPDIR" && sha256sum -c checksums.txt) || die "Checksum verification failed! Archive may be corrupted."

info "Extracting..."
tar xzf "${TMPDIR}/${ARCHIVE}" -C "$TMPDIR"

info "Running installer..."
echo ""
bash "${TMPDIR}/vpn-pack/install.sh"
