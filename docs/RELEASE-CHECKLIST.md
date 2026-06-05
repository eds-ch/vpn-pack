# vpn-pack release checklist

Step-by-step gate for cutting and shipping a new vpn-pack release.

Section ordering matches release execution: validate locally, sign and
publish, then run the **release-time E2E gate** that exercises the
production cosign identity end-to-end. The E2E gate is the only place
production keyless OIDC binding is actually verified — CI cannot
exercise it because the cert identity is minted at signing time.

---

## GAP-001: cosign bootstrap on target device — **MERGE BLOCKER for v1.5.2**

**Discovered:** 2026-06-05 during v1.5.2-beta.1 upgrade test on UDM-SE.

**Symptom:** `get.sh` (since `59c6bef`, Phase 8.1/8.2) calls
`verify_signature() → cosign verify-blob` unconditionally. On a stock
UDM-SE / UCG-Ultra there is no `cosign` binary, so the new `get.sh` exits
fail-closed with `FATAL: cosign required to verify the release signature`.

**Impact:** every existing v1.5.1 user upgrading via the documented
`curl get.sh | bash` path hits this fatal error. The release publishes,
but **nobody can install it** without first manually downloading a
≈110 MB cosign binary onto an embedded router. This is unacceptable UX
and was not part of the Phase 8 design.

**Why this must block merge to `main`:** the moment
`release/v1.5.2-beta` lands on `main`, the public
`raw.githubusercontent.com/.../main/get.sh` URL serves the cosign-strict
version. Stable users on v1.5.1 follow that exact URL — they break
silently. The window between merge and a fix is the blast radius.

**Acceptable resolutions** (pick one before merge):

- **Option A — bootstrap cosign inside `get.sh`.** Detect missing
  `cosign`, download
  `https://github.com/sigstore/cosign/releases/download/v<pin>/cosign-linux-arm64`,
  install to `${TMPDIR}/cosign` (mode 0700), use it for verify, discard
  on exit. Pin a specific cosign version (and its sha256) inside
  get.sh — this is the same trust boundary we already accept for
  Sigstore Fulcio, so it does not widen our threat model. Estimated
  effort: small. **Recommended.**

- **Option B — openssl-only verify.** Cosign-bundle contains a Fulcio
  certificate + signature; verify it with `openssl x509 -verify -CAfile`
  (Fulcio root pinned in get.sh) + `openssl dgst -verify`. UDM-SE ships
  `openssl` natively. Removes the cosign dependency entirely.
  Estimated effort: medium. Riskier (we re-implement bundle parsing).

- **Option C — bundle cosign in the release tarball + bootstrap
  installer.** Defeats the purpose: cosign would be needed *before*
  the tarball is unpacked, so this only works if a second tiny
  installer ships cosign first. Effectively the same trust dance as
  Option A with more moving parts. Not recommended.

**Rejected:** lowering `verify_signature()` to "warn on missing cosign,
continue install" — that re-opens SEC-B4 fail-closed guarantee.

**Required follow-ups when implementing the chosen option:**

1. Update Section 3b below — remove the assumption that cosign is
   pre-installed.
2. Extend `scripts/test-release-roundtrip.sh` to cover the bootstrap
   path: simulate a host without cosign, run get.sh against a mocked
   release endpoint, assert that the bootstrap step runs *and* that a
   tampered bundle still fails the verify.
3. CHANGELOG entry for v1.5.2 must mention the new cosign bootstrap and
   the pinned cosign version + sha256.
4. Re-run the full Phase B → Phase I upgrade test from
   `docs/superpowers/plans/2026-06-04-security-stability-remediation.md`
   Section "Final Ship" — the cosign bootstrap path is *part of* the
   v1.5.2 install surface and was not exercised in the v1.5.2-beta.1
   soak.

**Temporary workaround for v1.5.2-beta.1 soak only:** bypass `get.sh`
entirely; pull tarball with `gh release download` (or `curl` direct
from the release page), unpack on the device, run `install.sh`. This
does **not** exercise the cosign verify path but is fine for testing
Phase 4 (unix socket), Phase 5 (saga), Phase 6 (UDAPI client), Phase 7
(logredact), Phase 8.6 (systemd hardening), Phase 9 (timeouts), and
Phase 10 (tactical batch). Phase 8.1 / 8.2 verification path must be
re-tested after GAP-001 is resolved.

