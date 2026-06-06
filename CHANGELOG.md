# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Security
- Release artifacts (`vpn-pack-<ver>.tar.gz`, `checksums.txt`) are now
  signed with cosign keyless OIDC. Verifiers must pin the identity
  `eduard.chesnokov@gmail.com` and issuer
  `https://github.com/login/oauth` — both `get.sh` and the standalone
  `install.sh` enforce this and refuse silent fallback. See README
  "Verifying release signatures" for the manual `cosign verify-blob`
  invocation. This identity is the documented signer for the upcoming
  v1.6.0 release line; rotation will be noted here and bumped in the
  installer constants together.

## [1.5.2-beta.8] - 2026-06-05

### Changed
- Tailscale updated from 1.96.4 to 1.98.5 (resolves the deferral
  recorded in `[1.5.2-beta.7]`). Upstream skipped a stable 1.97 minor
  — only `v1.97.0-pre` exists — so this is a two-minor jump landing
  on the latest stable. Patches `001..006` regenerated against fresh
  v1.98.5 context: payload identical (line-for-line `+`/`-` counts
  match), only header `# Tailscale-Version` and surrounding context
  refreshed; `make verify-patches` clean.
- Upstream changes inside the patch surface that warranted review
  (all benign, none required patch-logic changes):
  - `wgengine/router/osrouter`: `src_valid_mark=1` set alongside the
    connmark save/restore rules so `rp_filter`'s reverse-path check
    actually uses the bypass fwmark — fixes a martian-drop class of
    bugs on hosts where `net.ipv4.conf.all.src_valid_mark` defaults
    to 0. Complements (does not collide with) our patch 006 exit-node
    routing into table 53.
  - `wgengine/router/osrouter`: netfilter add-on blocks (connmark
    rules and CGNAT drop) now gate on `r.netfilterMode` instead of
    `cfg.NetfilterMode` to match the SNAT/loopback pattern when
    `setNetfilterModeLocked` fails.
  - `util/linuxfw`: incoming CGNAT-range traffic now allowed when
    the new nodeattr is set; nil-deref fixes for nftables chain
    check and connmark rules without IPv6.

### Notes
- Manager Go code (`tailscale.com/{client/local,ipn,ipn/ipnstate,tailcfg,types/key,types/netmap,types/views}`)
  compiles unchanged against v1.98.5 — no API breaks. `go test -race
  -shuffle=on ./...`, `golangci-lint`, `svelte-check`, and `vitest`
  (220/220) all green on the dev host. Full ARM64 cross-build clean.
- 24h idle soak passed on UDM-SE (firmware 5.1.15, UniFi Network
  10.4.57) starting 2026-06-05 12:06Z. 13 checkpoints at 2h cadence,
  zero NRestarts on `tailscaled` and `vpn-pack-manager`, zero
  `journalctl -p warning` entries across the run, zero `journalctl -k`
  events after the post-OTA boot window, manager `/api/health`
  `reconnects=0` throughout. RSS reached plateau after ~14h
  (tailscaled ~40.5 MB, manager ~19.2 MB); FD and thread counts flat
  after T+6h. `ts-*` chain count (19) and route tables 52/30 / 53/0
  identical at T+0 and T+24h. Full trend per-checkpoint kept in the
  gitignored `docs/soak-1.5.2-beta.8-notes.md`. This was an
  observation soak (no synthetic API traffic, no peer-change events);
  it validates background watcher and Tailscale steady-state on the
  patched code, not per-request paths.
- Earlier note about patch surface in this Tailscale range: patches
  005 (`util/linuxfw`) and 006 (`wgengine/router/osrouter`) sit in
  the most-changed upstream zones (+80 / +68 LoC respectively); the
  soak above is the gate that closes that risk for the patched code's
  idle behavior.

## [1.5.2-beta.7] - 2026-06-05

