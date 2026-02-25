# VPN-pack

Tailscale and WireGuard Site-to-Site VPN for UniFi Cloud Gateway devices (UDM SE, UDM Pro, UCG Ultra, UDR SE). Survives firmware updates, integrates with UniFi firewall zones, and ships with a web UI for management.

## Features

- **Tailscale daemon** that persists across firmware updates and reboots
- **Web UI** at `https://<gateway-ip>/vpn-pack/` — protected by UniFi controller auth
- **Subnet routing** — advertise local networks to your tailnet
- **Exit node** — route all traffic from other Tailscale devices through your gateway
- **WireGuard site-to-site tunnels** — encrypted point-to-point links between networks, managed from the UI
- **UniFi firewall integration** — automatic zone and policy creation visible in the UniFi Network UI

## Supported Devices

| Device | Status |
|--------|--------|
| UDM SE | Tested |
| UDM Pro / UDM Pro Max | Supported |
| UDM | Supported |
| UCG Ultra | Supported |
| UDR SE | Supported |

Requirements: UniFi OS with controller (unifi-core), aarch64, systemd, `/dev/net/tun`.

## Install

[Enable SSH](https://help.ui.com/hc/en-us/articles/204909374-Connecting-to-UniFi-with-Debug-Tools-SSH) on your gateway (Settings → Control Plane → Console → SSH), then run:

```bash
curl -fsSL https://raw.githubusercontent.com/eds-ch/vpn-pack/main/get.sh | bash
```

The script checks for an existing installation, downloads the latest release from GitHub, verifies the SHA256 checksum, and runs the installer. On upgrade, auth state and config are preserved.

After installation:

1. Log in to your gateway at `https://<gateway-ip>` (establishes a UniFi auth session)
2. Create an Integration API key at Settings → API — copy it
3. Open `https://<gateway-ip>/vpn-pack/` — the setup screen will ask for the API key
4. After the key is validated, Tailscale auth appears — log in via browser link / QR code or paste an auth key

## What Gets Installed

```
/persistent/vpn-pack/
├── bin/              # tailscale, tailscaled, vpn-pack-manager
├── state/            # auth keys (survives upgrades)
└── config/           # settings, firewall manifest, WG S2S tunnels
```

Two systemd services: `tailscaled` (Tailscale daemon) and `vpn-pack-manager` (web UI). An nginx config proxies the UI to `/vpn-pack/`.

Everything under `/persistent/` survives firmware updates and factory resets.

## Web UI

The management interface runs behind the UniFi controller's authentication — no extra passwords needed.

**Dashboard** — connection status, Tailscale IP, peers with latency, DERP relay info, WG S2S tunnel status.

**Settings** — hostname, UDP port, relay server mode, custom control URL, subnet routes, exit node toggle, UniFi Integration API key.

**WireGuard S2S** — create and manage site-to-site tunnels with full lifecycle: create, configure peers, enable/disable, monitor traffic, export configs.

**Logs** — real-time structured logs from Tailscale daemon and manager.

All changes are pushed to the browser in real-time via Server-Sent Events.

## UniFi Firewall Integration

When you provide a UniFi Network API key in the Settings, vpn-pack:

1. Creates a dedicated "Tailscale" firewall zone
2. Sets up inbound/outbound traffic policies
3. Opens the WireGuard port on WAN
4. Assigns each S2S tunnel its own firewall zone

These zones and policies appear in the UniFi Network UI and persist across reboots. 

## Uninstall

```bash
/persistent/vpn-pack/uninstall.sh
```

Stops services, cleans up firewall rules, removes binaries and systemd units. Auth state and config are preserved by default (so a reinstall reconnects automatically).

Full removal including all state:

```bash
/persistent/vpn-pack/uninstall.sh --purge
```

## Build from Source

Requires Go 1.26+, Node.js 18+, Make.

```bash
make build                      # fetch Tailscale source, apply patches, cross-compile for ARM64
make package                    # create vpn-pack-<version>.tar.gz
make deploy HOST=<gateway-ip>   # deploy via SSH
```

The build applies four patches to upstream Tailscale v1.94.1 to avoid fwmark conflicts with UniFi VPN clients and to report correct device/platform info. See `patches/README.md` for details.

## How It Works

Tailscale runs in userspace via `/dev/net/tun` — no kernel modules needed. It uses its own iptables chains (`ts-*`) and fwmark bits that don't overlap with UniFi's (`UBIOS_*`). DNS resolution is left to UniFi (`--accept-dns=false`) to avoid conflicts.

The manager is a single Go binary with the Svelte UI embedded. It talks to tailscaled via the local Unix socket and to UniFi via the Integration API and UDAPI socket.

## WireGuard Site-to-Site

UniFi has Site Magic — SD-WAN built on WireGuard + OSPF, automatic mesh between sites. But Site Magic only works between UniFi devices under the same UI Account. UniFi also has built-in WireGuard Server and Client, but these are designed for remote access (phones/laptops connecting in), not site-to-site. The common workaround — WG Server on site A, WG Client on site B — requires manual route configuration, suffers from one-directional routing (server can't reach client subnets without AllowedIPs hacks), and has no firewall zone integration.

For connecting to non-UniFi peers (MikroTik, pfSense, Linux servers, cloud VPCs) the only native option is IPSec (200–400 Mbps). WG S2S fills that gap: true bidirectional site-to-site tunnels to any WireGuard peer using **kernel `wireguard.ko`** (800+ Mbps), with automatic route setup, firewall zone integration, and full lifecycle management from the web UI.

## License

MIT — see [LICENSE](LICENSE).

## Acknowledgments

Built on [Tailscale](https://tailscale.com/) (BSD-3-Clause, see [LICENSES/tailscale.txt](LICENSES/tailscale.txt)). This project is not affiliated with or endorsed by Tailscale Inc.

"[WireGuard](https://www.wireguard.com/)" is a registered trademark of Jason A. Donenfeld.
