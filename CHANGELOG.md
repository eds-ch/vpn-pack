# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/eds-ch/vpn-pack/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/eds-ch/vpn-pack/releases/tag/v1.0.0
