package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type Manifest struct {
	path           string                   `json:"-"`
	Version        int                      `json:"version"`
	CreatedAt      time.Time                `json:"createdAt"`
	UpdatedAt      time.Time                `json:"updatedAt"`
	SiteID         string                   `json:"siteId,omitempty"`
	Tailscale      ZoneManifest             `json:"tailscale"`
	WgS2s          map[string]ZoneManifest  `json:"wgS2s,omitempty"`
	WanPorts       map[string]WanPortEntry  `json:"wanPorts,omitempty"`
	ExternalZoneID string                   `json:"externalZoneId,omitempty"`
	GatewayZoneID  string                   `json:"gatewayZoneId,omitempty"`
}

type WanPortEntry struct {
	PolicyID   string `json:"policyId"`
	PolicyName string `json:"policyName"`
	Port       int    `json:"port"`
}

type ZoneManifest struct {
	ZoneID      string   `json:"zoneId,omitempty"`
	ZoneName    string   `json:"zoneName,omitempty"`
	PolicyIDs   []string `json:"policyIds,omitempty"`
	ChainPrefix string   `json:"chainPrefix,omitempty"`
}

type WgS2sZoneInfo struct {
	ZoneID      string `json:"zoneId"`
	ZoneName    string `json:"zoneName"`
	TunnelCount int    `json:"tunnelCount"`
}

func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Manifest{
				path:      path,
				Version:   2,
				CreatedAt: time.Now().UTC(),
			}, nil
		}
		return nil, err
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	var version int
	if v, ok := raw["version"]; ok {
		if err := json.Unmarshal(v, &version); err != nil {
			return nil, fmt.Errorf("parse manifest version: %w", err)
		}
	}

	if version <= 1 {
		m, err := migrateV1(data)
		if err != nil {
			return nil, err
		}
		m.path = path
		return m, nil
	}

	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	m.path = path
	return &m, nil
}

type manifestV1 struct {
	Version      int       `json:"version"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	FirewallMode string    `json:"firewallMode"`
	ModeB        struct {
		ZoneID    string   `json:"zoneID"`
		PolicyIDs []string `json:"policyIDs"`
	} `json:"modeB"`
}

func migrateV1(data []byte) (*Manifest, error) {
	var v1 manifestV1
	if err := json.Unmarshal(data, &v1); err != nil {
		return &Manifest{Version: 2, CreatedAt: time.Now().UTC()}, nil
	}

	m := &Manifest{
		Version:   2,
		CreatedAt: v1.CreatedAt,
		UpdatedAt: v1.UpdatedAt,
		Tailscale: ZoneManifest{
			ChainPrefix: "VPN",
		},
	}

	if v1.ModeB.ZoneID != "" {
		m.Tailscale.ZoneID = v1.ModeB.ZoneID
		m.Tailscale.PolicyIDs = v1.ModeB.PolicyIDs
	}

	return m, nil
}

func (m *Manifest) Save() {
	m.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		slog.Warn("manifest marshal failed", "err", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(m.path), dirPerm); err != nil {
		slog.Warn("manifest dir create failed", "err", err)
		return
	}
	if err := os.WriteFile(m.path, data, secretPerm); err != nil {
		slog.Warn("manifest save failed", "err", err)
	}
}

func (m *Manifest) SetTailscaleZone(zoneID string, policyIDs []string, chainPrefix string) {
	m.Tailscale = ZoneManifest{
		ZoneID:      zoneID,
		PolicyIDs:   policyIDs,
		ChainPrefix: chainPrefix,
	}
	m.UpdatedAt = time.Now().UTC()
}

func (m *Manifest) SetWgS2sZone(tunnelID, zoneID, zoneName string, policyIDs []string, chainPrefix string) {
	if m.WgS2s == nil {
		m.WgS2s = make(map[string]ZoneManifest)
	}
	m.WgS2s[tunnelID] = ZoneManifest{
		ZoneID:      zoneID,
		ZoneName:    zoneName,
		PolicyIDs:   policyIDs,
		ChainPrefix: chainPrefix,
	}
	m.UpdatedAt = time.Now().UTC()
}

func (m *Manifest) RemoveWgS2sTunnel(tunnelID string) {
	delete(m.WgS2s, tunnelID)
	m.UpdatedAt = time.Now().UTC()
}

func (m *Manifest) GetWgS2sZones() []WgS2sZoneInfo {
	seen := make(map[string]*WgS2sZoneInfo)
	var order []string
	for _, zm := range m.WgS2s {
		if zm.ZoneID == "" {
			continue
		}
		if info, ok := seen[zm.ZoneID]; ok {
			info.TunnelCount++
		} else {
			seen[zm.ZoneID] = &WgS2sZoneInfo{
				ZoneID:      zm.ZoneID,
				ZoneName:    zm.ZoneName,
				TunnelCount: 1,
			}
			order = append(order, zm.ZoneID)
		}
	}
	result := make([]WgS2sZoneInfo, 0, len(order))
	for _, id := range order {
		result = append(result, *seen[id])
	}
	return result
}

func (m *Manifest) GetTailscaleChainPrefix() string {
	if m.Tailscale.ChainPrefix != "" {
		return m.Tailscale.ChainPrefix
	}
	return "VPN"
}

func (m *Manifest) GetWgS2sChainPrefix(tunnelID string) string {
	if zm, ok := m.WgS2s[tunnelID]; ok && zm.ChainPrefix != "" {
		return zm.ChainPrefix
	}
	return "VPN"
}

func (m *Manifest) SetWanPort(marker, policyID, policyName string, port int) {
	if m.WanPorts == nil {
		m.WanPorts = make(map[string]WanPortEntry)
	}
	m.WanPorts[marker] = WanPortEntry{PolicyID: policyID, PolicyName: policyName, Port: port}
	m.UpdatedAt = time.Now().UTC()
}

func (m *Manifest) RemoveWanPort(marker string) {
	delete(m.WanPorts, marker)
	m.UpdatedAt = time.Now().UTC()
}

func (m *Manifest) GetWanPortPolicyID(marker string) string {
	if m.WanPorts == nil {
		return ""
	}
	if e, ok := m.WanPorts[marker]; ok {
		return e.PolicyID
	}
	return ""
}

func (m *Manifest) SetSystemZoneIDs(externalID, gatewayID string) {
	m.ExternalZoneID = externalID
	m.GatewayZoneID = gatewayID
	m.UpdatedAt = time.Now().UTC()
}
