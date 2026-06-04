#!/bin/sh
# vpn-pack installer for UniFi Cloud Gateway devices
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/eds-ch/vpn-pack/main/install.sh | sh
#
# Pin specific version:
#   VERSION_PIN=v1.0.0 curl -fsSL https://raw.githubusercontent.com/eds-ch/vpn-pack/main/install.sh | sh
#
set -eu

REPO="eds-ch/vpn-pack"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"
INSTALL_TMP="/tmp/vpn-pack-install"

# Pinned per release line; rotate together with the signing identity
# in CHANGELOG.md. No override flag — verification either succeeds
# against this identity or the install aborts.
COSIGN_IDENTITY="eduard.chesnokov@gmail.com"
COSIGN_ISSUER="https://github.com/login/oauth"

verify_signature() {
    file=$1
    bundle=$2
    if ! command -v cosign >/dev/null 2>&1; then
        printf 'FATAL: cosign required to verify the release signature.\n' >&2
        printf 'Install: https://docs.sigstore.dev/cosign/installation\n' >&2
        exit 1
    fi
    if ! cosign verify-blob \
        --certificate-identity "$COSIGN_IDENTITY" \
        --certificate-oidc-issuer "$COSIGN_ISSUER" \
        --bundle "$bundle" \
        "$file" >/dev/null 2>&1; then
        printf 'FATAL: signature verification failed for %s\n' "$file" >&2
        exit 1
    fi
}

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

info()  { printf "${GREEN}[+]${NC} %s\n" "$*"; }
warn()  { printf "${YELLOW}[!]${NC} %s\n" "$*"; }
error() { printf "${RED}[x]${NC} %s\n" "$*"; }
die()   { error "$*"; exit 1; }

# ── Phase 1: System validation ────────────────────────────────────

info "vpn-pack installer for UniFi Cloud Gateway"
echo ""

# Root check
if [ "$(id -u)" -ne 0 ]; then
    die "Must be run as root"
fi

# Architecture
ARCH=$(uname -m)
if [ "$ARCH" != "aarch64" ]; then
    die "Unsupported architecture: $ARCH (need aarch64)"
fi

# UniFi device
if ! command -v ubnt-device-info >/dev/null 2>&1; then
    die "Not a UniFi device (ubnt-device-info not found)"
fi

# UniFi controller (hard block — devices without controller are not supported)
if ! systemctl is-active --quiet unifi-core 2>/dev/null; then
    die "unifi-core is not running. This device needs a working UniFi OS controller."
fi

# systemd
if ! command -v systemctl >/dev/null 2>&1; then
    die "systemd not found"
fi

# /persistent/ partition
if [ ! -d /persistent ]; then
    die "/persistent/ not found"
fi
if [ ! -w /persistent ]; then
    die "/persistent/ is not writable"
fi

# Required commands
for cmd in iptables ip curl tar; do
    if ! command -v "$cmd" >/dev/null 2>&1; then
        die "Required command not found: $cmd"
    fi
done

# TUN device
if [ ! -e /dev/net/tun ]; then
    die "/dev/net/tun not found (required for Tailscale)"
fi

# Device model check (whitelist = silent, unknown = warning)
DEVICE_MODEL=$(ubnt-device-info model_short 2>/dev/null || echo "unknown")
DEVICE_FULL=$(ubnt-device-info model 2>/dev/null || echo "unknown")
FIRMWARE=$(ubnt-device-info firmware 2>/dev/null || echo "unknown")

case "$DEVICE_MODEL" in
    UDM-SE|UDM-Pro|UDM-Pro-Max|UDM|UCG-Ultra|UDR-SE)
        ;;
    *)
        warn "Unknown device model: $DEVICE_MODEL ($DEVICE_FULL)"
        warn "This device has not been tested. Proceeding anyway."
        ;;
esac

info "Device: ${BOLD}${DEVICE_FULL}${NC} (${DEVICE_MODEL})"
info "Firmware: ${FIRMWARE}"

# Disk space check (100MB minimum)
AVAIL_KB=$(df -k /persistent | awk 'NR==2 {print $4}')
AVAIL_MB=$((AVAIL_KB / 1024))
if [ "$AVAIL_MB" -lt 100 ]; then
    die "/persistent/ has only ${AVAIL_MB}MB free (need at least 100MB)"
fi
info "/persistent/ has ${AVAIL_MB}MB free"

echo ""

# ── Phase 2: Version resolution ───────────────────────────────────

if [ -n "${VERSION_PIN:-}" ]; then
    RELEASE_URL="https://api.github.com/repos/${REPO}/releases/tags/${VERSION_PIN}"
    info "Pinned version: ${VERSION_PIN}"
else
    RELEASE_URL="$GITHUB_API"
fi

info "Fetching release info from GitHub..."
RELEASE_JSON=$(curl -fsSL "$RELEASE_URL") || die "Failed to fetch release info from GitHub. Check internet connectivity."

