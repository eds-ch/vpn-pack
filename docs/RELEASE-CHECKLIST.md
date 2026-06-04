# vpn-pack release checklist

Step-by-step gate for cutting and shipping a new vpn-pack release.

Section ordering matches release execution: validate locally, sign and
publish, then run the **release-time E2E gate** that exercises the
production cosign identity end-to-end. The E2E gate is the only place
production keyless OIDC binding is actually verified — CI cannot
exercise it because the cert identity is minted at signing time.

---

## 1. Pre-release: local validation

From a clean checkout of the release branch:

```bash
make check                  # go vet/test, svelte-check, vitest, TLS pinning
bash scripts/test-installers.sh
bash scripts/test-deploy-symlink.sh
bash scripts/test-uninstall-integrity.sh
bash scripts/test-systemd-hardening.sh
bash scripts/check-no-key-leak.sh     # if present (SEC-C6 guard)
bash scripts/test-release-roundtrip.sh
```

`test-release-roundtrip.sh` exercises the cosign sign+verify mechanics
locally with an ephemeral keypair and asserts get.sh / install.sh still
call `cosign verify-blob --certificate-identity --certificate-oidc-issuer --bundle`.
Skips cleanly if `cosign` is not installed locally.

Confirm:

- `VERSION` file matches the intended tag (no trailing newline issues).
- `CHANGELOG.md` notes the signing identity if it has been rotated.
- `patches/` apply cleanly against the pinned `TAILSCALE_VERSION`
  (`make verify-patches`).

## 2. Sign and publish

```bash
git tag -a "v$(cat VERSION)" -m "vpn-pack v$(cat VERSION)"
git push origin "v$(cat VERSION)"
make release                # build → package → checksums → cosign-sign → gh release create
```

`make release` invokes `scripts/cosign-sign.sh` which prompts for the
**production identity** (`eduard.chesnokov@gmail.com` via
`https://github.com/login/oauth`). Do not substitute another identity
here — the SHA of the OIDC cert is what end users pin against.

## 3. Release-time E2E gate (production identity)

**This is the only step that exercises the production trust boundary.**
Run it on a clean UDM-SE (or equivalent UniFi Cloud Gateway) — not on
the development host.

### 3a. Clean device prerequisites

- Reset state if reusing a test device:
  ```bash
  ssh root@<device> "systemctl stop vpn-pack-manager tailscaled 2>/dev/null; \
      bash /persistent/vpn-pack/uninstall.sh --cleanup 2>/dev/null || true; \
      rm -rf /persistent/vpn-pack"
  ```
- Confirm `unifi-core` is running and UniFi Network ≥ 10.1 is installed
  (`get.sh` enforces both).

### 3b. Run the installer over the published release

```bash
ssh root@<device> 'curl -fsSL https://raw.githubusercontent.com/eds-ch/vpn-pack/main/get.sh \
    | VERSION_PIN=<the-tag-without-v> bash'
```

(For a pre-release, `VERSION_PIN` must precede `bash`, not the `curl`
in the pipe — otherwise the variable does not reach the script.)

### 3c. Post-install assertions

After the installer exits 0:

```bash
ssh root@<device> "systemctl is-active tailscaled vpn-pack-manager"
# expect: active / active

ssh root@<device> "journalctl -u vpn-pack-manager -n 100 --no-pager"
# expect: clean startup, no panic / no repeated cosign-related errors
# and no 'fail-closed' boot-time security probe failures

ssh root@<device> "head -1 /persistent/vpn-pack/VERSION"
# expect: the tag you just released (without leading v)
```

Open the manager UI at `https://<device>/vpn-pack/` and confirm the
login page renders (no 5xx from nginx).

### 3d. Negative roundtrip (optional but recommended on every other release)

On the same device, confirm cosign actually catches tamper:

```bash
ssh root@<device> '
    TMP=$(mktemp -d) && cd "$TMP" && \
    curl -fsSL "https://github.com/eds-ch/vpn-pack/releases/download/v<TAG>/vpn-pack-<TAG>.tar.gz" -o a.tar.gz && \
    curl -fsSL "https://github.com/eds-ch/vpn-pack/releases/download/v<TAG>/vpn-pack-<TAG>.tar.gz.cosign.bundle" -o a.tar.gz.cosign.bundle && \
    printf x >> a.tar.gz && \
    cosign verify-blob \
        --certificate-identity "eduard.chesnokov@gmail.com" \
        --certificate-oidc-issuer "https://github.com/login/oauth" \
        --bundle a.tar.gz.cosign.bundle a.tar.gz \
    && echo UNEXPECTED-PASS || echo expected-fail
'
# expect: expected-fail
```

If this passes the tampered archive, the release is broken — pull it
from GitHub and re-investigate signing before reusing the tag.

## 4. Post-ship

- Note any signing-identity rotation in `CHANGELOG.md` under the new
  release.
- Update the wiki install snippet if the curl one-liner changed.
- Soak the new release per `docs/soak-checkpoint.sh` cadence; gate the
  next bump on a clean soak run.
