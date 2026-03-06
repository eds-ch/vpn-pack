package state

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
)

type Manifest struct {
	mu             sync.RWMutex                       `json:"-"`
	path           string                             `json:"-"`
	Version        int                                `json:"version"`
	CreatedAt      time.Time                          `json:"createdAt"`
	UpdatedAt      time.Time                          `json:"updatedAt"`
	SiteID         string                             `json:"siteId,omitempty"`
	Tailscale      domain.ZoneManifest                `json:"tailscale"`
	WgS2s          map[string]domain.ZoneManifest     `json:"wgS2s,omitempty"`
	WanPorts       map[string]domain.WanPortEntry     `json:"wanPorts,omitempty"`
	ExternalZoneID string                             `json:"externalZoneId,omitempty"`
	GatewayZoneID  string                             `json:"gatewayZoneId,omitempty"`
	DNSPolicies    map[string]domain.DNSPolicyEntry   `json:"dnsPolicies,omitempty"`
}

func NewManifest(path string) *Manifest {
	return &Manifest{path: path, Version: 2, CreatedAt: time.Now().UTC()}
}

func (m *Manifest) Path() string { return m.path }

func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewManifest(path), nil
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
		Tailscale: domain.ZoneManifest{ChainPrefix: "VPN"},
	}

	if v1.ModeB.ZoneID != "" {
		m.Tailscale.ZoneID = v1.ModeB.ZoneID
		m.Tailscale.PolicyIDs = v1.ModeB.PolicyIDs
	}

	return m, nil
}

func (m *Manifest) saveLocked() error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("manifest marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(m.path), config.DirPerm); err != nil {
		return fmt.Errorf("manifest dir create: %w", err)
	}
	tmpPath := m.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, config.SecretPerm); err != nil {
		return fmt.Errorf("manifest write tmp: %w", err)
	}
	if err := os.Rename(tmpPath, m.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("manifest rename: %w", err)
	}
	return nil
}

func (m *Manifest) SetTailscaleZone(zoneID, zoneName string, policyIDs []string, chainPrefix string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Tailscale = domain.ZoneManifest{ZoneID: zoneID, ZoneName: zoneName, PolicyIDs: policyIDs, ChainPrefix: chainPrefix}
	m.UpdatedAt = time.Now().UTC()
	return m.saveLocked()
}

func (m *Manifest) SetWgS2sZone(tunnelID string, zm domain.ZoneManifest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.WgS2s == nil {
		m.WgS2s = make(map[string]domain.ZoneManifest)
	}
	m.WgS2s[tunnelID] = zm
	m.UpdatedAt = time.Now().UTC()
	return m.saveLocked()
}

func (m *Manifest) RemoveWgS2sTunnel(tunnelID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.WgS2s[tunnelID]; !ok {
		slog.Debug("removing non-existing wg-s2s tunnel from manifest", "tunnelID", tunnelID)
	}
	delete(m.WgS2s, tunnelID)
	m.UpdatedAt = time.Now().UTC()
	return m.saveLocked()
}

func (m *Manifest) GetWgS2sZones() []domain.WgS2sZoneInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := make(map[string]*domain.WgS2sZoneInfo)
	var order []string
	for _, zm := range m.WgS2s {
		if zm.ZoneID == "" {
			continue
		}
		if info, ok := seen[zm.ZoneID]; ok {
			info.TunnelCount++
		} else {
			seen[zm.ZoneID] = &domain.WgS2sZoneInfo{ZoneID: zm.ZoneID, ZoneName: zm.ZoneName, TunnelCount: 1}
			order = append(order, zm.ZoneID)
		}
	}
	result := make([]domain.WgS2sZoneInfo, 0, len(order))
	for _, id := range order {
		result = append(result, *seen[id])
	}
	return result
}

func (m *Manifest) GetTailscaleChainPrefix() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.Tailscale.ChainPrefix != "" {
		return m.Tailscale.ChainPrefix
	}
	return "VPN"
}

func (m *Manifest) GetWgS2sChainPrefix(tunnelID string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if zm, ok := m.WgS2s[tunnelID]; ok && zm.ChainPrefix != "" {
		return zm.ChainPrefix
	}
	return "VPN"
}