# Parse tag_name from JSON (no jq dependency)
TAG_NAME=$(echo "$RELEASE_JSON" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"//' | sed 's/".*//')
if [ -z "$TAG_NAME" ]; then
    die "Could not parse release tag from GitHub response"
fi

# Find asset URL for vpn-pack-*.tar.gz
ASSET_URL=$(echo "$RELEASE_JSON" | grep '"browser_download_url"' | grep 'vpn-pack-.*\.tar\.gz"' | head -1 | sed 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"//' | sed 's/".*//')
if [ -z "$ASSET_URL" ]; then
    die "Could not find vpn-pack archive in release $TAG_NAME"
fi

# Find checksums URL
CHECKSUMS_URL=$(echo "$RELEASE_JSON" | grep '"browser_download_url"' | grep 'checksums\.txt"' | head -1 | sed 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"//' | sed 's/".*//')

# Find cosign bundle URLs (SEC-C1: signed verification, no silent skip)
ARCHIVE_BUNDLE_URL=$(echo "$RELEASE_JSON" | grep '"browser_download_url"' | grep 'vpn-pack-.*\.tar\.gz\.cosign\.bundle"' | head -1 | sed 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"//' | sed 's/".*//')
CHECKSUMS_BUNDLE_URL=$(echo "$RELEASE_JSON" | grep '"browser_download_url"' | grep 'checksums\.txt\.cosign\.bundle"' | head -1 | sed 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"//' | sed 's/".*//')

# Show upgrade info if already installed
if [ -f /persistent/vpn-pack/VERSION ]; then
    CURRENT_VERSION=$(head -1 /persistent/vpn-pack/VERSION 2>/dev/null || echo "")
    if [ -n "$CURRENT_VERSION" ]; then
        info "Current version: ${BOLD}${CURRENT_VERSION}${NC}"
    fi
    info "New version: ${BOLD}${TAG_NAME}${NC}"
    info "Upgrading..."
else
    info "Installing vpn-pack ${BOLD}${TAG_NAME}${NC}..."
fi

echo ""

# ── Phase 3: Download & verify ────────────────────────────────────

rm -rf "$INSTALL_TMP"
mkdir -p "$INSTALL_TMP"

ARCHIVE_FILE=$(basename "$ASSET_URL")

info "Downloading ${ARCHIVE_FILE}..."
curl -fsSL -o "${INSTALL_TMP}/${ARCHIVE_FILE}" "$ASSET_URL" || {
    rm -rf "$INSTALL_TMP"
    die "Download failed"
}

# Verify cosign signature + SHA256 checksum (SEC-C1: no silent skip).
if [ -z "$CHECKSUMS_URL" ]; then
    rm -rf "$INSTALL_TMP"
    die "Release has no checksums.txt asset — refusing to install"
fi
if [ -z "$ARCHIVE_BUNDLE_URL" ] || [ -z "$CHECKSUMS_BUNDLE_URL" ]; then
    rm -rf "$INSTALL_TMP"
    die "Release has no cosign bundles — refusing to install unsigned release"
fi
if ! command -v sha256sum >/dev/null 2>&1; then
    rm -rf "$INSTALL_TMP"
    die "sha256sum required for checksum verification"
fi

info "Downloading cosign bundles..."
curl -fsSL -o "${INSTALL_TMP}/${ARCHIVE_FILE}.cosign.bundle" "$ARCHIVE_BUNDLE_URL" || {
    rm -rf "$INSTALL_TMP"
    die "cosign bundle for archive download failed"
}
curl -fsSL -o "${INSTALL_TMP}/checksums.txt.cosign.bundle" "$CHECKSUMS_BUNDLE_URL" || {
    rm -rf "$INSTALL_TMP"
    die "cosign bundle for checksums download failed"
}

info "Downloading checksums..."
curl -fsSL -o "${INSTALL_TMP}/checksums.txt" "$CHECKSUMS_URL" || {
    rm -rf "$INSTALL_TMP"
    die "Checksums download failed"
}

info "Verifying cosign signature on archive..."
verify_signature "${INSTALL_TMP}/${ARCHIVE_FILE}" "${INSTALL_TMP}/${ARCHIVE_FILE}.cosign.bundle"
info "Verifying cosign signature on checksums..."
verify_signature "${INSTALL_TMP}/checksums.txt" "${INSTALL_TMP}/checksums.txt.cosign.bundle"

info "Verifying SHA256 checksum..."
if ! (cd "$INSTALL_TMP" && sha256sum -c checksums.txt >/dev/null 2>&1); then
    rm -rf "$INSTALL_TMP"
    die "Checksum verification FAILED — download may be corrupted"
fi
info "Signatures and checksum verified"

info "Extracting..."
tar xzf "${INSTALL_TMP}/${ARCHIVE_FILE}" -C "$INSTALL_TMP"

echo ""

# ── Phase 4: Hand off to package installer ────────────────────────

# Find extracted directory
EXTRACTED=""
for d in "${INSTALL_TMP}"/vpn-pack*; do
    if [ -d "$d" ] && [ -f "$d/install.sh" ]; then
        EXTRACTED="$d"
        break
    fi
done

if [ -z "$EXTRACTED" ]; then
    rm -rf "$INSTALL_TMP"
    die "Archive does not contain expected install.sh"
fi

# Copy VERSION to /persistent so manager can read it
if [ -f "$EXTRACTED/VERSION" ]; then
    mkdir -p /persistent/vpn-pack
    cp -f "$EXTRACTED/VERSION" /persistent/vpn-pack/VERSION
fi

# Run the inner installer
bash "$EXTRACTED/install.sh"
INSTALL_EXIT=$?

# ── Phase 5: Cleanup ──────────────────────────────────────────────

rm -rf "$INSTALL_TMP"

if [ "$INSTALL_EXIT" -ne 0 ]; then
    die "Installation failed (exit code: $INSTALL_EXIT)"
fi

echo ""
info "vpn-pack ${TAG_NAME} installed successfully"