### Security
- Go toolchain bumped to 1.26.4 via `toolchain` directive in
  `manager/go.mod`. Pulls in upstream patches for `crypto/x509`,
  `crypto/tls`, `archive/tar`, `html/template`, `net/http`, `mime`,
  `net/textproto` across Go 1.26.2/.3/.4. Local Go install is not
  required to upgrade — `GOTOOLCHAIN=auto` downloads 1.26.4 on first
  build. Tailscale upstream `go.mod` is unchanged.
- UI: `vite` 6.4.1 → 6.4.3 closes two high-severity advisories
  (dev-server path traversal in optimized-deps `.map` handling and
  WebSocket arbitrary file read). In-range `npm audit fix` also swept
  transitive `undici`, `rollup`, `postcss`, `picomatch`. `npm audit`
  now reports 0 vulnerabilities.

### Changed
- manager: `fsnotify` v1.9.0 → v1.10.1 (inotify watch handling on
  shared path prefixes; kqueue fd leak fix)
- manager: `golang.org/x/sys` v0.40.0 → v0.45.0
- ui: `svelte` 5.53.1 → 5.56.2 (reactivity / effect-tracking patches)
- ui: `vitest` 4.0.18 → 4.1.8 (test-only)

### Notes
- No functional changes to the runtime; soak from `1.5.2-beta.6`
  carries forward. Patch stack (`patches/001..006`) applies cleanly
  against unchanged Tailscale `v1.96.4`. Tailscale `1.96.4 → 1.98.5`
  is intentionally deferred — see `release/v1.5.2-beta` discussion.

## [1.5.1] - 2026-04-30

### Changed
- Tailscale updated from 1.96.2 to 1.96.4

### Notes
- Upstream `v1.96.2..v1.96.4` introduces no Linux-side runtime changes affecting
  this build. The range contains a Go toolchain bump (MIPS/Synology segv fix —
  not applicable to ARM64 but inherited), a Windows NRPT DNS fix, a kube/certs
  TLS domain discovery change, and a test skip — all out of scope for the
  vpn-pack ARM64 Linux daemon. This release is a baseline-maintenance bump to
  stay current with upstream; all six local patches apply cleanly with no
  regeneration.
- Released after a 24-hour soak on UDM-SE through pre-release `v1.5.1-beta.1`
  with four checkpoints (T+6h/12h/18h/24h), all clean.

## [1.5.1-beta.1] - 2026-04-29

### Changed
- Tailscale updated from 1.96.2 to 1.96.4

### Notes
- Upstream `v1.96.2..v1.96.4` introduces no Linux-side runtime changes affecting
  this build. The range contains a Go toolchain bump (MIPS/Synology segv fix —
  not applicable to ARM64 but inherited), a Windows NRPT DNS fix, a kube/certs
  TLS domain discovery change, and a test skip — all out of scope for the
  vpn-pack ARM64 Linux daemon. This release is a baseline-maintenance bump to
  stay current with upstream; all six local patches apply cleanly with no
  regeneration.

## [1.5.0] - 2026-03-19

### Changed
- Tailscale updated from 1.94.2 to 1.96.2

### Fixed
- Log source filter showed "No matching entries" — timestamp was passed as source instead of `"tailscale"`, breaking UI filter
- Variable shadowing in `logcollector.go`
- Patch 006 hunk offsets regenerated for v1.96.2

## [1.4.2] - 2026-03-17

### Fixed
- Uninstall `--cleanup` now removes exit node ip rules (table 53, prio 5280-5300) and iptables masquerade rules that were previously orphaned
- Uninstall `--cleanup` now deletes DNS forwarding policies from the Integration API
- `uninstall.sh` removes itself so `--purge` leaves no orphan files in `/persistent/vpn-pack/`

## [1.4.1] - 2026-03-13

### Fixed
- Firewall deduplication test failed in CI where `tailscale0` interface doesn't exist — `interfaceExists` is now mockable in tests