---

## GAP-002: CAP_CHOWN stripped by hardening breaks unix-socket nginx access — **MERGE BLOCKER for v1.5.2**

**Discovered:** 2026-06-05 during v1.5.2-beta.1 upgrade test on UDM-SE.

**Symptom:** `vpn-pack-manager` boots, sets up the socket at
`/run/vpn-pack/manager.sock` with mode 0660, but the subsequent
`os.Chown(path, -1, nginx_gid)` call inside [listener.go:44](manager/listener.go#L44)
fails with `operation not permitted`. The socket is left as
`root:root mode=660`. `nginx` workers run as user `nginx` (gid 132)
and cannot `connect(2)` to a socket they have no read/write bit on.

```
WARN socket chown to nginx group failed; nginx may be unable to connect
err: chown /run/vpn-pack/manager.sock: operation not permitted
```

**Root cause:** Phase 8.6 (`deploy/vpn-pack-manager.service`) narrows
`CapabilityBoundingSet` to `CAP_NET_ADMIN CAP_NET_RAW`. **CAP_CHOWN is
not in this set**, so even though the process runs as uid 0, the kernel
refuses the chown — bounding set is the upper bound on effective caps.
Phase 4 (unix socket with nginx-group access) and Phase 8.6 (cap
narrowing) were designed independently and conflict.

**Verified impact:**
```
$ sudo -u nginx curl -sf --unix-socket /run/vpn-pack/manager.sock http://x/api/status
$ echo $?
7   # connection refused — permission denied
```
nginx cannot proxy `/vpn-pack/api/*` to the manager. **The UI is
broken on every production install of v1.5.2-beta.1.** The local
SSH-as-root test passes because root bypasses file mode.

**Acceptable resolutions** (pick one before merge):

- **Option A — add `CAP_CHOWN` to bounding set + ambient.** In
  `deploy/vpn-pack-manager.service`:
  ```
  CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_CHOWN
  AmbientCapabilities=CAP_CHOWN
  ```
  CAP_CHOWN by itself is a small attack surface (it permits chown of
  any file, but the process already runs as uid 0, so this just
  preserves a privilege we used to have implicitly). Smallest possible
  change; preserves all other Phase 8.6 hardening. **Recommended for
  v1.5.2-beta.2.**

- **Option B — systemd socket activation.** Move socket creation into
  systemd via `vpn-pack-manager.socket`:
  ```
  ListenStream=/run/vpn-pack/manager.sock
  SocketUser=root
  SocketGroup=nginx
  SocketMode=0660
  ```
  Plus pass the fd to the manager via `LISTEN_FDS` env (`systemd.socket`
  protocol). Removes the chown call entirely from the manager. Cleaner
  architecture but requires manager code changes (parse `LISTEN_FDS`,
  drop `openManagerSocket()`'s `net.Listen("unix", ...)`). Medium
  effort.

- **Option C — `Group=nginx` + `UMask=0007` on the service unit.**
  Run the manager with primary group `nginx`; the socket inherits
  that group automatically (no chown needed). Side effect: every file
  the manager writes (`/persistent/vpn-pack/state/*`, manifest,
  tunnels.json) will be `root:nginx` — which may unexpectedly grant
  read access to nginx for the state files. **Not recommended**
  unless we audit every write site for sensitivity.

**Required follow-ups when implementing the chosen option:**

1. Adjust hardening-smoke probe 6 to assert `socket owner=root:nginx`
   instead of just `mode=660`.
2. `make hardening-smoke HOST=...` must be re-run post-fix; this is
   the same probe that surfaced the issue.
3. Re-run Phase D upgrade-diff — `net/socket-modes.txt` anomaly should
   clear.
4. Re-run Phase E with nginx-proxy path (`curl https://device/vpn-pack/api/status`
   with valid UniFi auth cookie) — must return 200, not 502.

**Temporary workaround for v1.5.2-beta.1 soak only:** the manager is
functional and tailscaled is running; tests that hit the API via root
SSH (`curl --unix-socket`) still work. UI-based smoke tests will fail
until GAP-002 is fixed. Do **not** ask test users to install
v1.5.2-beta.1 — they will see broken UI.

---

## GAP-002b: socket unit missing `RuntimeDirectory=` — fails at every reboot — **MERGE BLOCKER for v1.5.2**

**Discovered:** 2026-06-05 during external review of the GAP-002
socket-activation fix. Caught before users hit it (the beta.2 upgrade
test did not reboot the device, so the regression was latent).

**Symptom:** `vpn-pack-manager.socket` declares
`ListenStream=/run/vpn-pack/manager.sock` but has no `RuntimeDirectory=`.
`/run` is tmpfs on UDM-SE / UCG-Ultra / UDR-SE (CLAUDE.md persistence
matrix), so after every reboot `/run/vpn-pack/` does not exist.
systemd does **not** auto-create parent directories for `ListenStream=`
unix-socket paths — the unit fails with `ENOENT` before the manager is
ever activated. `vpn-pack-manager.service` has `Requires=` on the
socket, so the service also refuses to start. The manager is dead
after every reboot; nginx returns 502 from `/vpn-pack/`.

**Why the beta.2 upgrade test missed it:** the test went beta.1 → beta.2
without rebooting. beta.1's `.service` had `RuntimeDirectory=vpn-pack`,
which had already created `/run/vpn-pack/`. The tmpfs entry survived
the install. The fresh-boot path was never exercised.

**Resolution:** add `RuntimeDirectory=vpn-pack` + `RuntimeDirectoryMode=0755`
to the `[Socket]` section of `deploy/vpn-pack-manager.socket`. systemd
ref-counts `RuntimeDirectory=` across all units that declare it; the
`.service` retains its own declaration as defense-in-depth (downgrade /
manual partial install scenarios).

**Regression guard:** `scripts/hardening-smoke.sh` probe 8 tears down
both units, wipes `/run/vpn-pack/`, starts the socket cold, and asserts
both the directory and the units come back active. Catches this exact
class of regression without requiring an actual `reboot`.

**Out-of-scope follow-ups** (called out during the GAP-002b review,
backlogged for v1.5.3 — none are merge blockers):

- Manager exit 78 (UniFi-version-check failure) under socket activation:
  every subsequent nginx connection re-triggers activation, manager
  exits 78 again, `RestartPreventExitStatus=78` declines to restart.
  Net effect is "502 forever after the first connection" rather than
  "service in failed state at boot". The user can still SSH in and
  diagnose; documented here so operators understand the new failure
  signature.
- Boot-order race: `Requires=vpn-pack-manager.socket` on the service
  plus `Wants=tailscaled.service` means an incoming nginx connection
  can activate the manager before tailscaled finishes coming up. The
  manager's tailscaled-localapi client already retries (Phase 9
  bounded localapi decorator); not a new failure mode introduced by
  socket activation, but slightly more likely now.

---

## GAP-003: CI workflow does not run on `release/*` branches — **fix before next release**

**Discovered:** 2026-06-05 during v1.5.2-beta.1 build preparation.

**Symptom:** `make check` surfaced 12 golangci-lint issues introduced
across Phase 1-10 commits on `release/v1.5.2-beta`. CI never caught
them because the workflow trigger excludes the release branch.

**Root cause:** [.github/workflows/ci.yml](.github/workflows/ci.yml#L1-L5)
declares:

```yaml
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]
```

Push events to `release/v1.5.2-beta` (and earlier `release/*` branches)
do not match either trigger. There is no `workflow_dispatch` either, so
manual runs are impossible without editing the file.

**Impact:** every release-branch commit since v1.5.1 (Apr 30) ran
without CI. Lint regressions, test regressions, and build breakage
would all reach a tagged beta release before anyone noticed — exactly
what happened with the 12 lint findings.

**Resolution:**

```yaml
on:
  push:
    branches: [main, "release/**"]
  pull_request:
    branches: [main]
  workflow_dispatch:
```

`release/**` covers `release/v1.5.2-beta`, `release/v1.5.3-beta`, etc.
`workflow_dispatch` adds a manual-run option for ad-hoc verification.
PR trigger remains `main`-only — release branches don't open PRs in
themselves; the only PR is `release/* → main` at merge time.

**Effort:** trivial (4-line edit). Include in the same v1.5.2-beta.2
fix batch as GAP-001 / GAP-002.

**Required follow-ups:**

1. After the workflow change lands on `release/v1.5.2-beta`, verify CI
   actually fires on the next push.
2. Add a CHANGELOG entry under v1.5.2 noting the CI scope change.

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
