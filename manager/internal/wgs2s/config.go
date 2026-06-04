package wgs2s

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"

	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/state"
)

type TunnelConfig = domain.TunnelConfig

type TunnelsConfig struct {
	Tunnels []TunnelConfig `json:"tunnels"`
	Version int            `json:"version"`
}

func loadConfig(path string) (*TunnelsConfig, error) {
	cfg, recovered, err := state.LoadJSON[TunnelsConfig](path, TunnelsConfig{Version: 1})
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	if recovered {
		slog.Warn("tunnels.json corrupted; loaded empty config and quarantined original", "path", path)
	}
	return &cfg, nil
}

func saveConfig(path string, cfg *TunnelsConfig) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := state.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("save config: %w", err)
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

func generateID() (string, error) {
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand failed: %w", err)
	}
	return fmt.Sprintf("%x", b), nil
}