## [1.4.0] - 2026-03-13

### Added
- **Exit node**: confirmation gate before enabling, selective per-client ip rules (table 53), mutual exclusion with advertise exit node
- **Remote exit node**: peer selection with online/offline grouping, routing mode (all traffic / selected clients), confirmation gate, unified Apply flow with routes
- **Routing health monitor**: validates rp_filter, BypassMark, IPv6 settings
- **Subnet validator**: table 52 route validation, accept-routes vs S2S conflict detection
- **S2S routing**: per-tunnel configurable route metric, ref-counted route ownership (fixes shared-prefix race)
- **PBR conflict detection**: warns when Traffic Routes conflict with S2S tunnels
- **HealthTracker**: exponential backoff, `/api/health` endpoint, SSE health events
- **Beta release support**: `VERSION_PIN` env var in `get.sh`, pre-release version comparison
- **Firewall zone reconciliation** for WG S2S tunnels
- UI tests for remote exit node, exit node toggle, API client

### Changed
- **Architecture overhaul**: monolithic Server decomposed into isolated services (Settings, Diagnostics, Integration, Routing, Tailscale, WgS2s, FirewallOrchestrator) with dependency injection and interfaces
- **Exit node split**: separated into advertise (simple toggle) and remote exit node (peer selection, client picker, ip rules) — enabling one atomically disables the other
- Result types, context propagation, typed SSE events throughout
- Centralized manifest mutations with atomic save
- Synchronous firewall coordination with transaction semantics and inline rollback
- Restructured `manager/` into sub-packages for progressive disclosure
- Frontend: extracted FormField, Button components; typed API layer; RemoteExitNode as presentational component with state lifted to RoutingTab
- Deduplicated firewall rollback/logging, extracted service error infrastructure

### Fixed
- Patch 005: ts-forward chain reordered after UBIOS_FORWARD_JUMP on UBNT devices
- **TKA log spam**: added `--statedir` to tailscaled service
- **Conntrack flush spam** in firewall watcher eliminated
- **Exit node routing stuck after disable**: reversed sync direction so Tailscale is source of truth
- Enable exit node reordered: manifest and ip rules persisted before `EditPrefs` to survive HTTP drop
- Off-by-one in `ubntFindForwardInsertPos` and exit node priority range
- Data races in SetWgS2s/SetWireGuard, DPI logic inversion, cleanup timeout
- WAN IP detection for UDM-SE and UCG-Ultra
- Stale chain prefix read, unsafe TailscaleState Lock/Data/Unlock
- Reconcile mutex race in exit node
- Potential crash when SSE update clears `usingExitNode` while confirmation dialog is open

### Removed
- Dead `Warning` field from `EnableRemoteExitResult`
- Unused `GetAdvertiseExitNodeEnabled()` from `RemoteExitManifest` interface

## [1.3.1] - 2026-03-02

### Fixed
- DNS forwarding toggle reverted in UI after successful save — SSE was not broadcasting the updated AcceptDNS state because no Tailscale pref actually changed (DNS forwarding is managed via Integration API, not Tailscale prefs)

## [1.3.0] - 2026-03-02

### Added
- Tailscale DNS forwarding via Integration API — LAN clients can now resolve Tailscale peer names (*.ts.net) without enabling MagicDNS on the router; creates a dnsmasq forward policy through UniFi DNS settings

## [1.2.3] - 2026-03-02

### Changed
- Tailscale DNS (MagicDNS) toggle permanently disabled in UI with warning — may break router DNS for LAN clients
- Tailscale patches updated to v1.94.2 headers

## [1.2.2] - 2026-03-02

### Fixed
- WG S2S tunnel creation rejected local subnets selected for sharing as "subnet conflict" — local and remote subnets are now separate fields in the data model and validated independently

## [1.2.1] - 2026-03-02

