# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/eds-ch/vpn-pack/compare/v1.1.0...HEAD
[1.1.0]: https://github.com/eds-ch/vpn-pack/compare/v1.0.3...v1.1.0
[1.0.3]: https://github.com/eds-ch/vpn-pack/compare/v1.0.2...v1.0.3
[1.0.2]: https://github.com/eds-ch/vpn-pack/compare/v1.0.0...v1.0.2
[1.0.0]: https://github.com/eds-ch/vpn-pack/releases/tag/v1.0.0
