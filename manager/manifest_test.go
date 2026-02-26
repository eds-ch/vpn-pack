package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadManifest(t *testing.T) {
	t.Run("file not found returns v2", func(t *testing.T) {
		dir := t.TempDir()
		m, err := LoadManifest(filepath.Join(dir, "manifest.json"))
		require.NoError(t, err)
		assert.Equal(t, 2, m.Version)
	})

	t.Run("valid v2 JSON", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "manifest.json")
		data := `{"version":2,"siteId":"site1","tailscale":{"zoneId":"z1","chainPrefix":"TS"}}`
		require.NoError(t, os.WriteFile(path, []byte(data), 0600))

		m, err := LoadManifest(path)
		require.NoError(t, err)
		assert.Equal(t, 2, m.Version)
		assert.Equal(t, "site1", m.SiteID)
		assert.Equal(t, "z1", m.Tailscale.ZoneID)
		assert.Equal(t, "TS", m.Tailscale.ChainPrefix)
	})

	t.Run("v1 JSON migrated to v2", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "manifest.json")
		v1 := map[string]any{
			"version":      1,
			"firewallMode": "integration",
			"modeB": map[string]any{
				"zoneID":    "zone-abc",
				"policyIDs": []string{"p1", "p2"},
			},
		}
		data, _ := json.Marshal(v1)
		require.NoError(t, os.WriteFile(path, data, 0600))

		m, err := LoadManifest(path)
		require.NoError(t, err)
		assert.Equal(t, 2, m.Version)
		assert.Equal(t, "zone-abc", m.Tailscale.ZoneID)
		assert.Equal(t, []string{"p1", "p2"}, m.Tailscale.PolicyIDs)
		assert.Equal(t, "VPN", m.Tailscale.ChainPrefix)
	})

	t.Run("corrupt JSON returns error", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "manifest.json")
		require.NoError(t, os.WriteFile(path, []byte("{invalid"), 0600))

		_, err := LoadManifest(path)
		assert.Error(t, err)
	})
}

func TestManifestSaveRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m, err := LoadManifest(path)
	require.NoError(t, err)

	m.SiteID = "my-site"
	m.SetTailscaleZone("z1", []string{"p1"}, "TS")
	require.NoError(t, m.Save())

	m2, err := LoadManifest(path)
	require.NoError(t, err)
	assert.Equal(t, "my-site", m2.SiteID)
	assert.Equal(t, "z1", m2.Tailscale.ZoneID)
	assert.Equal(t, []string{"p1"}, m2.Tailscale.PolicyIDs)
	assert.Equal(t, "TS", m2.Tailscale.ChainPrefix)
}

func TestGetWgS2sZones(t *testing.T) {
	t.Run("empty map", func(t *testing.T) {
		m := &Manifest{}
		zones := m.GetWgS2sZones()
		assert.Empty(t, zones)
	})

	t.Run("2 tunnels same zone", func(t *testing.T) {
		m := &Manifest{
			WgS2s: map[string]ZoneManifest{
				"t1": {ZoneID: "z1", ZoneName: "Zone One"},
				"t2": {ZoneID: "z1", ZoneName: "Zone One"},
			},
		}
		zones := m.GetWgS2sZones()
		require.Len(t, zones, 1)
		assert.Equal(t, "z1", zones[0].ZoneID)
		assert.Equal(t, 2, zones[0].TunnelCount)
	})

	t.Run("2 different zones", func(t *testing.T) {
		m := &Manifest{
			WgS2s: map[string]ZoneManifest{
				"t1": {ZoneID: "z1", ZoneName: "Zone One"},
				"t2": {ZoneID: "z2", ZoneName: "Zone Two"},
			},
		}
		zones := m.GetWgS2sZones()
		assert.Len(t, zones, 2)
	})
}

func TestGetTailscaleChainPrefix(t *testing.T) {
	t.Run("empty returns VPN", func(t *testing.T) {
		m := &Manifest{}
		assert.Equal(t, "VPN", m.GetTailscaleChainPrefix())
	})

	t.Run("custom prefix", func(t *testing.T) {
		m := &Manifest{Tailscale: ZoneManifest{ChainPrefix: "CUSTOM"}}
		assert.Equal(t, "CUSTOM", m.GetTailscaleChainPrefix())
	})
}

func TestWanPortCycle(t *testing.T) {
	m := &Manifest{}

	m.SetWanPort("test-marker", "pol-1", "Test Policy", 8080)
	assert.Equal(t, "pol-1", m.GetWanPortPolicyID("test-marker"))

	m.RemoveWanPort("test-marker")
	assert.Equal(t, "", m.GetWanPortPolicyID("test-marker"))
}

func TestManifestSave_ReturnsError(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		dir := t.TempDir()
		m := &Manifest{path: filepath.Join(dir, "manifest.json"), Version: 2}
		require.NoError(t, m.Save())
		data, err := os.ReadFile(m.path)
		require.NoError(t, err)
		var loaded Manifest
		require.NoError(t, json.Unmarshal(data, &loaded))
		assert.Equal(t, 2, loaded.Version)
	})

	t.Run("unwritable path", func(t *testing.T) {
		m := &Manifest{path: "/proc/nonexistent/manifest.json", Version: 2}
		err := m.Save()
		assert.Error(t, err)
	})
}

func TestManifestSave_Atomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")

	m := &Manifest{path: path, Version: 2, SiteID: "initial"}
	require.NoError(t, m.Save())

	m.SiteID = "updated"
	require.NoError(t, m.Save())

	_, err := os.Stat(path + ".tmp")
	assert.True(t, os.IsNotExist(err))

	m2, err := LoadManifest(path)
	require.NoError(t, err)
	assert.Equal(t, "updated", m2.SiteID)
}

func TestManifest_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	m, err := LoadManifest(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	var wg sync.WaitGroup
	const goroutines = 10
	const iterations = 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				tunnelID := fmt.Sprintf("tunnel-%d-%d", id, j)
				m.SetWgS2sZone(tunnelID, "zone-1", "Zone One", []string{"p1"}, "VPN")
			}
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = m.GetWgS2sZones()
				_, _ = m.GetWgS2sZone("tunnel-0-0")
				_ = m.GetWgS2sSnapshot()
			}
		}()
	}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			marker := fmt.Sprintf("marker-%d", id)
			for j := 0; j < iterations; j++ {
				m.SetWanPort(marker, "pol-1", "Test", 8080+id)
				_ = m.GetWanPortPolicyID(marker)
				_, _ = m.GetWanPortEntry(marker)
				m.RemoveWanPort(marker)
			}
		}(i)
	}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				m.SetSiteID(fmt.Sprintf("site-%d", id))
				_ = m.GetSiteID()
				_ = m.HasSiteID()
			}
		}(i)
	}

	for i := 0; i < goroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations/10; j++ {
				_ = m.Save()
			}
		}()
	}

	wg.Wait()
}

func TestManifest_SnapshotIsolation(t *testing.T) {
	m := &Manifest{
		WanPorts: map[string]WanPortEntry{
			"a": {PolicyID: "p1", Port: 80},
			"b": {PolicyID: "p2", Port: 443},
		},
	}

	snap := m.GetWanPortsSnapshot()
	m.RemoveWanPort("a")

	assert.Contains(t, snap, "a")
	assert.Equal(t, "", m.GetWanPortPolicyID("a"))
}