### Changed
- Decomposed god functions: `UpdateTunnel` (116→82 lines), `processNotify` (79→5 line orchestrator), `runStatusRefresh` (53→13 lines), `handleSetSettings` (64→44 lines)
- Extracted `Server` struct sub-components: `integrationCache` and `integrationRetryState` (24→19 fields)
- Extracted shared helpers: `buildPeerConfig`, `buildRouteStatuses`, `checkZBFEnabled`, `swapWanPort`
- Moved UDAPI config parsing out of `internal/wgs2s` package into `manager/` (package boundary fix)
- Replaced `"new"` sentinel zone ID with explicit `createZone` boolean in WG S2S API
- `validateTunnelSubnets` is now a pure function (no longer writes HTTP response directly)
- Standardized `wgManagerOrError` pattern across all WG S2S handlers
- Extracted magic values into named constants (`maxPort`, `mongoPort`, WireGuard params, retry intervals)
- Removed dead guard after `requireIntegration()`, unconditional `UpdatedAt` bump in `Save()`

### Fixed
- TOCTOU race in `updateChecker.check()` — replaced mutex drop/reacquire with `singleflight`
- HTTP client reused across update checks (was creating new client per call)
- `ListenPort` validation now caps at 65535 in both create and update WG S2S handlers
- Enable and delete handlers return 404 for non-existent WG S2S tunnels
- Integration cache invalidated on API key rollback (prevents stale `Configured: true`)
- `rand.Read` error checked in tunnel ID generation (was silently ignored)
- `EnsurePolicies` failure now treated as fatal for zone setup (was warning + continue)
- Preflight key check in `recreateTunnel` runs before teardown (preserves running tunnel on missing key)

## [1.2.0] - 2026-03-02

### Added
- UniFi device info card on dashboard — shows hostname, firmware, UniFi Network version, and system uptime
- System uptime exposed via `/api/device` endpoint (Linux `sysinfo` syscall)
- `formatUptime` utility with tests

### Changed
- Dashboard layout reorganized — UniFi card and WG S2S on the left panel, Tailscale on the right
- TopBar hostname moved inline next to "VPN Pack" title with a vertical separator
- Card heading text vertically centered with icons via `cap-center` CSS utility

### Fixed
- Tailscale card hostname field clarified as "Tailscale Hostname" to distinguish from device hostname

## [1.1.15] - 2026-03-01

### Fixed
- Custom Remote Subnets field in WG S2S tunnel form was taller than other inputs and had smaller placeholder font
- Tailscale DNS toggle now shows a persistent red warning about potential DNS breakage before enabling (previously only visible after toggle)
- Removed unimplemented "remote VPN Pack" hint from tunnel creation and config copy screens

## [1.1.14] - 2026-03-01

### Added
- WG S2S hot-update — endpoint, AllowedIPs, and keepalive changes no longer tear down the tunnel (graceful fallback to recreate on failure)
- AcceptDNS backend guard — rejects MagicDNS enable with HTTP 400 and explanation
- Explicit route metric (100) for WG S2S routes, aligning with UniFi IPSec S2S pattern

### Changed
- Firewall watcher now checks INPUT and OUTPUT rules in addition to FORWARD_IN for WG S2S interfaces
- Dropped firewall requests logged at Warn level instead of Debug
- WAN IP detection uses UDAPI config (`identification.type == "wan"`) instead of hardcoded `eth8`, improving UCG-Ultra portability
- Settings UI shows effective hostname as placeholder when hostname field is empty

### Fixed
- MTU now applied to WG S2S kernel interface via rtnetlink (previously stored but not set)
- Atomic write for `tunnels.json` using temp+rename pattern (prevents data loss on power failure)
- systemd unit allows `/proc/nf_dpi/` writes while keeping `ProtectKernelTunables=yes` (fixes DPI crash on exit node)
- Router public key displayed with copy button in WG S2S tunnel card

## [1.1.13] - 2026-03-01