func (m *Manifest) SetWanPort(marker, policyID, policyName string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.WanPorts == nil {
		m.WanPorts = make(map[string]domain.WanPortEntry)
	}
	m.WanPorts[marker] = domain.WanPortEntry{PolicyID: policyID, PolicyName: policyName, Port: port}
	m.UpdatedAt = time.Now().UTC()
	return m.saveLocked()
}

func (m *Manifest) RemoveWanPort(marker string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.WanPorts, marker)
	m.UpdatedAt = time.Now().UTC()
	return m.saveLocked()
}

func (m *Manifest) GetWanPortPolicyID(marker string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.WanPorts == nil {
		return ""
	}
	if e, ok := m.WanPorts[marker]; ok {
		return e.PolicyID
	}
	return ""
}

func (m *Manifest) SetSystemZoneIDs(externalID, gatewayID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExternalZoneID = externalID
	m.GatewayZoneID = gatewayID
	m.UpdatedAt = time.Now().UTC()
	return m.saveLocked()
}

func (m *Manifest) GetSiteID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.SiteID
}

func (m *Manifest) SetSiteID(siteID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SiteID = siteID
	m.UpdatedAt = time.Now().UTC()
	return m.saveLocked()
}

func (m *Manifest) HasSiteID() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.SiteID != ""
}

func (m *Manifest) GetWgS2sZone(tunnelID string) (domain.ZoneManifest, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	zm, ok := m.WgS2s[tunnelID]
	return zm, ok
}

func (m *Manifest) GetWanPortEntry(marker string) (domain.WanPortEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.WanPorts == nil {
		return domain.WanPortEntry{}, false
	}
	e, ok := m.WanPorts[marker]
	return e, ok
}

func (m *Manifest) SetDNSPolicy(marker, policyID, dom, ipAddress string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.DNSPolicies == nil {
		m.DNSPolicies = make(map[string]domain.DNSPolicyEntry)
	}
	m.DNSPolicies[marker] = domain.DNSPolicyEntry{PolicyID: policyID, Domain: dom, IPAddress: ipAddress}
	m.UpdatedAt = time.Now().UTC()
	return m.saveLocked()
}

func (m *Manifest) RemoveDNSPolicy(marker string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.DNSPolicies, marker)
	m.UpdatedAt = time.Now().UTC()
	return m.saveLocked()
}

func (m *Manifest) GetDNSPolicy(marker string) (domain.DNSPolicyEntry, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.DNSPolicies == nil {
		return domain.DNSPolicyEntry{}, false
	}
	e, ok := m.DNSPolicies[marker]
	return e, ok
}

func (m *Manifest) HasDNSPolicy(marker string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.DNSPolicies == nil {
		return false
	}
	_, ok := m.DNSPolicies[marker]
	return ok
}

func (m *Manifest) GetTailscaleZone() domain.ZoneManifest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.Tailscale
}

func (m *Manifest) GetSystemZoneIDs() (string, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ExternalZoneID, m.GatewayZoneID
}

func (m *Manifest) ResetIntegration() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Tailscale = domain.ZoneManifest{}
	m.WgS2s = nil
	m.WanPorts = nil
	m.DNSPolicies = nil
	m.ExternalZoneID = ""
	m.GatewayZoneID = ""
	m.UpdatedAt = time.Now().UTC()
	return m.saveLocked()
}

func (m *Manifest) GetWanPortsSnapshot() map[string]domain.WanPortEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.WanPorts) == 0 {
		return nil
	}
	cp := make(map[string]domain.WanPortEntry, len(m.WanPorts))
	for k, v := range m.WanPorts {
		cp[k] = v
	}
	return cp
}

func (m *Manifest) GetWgS2sSnapshot() map[string]domain.ZoneManifest {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.WgS2s) == 0 {
		return nil
	}
	cp := make(map[string]domain.ZoneManifest, len(m.WgS2s))
	for k, v := range m.WgS2s {
		cp[k] = v
	}
	return cp
}
