# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/eds-ch/vpn-pack/compare/v1.1.9...HEAD
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