### Changed
- Diagnostics checks now run concurrently with caching and batched iptables lookups
- Extracted reusable helpers: WAN port, firewall, subnet, settings, typed constants, writeOK, readFileTrimmed
- Ring buffer LogBuffer replaces slice-based log storage; generic doListRequest for paginated APIs
- Merged DERPInfo into single struct; extracted settingsFields shared between API response and state

### Fixed
- TOCTOU race in UDAPI ipset lookup — findIPSet now uses atomic check; HasMarkerRule parsing corrected

## [1.1.12] - 2026-03-01

### Fixed
- Installer and `get.sh` now detect `unifi-native` package on UCG-Ultra devices (previously only checked for `unifi`, causing "UniFi Network Application not found" on native-stack devices)
- Runtime device detection (`detect.go`) falls back to `unifi-native` when `unifi` package is absent

## [1.1.11] - 2026-03-01

### Added
- Zone-aware ipset management — WG S2S tunnel subnets are now added to zone ipsets with cross-zone conflict detection (prevents LAN reclassification when remote subnets overlap)

### Fixed
- `forwardINOk` now correctly enriched in `/api/wg-s2s/tunnels` response (was always `false` due to missing `CheckWgS2sRulesPresent` call)
- `WgS2sTab` frontend component read non-existent field `forwardINRule` instead of `forwardINOk`
- Eliminated UDAPI rule duplication race — post-policy restore now routes through the firewall request channel instead of calling `checkAndRestoreRules` directly from a separate goroutine

## [1.1.10] - 2026-02-28

### Changed
- Enabled Unix socket identity support — `tailscaled` can now identify local callers via peer credentials (removed `ts_omit_unixsocketidentity` build tag)

## [1.1.9] - 2026-02-28

### Fixed
- Integration API zone/policy creation now retries when Zone-Based Firewall is not yet initialized after factory reset (intervals: 0s, 5s, then every 10s)
- Suppressed `VPN_subnets` ipset log spam in UDAPI-only fallback mode (when ZBF zone not yet created)
- Added additional `ts_omit_*` build tags to further reduce binary size

## [1.1.8] - 2026-02-28

### Added
- UniFi Network 10.1+ version gate — installer and daemon refuse to run on older versions
- Factory reset recovery in `get.sh` — reinstalls when systemd units or services are missing but data persists
- Integration resilience — automatic zone-based firewall (ZBF) detection, CSRF token refresh on rotation, recovery after factory reset

### Changed
- `get.sh` checks Network version before downloading the archive (saves bandwidth on unsupported versions)
- Daemon exits with code 78 (EX_CONFIG) on version mismatch; systemd won't auto-restart (`RestartPreventExitStatus=78`)

## [1.1.7] - 2026-02-26

### Changed
- Restored `GOARM64=v8.0,crypto` — all supported UniFi devices have ARMv8 crypto extensions

## [1.1.6] - 2026-02-26

### Changed
- Removed `GOARM64=v8.0,crypto` build hint — Go runtime detects hardware crypto at startup, the compile-time hint is unnecessary and may cause issues on devices without ARMv8 crypto extensions

## [1.1.5] - 2026-02-26

### Changed
- Tailscale updated from 1.94.1 to 1.94.2
- 17 additional `ts_omit_*` build tags added (total 29), further reducing binary size by excluding unused features (ACME, DBus, NetworkManager, TPM, etc.)
- Version stamp now includes vpn-pack version and git commit hash for traceability (e.g. `1.94.2-vpnpack1.1.5-g99ab894`)

## [1.1.4] - 2026-02-26

### Changed
- ARM64 build targets hardware crypto extensions (`GOARM64=v8.0,crypto`) for Cortex-A57 and IPQ5322 CPUs
- Binaries stripped of debug info and symbol tables (`-s -w`), reducing total size from 78 MB to 45 MB
- Unused Tailscale components excluded via `ts_omit_*` build tags (AWS, Kubernetes, Synology, Drive, Serve, etc.)

