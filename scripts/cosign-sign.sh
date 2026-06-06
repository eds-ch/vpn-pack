#!/usr/bin/env bash
#
# Sign release artifacts with cosign (keyless OIDC).
# Closes SEC-B4 — release artifacts gain a verifiable signature.
#
# Usage: scripts/cosign-sign.sh <file> [<file> ...]
#
# Each input file gets a sibling "<file>.cosign.bundle" containing the
# certificate, signature, and Rekor transparency-log entry. Verifiers
# pin both the certificate identity (the maintainer's email) and the
# OIDC issuer to detect substitution.
#
set -euo pipefail

if [[ $# -lt 1 ]]; then
    echo "usage: $0 <file> [<file> ...]" >&2
    exit 2
fi

if ! command -v cosign >/dev/null 2>&1; then
    echo "FATAL: cosign not installed (https://docs.sigstore.dev/cosign/installation)" >&2
    exit 1
fi

for f in "$@"; do
    if [[ ! -f "$f" ]]; then
        echo "FATAL: not a regular file: $f" >&2
        exit 1
    fi
    echo "==> Signing $f"
    cosign sign-blob --yes --bundle "${f}.cosign.bundle" "$f"
done
