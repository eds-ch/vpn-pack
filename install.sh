#!/bin/sh
# vpn-pack installer for UniFi Cloud Gateway devices
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/eds-ch/vpn-pack/main/install.sh | sh
#
# Pin specific version:
#   VERSION_PIN=v1.0.0 curl -fsSL https://raw.githubusercontent.com/eds-ch/vpn-pack/main/install.sh | sh
#
# Phase decomposition (Task 8.12) lets scripts/test-installers.sh
# exercise vp_download_and_verify against a tampered bundle without
# satisfying the on-device preflight. Each vp_* function preserves
# the same user-visible output and ordering as the pre-refactor
# script (locked by scripts/test-installer-characterization.sh).
#
set -eu

REPO="eds-ch/vpn-pack"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"

# Pinned per release line; rotate together with the signing identity
# in CHANGELOG.md. No override flag — verification either succeeds
# against this identity or the install aborts.
COSIGN_IDENTITY="eduard.chesnokov@gmail.com"
COSIGN_ISSUER="https://github.com/login/oauth"

# Pinned cosign for bootstrap. Refreshing this pin: download the new
# cosign-linux-arm64 from
# https://github.com/sigstore/cosign/releases/download/<ver>/cosign-linux-arm64,
# verify its sha256 against the upstream cosign_checksums.txt at the
# same release, then update both lines.
COSIGN_VERSION="v2.4.1"
COSIGN_SHA256_ARM64="3b2e2e3854d0356c45fe6607047526ccd04742d20bd44afb5be91fa2a6e7cb4a"

# COSIGN_BIN is set by ensure_cosign and used by verify_signature.
COSIGN_BIN=""

ensure_cosign() {
    if command -v cosign >/dev/null 2>&1; then
        COSIGN_BIN=$(command -v cosign)
        return 0
    fi
    if [ "$(uname -m)" != "aarch64" ]; then
        printf 'FATAL: cosign bootstrap is currently arm64-only; install cosign manually.\n' >&2
        exit 1
    fi
    cosign_dir=${INSTALL_TMP:-${TMPDIR:-/tmp}}
    cosign_tmp="${cosign_dir}/cosign"
    cosign_url="https://github.com/sigstore/cosign/releases/download/${COSIGN_VERSION}/cosign-linux-arm64"
    info "Bootstrapping cosign ${COSIGN_VERSION} (no persistent install)..."
    if ! curl -fsSL -o "$cosign_tmp" "$cosign_url"; then
        printf 'FATAL: failed to download cosign from %s\n' "$cosign_url" >&2
        exit 1
    fi
    got=$(sha256sum "$cosign_tmp" | awk '{print $1}')
    if [ "$got" != "$COSIGN_SHA256_ARM64" ]; then
        printf 'FATAL: cosign sha256 mismatch (got %s, want %s); refusing to use untrusted binary\n' \
            "$got" "$COSIGN_SHA256_ARM64" >&2
        exit 1
    fi
    chmod 0700 "$cosign_tmp"
    COSIGN_BIN="$cosign_tmp"
}