## [1.1.3] - 2026-02-26

### Fixed
- Manifest race condition — concurrent map access from HTTP handlers and background goroutines could cause runtime panic; protected with `sync.RWMutex` and thread-safe accessors
- Firewall rules now restored after WAN port policy changes to prevent UDAPI interface bindings from being lost

### Changed
- All JSON request body parsing wrapped with `MaxBytesReader` (64 KB limit) to reject oversized payloads
- systemd service hardened with `NoNewPrivileges`, `ProtectHome`, `PrivateTmp`, `ProtectKernelTunables`, `ProtectControlGroups`, `RestrictSUIDSGID`, `MemoryDenyWriteExecute`
- WG S2S tunnel updates run preflight checks (private key, endpoint resolution, port availability) before tearing down the active tunnel

### Removed
- Dead CSRF token code from frontend — authentication is handled by UniFi nginx layer

## [1.1.2] - 2026-02-26

### Added
- Hash-based URL routing — UI preserves current page on refresh (F5), supports browser Back/Forward and deep links
- Real-time settings sync via SSE — CLI changes (`tailscale set`) instantly reflected in UI without polling

### Fixed
- SSE auth keepalive prevents session expiry during long idle periods on SSE connections

## [1.1.1] - 2026-02-26

### Fixed
- Exit node status was always reported as off in SSE/UI (checked wrong Tailscale pref field)
- DPI fingerprinting now auto-disabled when exit node is active to prevent dpi-flow-stats crash on UniFi devices (TUN interface lacks MAC addresses, causing OUI lookup error loop)

### Added
- DPI fingerprint monitoring with 5s enforcement — re-disables if system resets the value
- UI warning in Routing tab when DPI fingerprinting is disabled

## [1.1.0] - 2026-02-25

### Added
- Device Tags (advertise-tags) setting in Peer Relay section — set ACL tags like `tag:relay` directly from the UI without SSH
- Tag format validation (backend and frontend) matching Tailscale rules
- Warning about device ownership change when adding tags

## [1.0.3] - 2026-02-25

### Fixed
- Login/auth-key flows now fail early if MagicDNS cannot be disabled, preventing DNS breakage on router
- Manifest saves use atomic write (temp file + rename) to prevent corruption on crash
- Logs tab polling replaced with proper async loop to fix race condition on unmount
- Clipboard copy errors now caught and surfaced in UI instead of silently failing
- SSE error deduplication (5s window) and cap at 50 entries to prevent memory leak
- Install command in README corrected to `get.sh`

### Added
- WireGuard S2S tunnel update validation (ports, CIDRs, base64 keys)
- API client request timeouts (30s default, 60s for diagnostics) with AbortController
- Login flow retry button on failure with 3s delay
- IPv6 bracket notation support in endpoint validation
- Reusable ApiKeyForm component (extracted from Settings and Setup)
- Semantic version comparison for update checker
- Tests for manifest atomicity, WG S2S validation, version comparison, firewall watcher

### Changed
- UCG Ultra marked as "Tested" in README (previously "Supported")

## [1.0.2] - 2026-02-25

### Fixed
- Tailscale DNS (MagicDNS) no longer overwrites router's resolv.conf on first activation — `CorpDNS` is now set to `false` by default in all activation paths (interactive login, auth key)

### Added
- "Tailscale DNS" toggle in Settings → Advanced with a warning about consequences of enabling DNS takeover on a router

## [1.0.0] - 2026-02-25

### Added
- Tailscale daemon and CLI for UniFi Cloud Gateway devices (based on Tailscale 1.94.1)
- Web UI manager accessible at `/vpn-pack/` (behind UniFi auth)
- UniFi firewall integration via Integration API v1 and UDAPI
- WireGuard site-to-site tunnel management
- Subnet routing configuration
- Exit node support
- One-line installer: `curl -fsSL https://raw.githubusercontent.com/eds-ch/vpn-pack/main/install.sh | sh`
- Automatic update notifications in Web UI
- Custom fwmark patch to avoid conflict with UniFi VPN clients
- Support for UDM-SE, UDM-Pro, UDM-Pro-Max, UDM, UCG-Ultra, UDR-SE

