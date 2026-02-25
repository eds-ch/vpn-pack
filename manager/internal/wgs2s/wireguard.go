package wgs2s

import (
	"fmt"
	"net"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type peerConfig struct {
	PublicKey           string
	Endpoint            string
	AllowedIPs          []string
	PersistentKeepalive int
}

func configureDevice(client *wgctrl.Client, name string, privKey wgtypes.Key, port int, peer *peerConfig) error {
	cfg := wgtypes.Config{
		PrivateKey:   &privKey,
		ListenPort:   &port,
		ReplacePeers: true,
	}

	if peer != nil && peer.PublicKey != "" {
		peerKey, err := wgtypes.ParseKey(peer.PublicKey)
		if err != nil {
			return fmt.Errorf("parse peer public key: %w", err)
		}

		pc := wgtypes.PeerConfig{
			PublicKey:         peerKey,
			ReplaceAllowedIPs: true,
		}

		if peer.Endpoint != "" {
			endpoint, err := net.ResolveUDPAddr("udp", peer.Endpoint)
			if err != nil {
				return fmt.Errorf("resolve peer endpoint %s: %w", peer.Endpoint, err)
			}
			pc.Endpoint = endpoint
		}

		for _, cidr := range peer.AllowedIPs {
			_, ipNet, err := net.ParseCIDR(cidr)
			if err != nil {
				return fmt.Errorf("parse allowed IP %s: %w", cidr, err)
			}
			pc.AllowedIPs = append(pc.AllowedIPs, *ipNet)
		}

		if peer.PersistentKeepalive > 0 {
			d := time.Duration(peer.PersistentKeepalive) * time.Second
			pc.PersistentKeepaliveInterval = &d
		}

		cfg.Peers = []wgtypes.PeerConfig{pc}
	}

	if err := client.ConfigureDevice(name, cfg); err != nil {
		return fmt.Errorf("configure device %s: %w", name, err)
	}
	return nil
}