verify_signature() {
    file=$1
    bundle=$2
    if [ -z "$COSIGN_BIN" ]; then
        printf 'FATAL: ensure_cosign must run before verify_signature\n' >&2
        exit 1
    fi
    if ! "$COSIGN_BIN" verify-blob \
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

vp_preflight() {
    info "vpn-pack installer for UniFi Cloud Gateway"
    echo ""

    if [ "$(id -u)" -ne 0 ]; then
        die "Must be run as root"
    fi

    ARCH=$(uname -m)
    if [ "$ARCH" != "aarch64" ]; then
        die "Unsupported architecture: $ARCH (need aarch64)"
    fi

    if ! command -v ubnt-device-info >/dev/null 2>&1; then
        die "Not a UniFi device (ubnt-device-info not found)"
    fi

    # Hard block — devices without a controller are not supported.
    if ! systemctl is-active --quiet unifi-core 2>/dev/null; then
        die "unifi-core is not running. This device needs a working UniFi OS controller."
    fi

    if ! command -v systemctl >/dev/null 2>&1; then
        die "systemd not found"
    fi

    if [ ! -d /persistent ]; then
        die "/persistent/ not found"
    fi
    if [ ! -w /persistent ]; then
        die "/persistent/ is not writable"
    fi

    for cmd in iptables ip curl tar; do
        if ! command -v "$cmd" >/dev/null 2>&1; then
            die "Required command not found: $cmd"
        fi
    done

    if [ ! -e /dev/net/tun ]; then
        die "/dev/net/tun not found (required for Tailscale)"
    fi

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

    AVAIL_KB=$(df -k /persistent | awk 'NR==2 {print $4}')
    AVAIL_MB=$((AVAIL_KB / 1024))
    if [ "$AVAIL_MB" -lt 100 ]; then
        die "/persistent/ has only ${AVAIL_MB}MB free (need at least 100MB)"
    fi
    info "/persistent/ has ${AVAIL_MB}MB free"

    echo ""
}

vp_resolve_release() {
    if [ -n "${VERSION_PIN:-}" ]; then
        RELEASE_URL="https://api.github.com/repos/${REPO}/releases/tags/${VERSION_PIN}"
        info "Pinned version: ${VERSION_PIN}"
    else
        RELEASE_URL="$GITHUB_API"
    fi

    info "Fetching release info from GitHub..."
    RELEASE_JSON=$(curl -fsSL "$RELEASE_URL") || die "Failed to fetch release info from GitHub. Check internet connectivity."

    TAG_NAME=$(echo "$RELEASE_JSON" | grep '"tag_name"' | head -1 | sed 's/.*"tag_name"[[:space:]]*:[[:space:]]*"//' | sed 's/".*//')
    if [ -z "$TAG_NAME" ]; then
        die "Could not parse release tag from GitHub response"
    fi

    ASSET_URL=$(echo "$RELEASE_JSON" | grep '"browser_download_url"' | grep 'vpn-pack-.*\.tar\.gz"' | head -1 | sed 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"//' | sed 's/".*//')
    if [ -z "$ASSET_URL" ]; then
        die "Could not find vpn-pack archive in release $TAG_NAME"
    fi

    CHECKSUMS_URL=$(echo "$RELEASE_JSON" | grep '"browser_download_url"' | grep 'checksums\.txt"' | head -1 | sed 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"//' | sed 's/".*//')

    ARCHIVE_BUNDLE_URL=$(echo "$RELEASE_JSON" | grep '"browser_download_url"' | grep 'vpn-pack-.*\.tar\.gz\.cosign\.bundle"' | head -1 | sed 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"//' | sed 's/".*//')
    CHECKSUMS_BUNDLE_URL=$(echo "$RELEASE_JSON" | grep '"browser_download_url"' | grep 'checksums\.txt\.cosign\.bundle"' | head -1 | sed 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"//' | sed 's/".*//')

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
}

vp_download_and_verify() {
    INSTALL_TMP=$(mktemp -d -t vpn-pack-install.XXXXXX)
    chmod 700 "$INSTALL_TMP"
    trap 'rm -rf "$INSTALL_TMP"' EXIT

    ARCHIVE_FILE=$(basename "$ASSET_URL")

    info "Downloading ${ARCHIVE_FILE}..."
    curl -fsSL -o "${INSTALL_TMP}/${ARCHIVE_FILE}" "$ASSET_URL" || {
        rm -rf "$INSTALL_TMP"
        die "Download failed"
    }

    # SEC-C1: signed verification, no silent skip.
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

    ensure_cosign

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
}

vp_extract() {
    info "Extracting..."
    tar xzf "${INSTALL_TMP}/${ARCHIVE_FILE}" -C "$INSTALL_TMP"

    echo ""

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
}

vp_run_inner_installer() {
    bash "$EXTRACTED/install.sh"
    INSTALL_EXIT=$?

    rm -rf "$INSTALL_TMP"

    if [ "$INSTALL_EXIT" -ne 0 ]; then
        die "Installation failed (exit code: $INSTALL_EXIT)"
    fi

    echo ""
    info "vpn-pack ${TAG_NAME} installed successfully"
}

# ── Orchestration ─────────────────────────────────────────────────
vp_preflight
vp_resolve_release
vp_download_and_verify
vp_extract
vp_run_inner_installer
