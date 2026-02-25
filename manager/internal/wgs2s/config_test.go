package wgs2s

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	t.Run("not found returns fresh", func(t *testing.T) {
		dir := t.TempDir()
		cfg, err := loadConfig(filepath.Join(dir, "tunnels.json"))
		require.NoError(t, err)
		assert.Equal(t, 1, cfg.Version)
		assert.Empty(t, cfg.Tunnels)
	})

	t.Run("valid JSON", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "tunnels.json")
		data, _ := json.Marshal(TunnelsConfig{
			Version: 1,
			Tunnels: []TunnelConfig{
				{ID: "abc", Name: "test", ListenPort: 51820},
			},
		})
		require.NoError(t, os.WriteFile(path, data, 0600))

		cfg, err := loadConfig(path)
		require.NoError(t, err)
		require.Len(t, cfg.Tunnels, 1)
		assert.Equal(t, "abc", cfg.Tunnels[0].ID)
		assert.Equal(t, 51820, cfg.Tunnels[0].ListenPort)
	})

	t.Run("corrupt JSON returns error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "tunnels.json")
		require.NoError(t, os.WriteFile(path, []byte("{bad"), 0600))

		_, err := loadConfig(path)
		assert.Error(t, err)
	})
}

func TestSaveConfigRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tunnels.json")

	created := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	original := &TunnelsConfig{
		Version: 1,
		Tunnels: []TunnelConfig{
			{
				ID:                  "t1",
				Name:                "tunnel-one",
				InterfaceName:       "wg-s2s0",
				ListenPort:          51820,
				TunnelAddress:       "10.0.0.1/24",
				PeerPublicKey:       "cGVlcmtleQ==",
				PeerEndpoint:        "1.2.3.4:51820",
				AllowedIPs:          []string{"10.0.0.0/24", "192.168.1.0/24"},
				PersistentKeepalive: 25,
				MTU:                 1420,
				Enabled:             true,
				CreatedAt:           created,
			},
			{ID: "t2", Name: "tunnel-two", ListenPort: 51821, Enabled: false},
		},
	}

	require.NoError(t, saveConfig(path, original))

	loaded, err := loadConfig(path)
	require.NoError(t, err)
	require.Len(t, loaded.Tunnels, 2)

	got := loaded.Tunnels[0]
	assert.Equal(t, "t1", got.ID)
	assert.Equal(t, "tunnel-one", got.Name)
	assert.Equal(t, "wg-s2s0", got.InterfaceName)
	assert.Equal(t, 51820, got.ListenPort)
	assert.Equal(t, "10.0.0.1/24", got.TunnelAddress)
	assert.Equal(t, "cGVlcmtleQ==", got.PeerPublicKey)
	assert.Equal(t, "1.2.3.4:51820", got.PeerEndpoint)
	assert.Equal(t, []string{"10.0.0.0/24", "192.168.1.0/24"}, got.AllowedIPs)
	assert.Equal(t, 25, got.PersistentKeepalive)
	assert.Equal(t, 1420, got.MTU)
	assert.Equal(t, true, got.Enabled)
	assert.Equal(t, created, got.CreatedAt)

	assert.Equal(t, "tunnel-two", loaded.Tunnels[1].Name)
	assert.Equal(t, false, loaded.Tunnels[1].Enabled)
}

func TestNextInterfaceName(t *testing.T) {
	tests := []struct {
		name     string
		existing []TunnelConfig
		want     string
	}{
		{"empty", nil, "wg-s2s0"},
		{"wg-s2s0 taken", []TunnelConfig{{InterfaceName: "wg-s2s0"}}, "wg-s2s1"},
		{"gap at 1", []TunnelConfig{
			{InterfaceName: "wg-s2s0"},
			{InterfaceName: "wg-s2s2"},
		}, "wg-s2s1"},
		{"0 through 4 taken", []TunnelConfig{
			{InterfaceName: "wg-s2s0"},
			{InterfaceName: "wg-s2s1"},
			{InterfaceName: "wg-s2s2"},
			{InterfaceName: "wg-s2s3"},
			{InterfaceName: "wg-s2s4"},
		}, "wg-s2s5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nextInterfaceName(tt.existing))
		})
	}
}
