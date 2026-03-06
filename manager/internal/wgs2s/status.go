package wgs2s

import (
	"log/slog"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"

	"unifi-tailscale/manager/domain"
)

type WgS2sStatus = domain.WgS2sStatus

func getAllStatuses(wgClient *wgctrl.Client, tunnels []TunnelConfig, log *slog.Logger) []WgS2sStatus {
	statuses := make([]WgS2sStatus, 0, len(tunnels))

	for _, t := range tunnels {
		st := WgS2sStatus{
			ID:            t.ID,
			Name:          t.Name,
			InterfaceName: t.InterfaceName,
			Enabled:       t.Enabled,
			ListenPort:    t.ListenPort,
			LocalAddress:  t.TunnelAddress,
			RemoteSubnets: t.AllowedIPs,
		}

		if !t.Enabled {
			statuses = append(statuses, st)
			continue
		}

		dev, err := wgClient.Device(t.InterfaceName)
		if err != nil {
			log.Debug("wgctrl device query failed", "iface", t.InterfaceName, "err", err)
			statuses = append(statuses, st)
			continue
		}

		st.ListenPort = dev.ListenPort
		if len(dev.Peers) > 0 {
			p := dev.Peers[0]
			st.LastHandshake = p.LastHandshakeTime
			st.TransferRx = p.ReceiveBytes
			st.TransferTx = p.TransmitBytes
			st.Connected = !p.LastHandshakeTime.IsZero() && time.Since(p.LastHandshakeTime) < handshakeTimeout
			if p.Endpoint != nil {
				st.Endpoint = p.Endpoint.String()
			}
		}

		statuses = append(statuses, st)
	}

	return statuses
}
