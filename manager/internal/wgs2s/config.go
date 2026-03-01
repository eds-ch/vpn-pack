package wgs2s

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

type TunnelConfig struct {
	ID                  string    `json:"id"`
	Name                string    `json:"name"`
	InterfaceName       string    `json:"interfaceName"`
	ListenPort          int       `json:"listenPort"`
	TunnelAddress       string    `json:"tunnelAddress"`
	PeerPublicKey       string    `json:"peerPublicKey"`
	PeerEndpoint        string    `json:"peerEndpoint"`
	AllowedIPs          []string  `json:"allowedIPs"`
	PersistentKeepalive int       `json:"persistentKeepalive"`
	MTU                 int       `json:"mtu"`
	Enabled             bool      `json:"enabled"`
	CreatedAt           time.Time `json:"createdAt"`
}

type TunnelsConfig struct {
	Tunnels []TunnelConfig `json:"tunnels"`
	Version int            `json:"version"`
}

func loadConfig(path string) (*TunnelsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &TunnelsConfig{Version: 1}, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg TunnelsConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func saveConfig(path string, cfg *TunnelsConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return fmt.Errorf("write config tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}

func nextInterfaceName(existing []TunnelConfig) string {
	used := make(map[string]bool, len(existing))
	for _, t := range existing {
		used[t.InterfaceName] = true
	}
	for i := 0; ; i++ {
		name := fmt.Sprintf("wg-s2s%d", i)
		if !used[name] {
			return name
		}
	}
}

func generateID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}
