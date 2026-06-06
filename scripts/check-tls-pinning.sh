#!/usr/bin/env bash
#
# CI guard against TLS-pin regressions in the manager Integration client.
# Closes SEC-C5 (preventive). The InsecureSkipVerify: true flag in
# manager/client/integration.go is intentional but copy-paste dangerous
# (the SPKI pin lives in a callback the next contributor might delete).
# This script:
#   - rejects InsecureSkipVerify anywhere under manager/ EXCEPT the
#     single allowed file;
#   - asserts the allowed file actually assigns a VerifyPeerCertificate
#     callback whose body contains BOTH sha256.Sum256 AND
#     subtle.ConstantTimeCompare — the OR form would pass even if the
#     real pin check is deleted (sha256.Sum256 is also used elsewhere
#     in the same package for LoadSPKIPin).
#
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
ALLOWED='manager/client/integration.go'
fail=0

while IFS= read -r line; do
    [[ -z "$line" ]] && continue
    file=${line%%:*}
    rel=${file#"$ROOT/"}
    # Strip the code portion before any `//` so an InsecureSkipVerify
    # mention inside a comment doesn't trip the check.
    code=$(echo "$line" | sed 's|//.*||')
    if [[ "$code" != *InsecureSkipVerify* ]]; then
        continue
    fi
    if [[ "$rel" != "$ALLOWED" ]]; then
        echo "FORBIDDEN InsecureSkipVerify in $rel:" >&2
        echo "  $line" >&2
        fail=1
    fi
done < <(grep -rn 'InsecureSkipVerify' "$ROOT/manager/" --include='*.go' || true)

# Verify the allowed file actually pins via VerifyPeerCertificate AND
# uses both sha256.Sum256 and subtle.ConstantTimeCompare.
awk -v file="$ALLOWED" '
    /^[[:space:]]*VerifyPeerCertificate[[:space:]]*[:=][[:space:]]*func/ { inblock=1; depth=0 }
    inblock {
        body = body $0 "\n"
        depth += gsub(/\{/, "{")
        depth -= gsub(/\}/, "}")
        if (depth == 0 && body ~ /\}/) { exit }
    }
    END {
        if (!body) {
            print "FATAL: " file " must assign VerifyPeerCertificate to a real func" > "/dev/stderr"
            exit 1
        }
        if (body !~ /sha256\.Sum256/) {
            print "FATAL: VerifyPeerCertificate body missing sha256.Sum256" > "/dev/stderr"
            exit 1
        }
        if (body !~ /subtle\.ConstantTimeCompare/) {
            print "FATAL: VerifyPeerCertificate body missing subtle.ConstantTimeCompare" > "/dev/stderr"
            exit 1
        }
    }
' "$ROOT/$ALLOWED" || fail=1

if [[ "$fail" -eq 0 ]]; then
    echo "TLS pinning guard: OK"
fi
exit "$fail"