[Unreleased]: https://github.com/eds-ch/vpn-pack/compare/v1.5.2-beta.8...HEAD
[1.5.2-beta.8]: https://github.com/eds-ch/vpn-pack/compare/v1.5.2-beta.7...v1.5.2-beta.8
[1.5.2-beta.7]: https://github.com/eds-ch/vpn-pack/compare/v1.5.1...v1.5.2-beta.7
[1.5.0]: https://github.com/eds-ch/vpn-pack/compare/v1.4.2...v1.5.0
[1.4.2]: https://github.com/eds-ch/vpn-pack/compare/v1.4.1...v1.4.2
[1.4.1]: https://github.com/eds-ch/vpn-pack/compare/v1.4.0...v1.4.1
[1.4.0]: https://github.com/eds-ch/vpn-pack/compare/v1.3.1...v1.4.0
[1.3.1]: https://github.com/eds-ch/vpn-pack/compare/v1.3.0...v1.3.1
[1.3.0]: https://github.com/eds-ch/vpn-pack/compare/v1.2.3...v1.3.0
[1.2.3]: https://github.com/eds-ch/vpn-pack/compare/v1.2.2...v1.2.3
[1.2.2]: https://github.com/eds-ch/vpn-pack/compare/v1.2.1...v1.2.2
[1.2.1]: https://github.com/eds-ch/vpn-pack/compare/v1.2.0...v1.2.1
[1.2.0]: https://github.com/eds-ch/vpn-pack/compare/v1.1.15...v1.2.0
[1.1.15]: https://github.com/eds-ch/vpn-pack/compare/v1.1.14...v1.1.15
[1.1.14]: https://github.com/eds-ch/vpn-pack/compare/v1.1.13...v1.1.14
[1.1.13]: https://github.com/eds-ch/vpn-pack/compare/v1.1.12...v1.1.13
[1.1.12]: https://github.com/eds-ch/vpn-pack/compare/v1.1.11...v1.1.12
[1.1.11]: https://github.com/eds-ch/vpn-pack/compare/v1.1.10...v1.1.11
[1.1.10]: https://github.com/eds-ch/vpn-pack/compare/v1.1.9...v1.1.10
[1.1.9]: https://github.com/eds-ch/vpn-pack/compare/v1.1.8...v1.1.9
[1.1.8]: https://github.com/eds-ch/vpn-pack/compare/v1.1.7...v1.1.8
[1.1.7]: https://github.com/eds-ch/vpn-pack/compare/v1.1.6...v1.1.7
[1.1.6]: https://github.com/eds-ch/vpn-pack/compare/v1.1.5...v1.1.6
[1.1.5]: https://github.com/eds-ch/vpn-pack/compare/v1.1.4...v1.1.5
[1.1.4]: https://github.com/eds-ch/vpn-pack/compare/v1.1.3...v1.1.4
[1.1.3]: https://github.com/eds-ch/vpn-pack/compare/v1.1.2...v1.1.3
[1.1.2]: https://github.com/eds-ch/vpn-pack/compare/v1.1.1...v1.1.2
[1.1.1]: https://github.com/eds-ch/vpn-pack/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/eds-ch/vpn-pack/compare/v1.0.3...v1.1.0
[1.0.3]: https://github.com/eds-ch/vpn-pack/compare/v1.0.2...v1.0.3
[1.0.2]: https://github.com/eds-ch/vpn-pack/compare/v1.0.0...v1.0.2
[1.0.0]: https://github.com/eds-ch/vpn-pack/releases/tag/v1.0.0
