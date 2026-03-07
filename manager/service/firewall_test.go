package service

import (
	"context"
	"errors"
	"testing"
	"unifi-tailscale/manager/domain"

	"github.com/stretchr/testify/assert"
)

// --- Mocks ---

type mockFWIntegration struct {
	hasAPIKey      bool
	ensureZoneFn   func(ctx context.Context, siteID, name string) (ZoneInfo, error)
	ensurePolicies func(ctx context.Context, siteID, name, zoneID string) ([]string, error)
	deletePolicy   func(ctx context.Context, siteID, policyID string) error
	deleteZone     func(ctx context.Context, siteID, zoneID string) error
}

func (m *mockFWIntegration) HasAPIKey() bool { return m.hasAPIKey }
func (m *mockFWIntegration) EnsureZone(ctx context.Context, siteID, name string) (ZoneInfo, error) {
	return m.ensureZoneFn(ctx, siteID, name)
}
func (m *mockFWIntegration) EnsurePolicies(ctx context.Context, siteID, name, zoneID string) ([]string, error) {
	return m.ensurePolicies(ctx, siteID, name, zoneID)
}
func (m *mockFWIntegration) DeletePolicy(ctx context.Context, siteID, policyID string) error {
	if m.deletePolicy != nil {
		return m.deletePolicy(ctx, siteID, policyID)
	}
	return nil
}
func (m *mockFWIntegration) DeleteZone(ctx context.Context, siteID, zoneID string) error {
	if m.deleteZone != nil {
		return m.deleteZone(ctx, siteID, zoneID)
	}
	return nil
}

type mockFWManifest struct {
	siteID              string
	tailscaleZone       domain.ZoneManifest
	tailscalePrefix     string
	wgS2sZones          map[string]domain.ZoneManifest
	setTailscaleZoneFn  func(zoneID, zoneName string, policyIDs []string, chainPrefix string) error
	setWgS2sZoneFn      func(tunnelID string, zs domain.ZoneManifest) error
	removeWgS2sTunnelFn func(tunnelID string) error
}

func (m *mockFWManifest) GetSiteID() string                  { return m.siteID }
func (m *mockFWManifest) HasSiteID() bool                    { return m.siteID != "" }
func (m *mockFWManifest) GetTailscaleZone() domain.ZoneManifest { return m.tailscaleZone }
func (m *mockFWManifest) GetTailscaleChainPrefix() string {
	if m.tailscalePrefix != "" {
		return m.tailscalePrefix
	}
	return m.tailscaleZone.ChainPrefix
}
func (m *mockFWManifest) SetTailscaleZone(zoneID, zoneName string, policyIDs []string, chainPrefix string) error {
	if m.setTailscaleZoneFn != nil {
		return m.setTailscaleZoneFn(zoneID, zoneName, policyIDs, chainPrefix)
	}
	m.tailscaleZone = domain.ZoneManifest{ZoneID: zoneID, ZoneName: zoneName, PolicyIDs: policyIDs, ChainPrefix: chainPrefix}
	return nil
}
func (m *mockFWManifest) GetWgS2sSnapshot() map[string]domain.ZoneManifest {
	if m.wgS2sZones == nil {
		return map[string]domain.ZoneManifest{}
	}
	cp := make(map[string]domain.ZoneManifest, len(m.wgS2sZones))
	for k, v := range m.wgS2sZones {
		cp[k] = v
	}
	return cp
}
func (m *mockFWManifest) GetWgS2sZone(tunnelID string) (domain.ZoneManifest, bool) {
	zm, ok := m.wgS2sZones[tunnelID]
	return zm, ok
}
func (m *mockFWManifest) SetWgS2sZone(tunnelID string, zs domain.ZoneManifest) error {
	if m.setWgS2sZoneFn != nil {
		return m.setWgS2sZoneFn(tunnelID, zs)
	}
	if m.wgS2sZones == nil {
		m.wgS2sZones = make(map[string]domain.ZoneManifest)
	}
	m.wgS2sZones[tunnelID] = zs
	return nil
}
func (m *mockFWManifest) RemoveWgS2sTunnel(tunnelID string) error {
	if m.removeWgS2sTunnelFn != nil {
		return m.removeWgS2sTunnelFn(tunnelID)
	}
	delete(m.wgS2sZones, tunnelID)
	return nil
}

type mockFWOps struct {
	discoverChainPrefix       func(ctx context.Context, zoneID string) string
	ensureTailscaleRules      func(chainPrefix string) error
	removeTailscaleIfaceRules func() error
}

func (m *mockFWOps) DiscoverChainPrefix(ctx context.Context, zoneID string) string {
	if m.discoverChainPrefix != nil {
		return m.discoverChainPrefix(ctx, zoneID)
	}
	return ""
}
func (m *mockFWOps) EnsureTailscaleRules(chainPrefix string) error {
	if m.ensureTailscaleRules != nil {
		return m.ensureTailscaleRules(chainPrefix)
	}
	return nil
}
func (m *mockFWOps) RemoveTailscaleInterfaceRules() error {
	if m.removeTailscaleIfaceRules != nil {
		return m.removeTailscaleIfaceRules()
	}
	return nil
}

// --- Helpers ---

func hasError(r *SetupResult, substr string) bool {
	for _, e := range r.Errors {
		if len(e) >= len(substr) && contains(e, substr) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func newTestOrch(ic *mockFWIntegration, mf *mockFWManifest, ops *mockFWOps) *FirewallOrchestrator {
	return NewFirewallOrchestrator(ic, mf, ops)
}

// --- SetupTailscaleFirewall Tests ---

func TestSetupTailscaleFirewall_IntegrationNotConfigured(t *testing.T) {
	ic := &mockFWIntegration{hasAPIKey: false}
	mf := &mockFWManifest{siteID: "site-1"}
	ops := &mockFWOps{}

	result := newTestOrch(ic, mf, ops).SetupTailscaleFirewall(context.Background())

	assert.False(t, result.ZoneCreated)
	assert.False(t, result.PoliciesReady)
	assert.False(t, result.UDAPIApplied)
	assert.Empty(t, result.Errors)
	assert.False(t, result.OK())
	assert.False(t, result.Degraded())
}

func TestSetupTailscaleFirewall_ZoneFail(t *testing.T) {
	ic := &mockFWIntegration{
		hasAPIKey: true,
		ensureZoneFn: func(ctx context.Context, siteID, name string) (ZoneInfo, error) {
			return ZoneInfo{}, errors.New("zone error")
		},
	}
	mf := &mockFWManifest{siteID: "site-1"}
	ops := &mockFWOps{}

	result := newTestOrch(ic, mf, ops).SetupTailscaleFirewall(context.Background())

	assert.False(t, result.ZoneCreated)
	assert.False(t, result.PoliciesReady)
	assert.False(t, result.OK())
	assert.True(t, hasError(result, "zone"))
}

func TestSetupTailscaleFirewall_PolicyFail_RollbackZone(t *testing.T) {
	var deletedZoneID string
	ic := &mockFWIntegration{
		hasAPIKey: true,
		ensureZoneFn: func(ctx context.Context, siteID, name string) (ZoneInfo, error) {
			return ZoneInfo{ZoneID: "zone-ts", ZoneName: "VPN Pack: Tailscale"}, nil
		},
		ensurePolicies: func(ctx context.Context, siteID, name, zoneID string) ([]string, error) {
			return nil, errors.New("policy error")
		},
		deleteZone: func(ctx context.Context, siteID, zoneID string) error {
			deletedZoneID = zoneID
			return nil
		},
	}
	mf := &mockFWManifest{siteID: "site-1"}
	ops := &mockFWOps{}

	result := newTestOrch(ic, mf, ops).SetupTailscaleFirewall(context.Background())

	assert.False(t, result.ZoneCreated, "zone should be rolled back")
	assert.Empty(t, result.ZoneID)
	assert.False(t, result.PoliciesReady)
	assert.False(t, result.OK())
	assert.Equal(t, "zone-ts", deletedZoneID, "rollback should delete the zone")
	assert.True(t, hasError(result, "policies"))
	assert.Equal(t, "", mf.tailscaleZone.ZoneID, "manifest should not contain zone")
}

func TestSetupTailscaleFirewall_UDAPIFail(t *testing.T) {
	ic := &mockFWIntegration{
		hasAPIKey: true,
		ensureZoneFn: func(ctx context.Context, siteID, name string) (ZoneInfo, error) {
			return ZoneInfo{ZoneID: "zone-ts", ZoneName: "VPN Pack: Tailscale"}, nil
		},
		ensurePolicies: func(ctx context.Context, siteID, name, zoneID string) ([]string, error) {
			return []string{"pol-1", "pol-2"}, nil
		},
	}
	mf := &mockFWManifest{siteID: "site-1"}
	ops := &mockFWOps{
		ensureTailscaleRules: func(chainPrefix string) error {
			return errors.New("udapi error")
		},
	}

	result := newTestOrch(ic, mf, ops).SetupTailscaleFirewall(context.Background())

	assert.True(t, result.ZoneCreated)
	assert.Equal(t, "zone-ts", result.ZoneID)
	assert.True(t, result.PoliciesReady)
	assert.False(t, result.UDAPIApplied)
	assert.True(t, result.Degraded())
	assert.False(t, result.OK())
	assert.True(t, hasError(result, "udapi"))
}

func TestSetupTailscaleFirewall_Success(t *testing.T) {
	ic := &mockFWIntegration{
		hasAPIKey: true,
		ensureZoneFn: func(ctx context.Context, siteID, name string) (ZoneInfo, error) {
			return ZoneInfo{ZoneID: "zone-ts", ZoneName: "VPN Pack: Tailscale"}, nil
		},
		ensurePolicies: func(ctx context.Context, siteID, name, zoneID string) ([]string, error) {
			return []string{"pol-1", "pol-2"}, nil
		},
	}
	mf := &mockFWManifest{siteID: "site-1"}
	ops := &mockFWOps{}

	result := newTestOrch(ic, mf, ops).SetupTailscaleFirewall(context.Background())

	assert.True(t, result.ZoneCreated)
	assert.Equal(t, "zone-ts", result.ZoneID)
	assert.Equal(t, "VPN Pack: Tailscale", result.ZoneName)
	assert.True(t, result.PoliciesReady)
	assert.True(t, result.UDAPIApplied)
	assert.True(t, result.OK())
	assert.False(t, result.Degraded())
	assert.Empty(t, result.Errors)
}

func TestSetupTailscaleFirewall_ManifestFail_RollbackZoneAndPolicies(t *testing.T) {
	var deletedZoneID string
	var deletedPolicies []string
	ic := &mockFWIntegration{
		hasAPIKey: true,
		ensureZoneFn: func(ctx context.Context, siteID, name string) (ZoneInfo, error) {
			return ZoneInfo{ZoneID: "zone-ts", ZoneName: "VPN Pack: Tailscale"}, nil
		},
		ensurePolicies: func(ctx context.Context, siteID, name, zoneID string) ([]string, error) {
			return []string{"pol-1", "pol-2"}, nil
		},
		deletePolicy: func(ctx context.Context, siteID, policyID string) error {
			deletedPolicies = append(deletedPolicies, policyID)
			return nil
		},
		deleteZone: func(ctx context.Context, siteID, zoneID string) error {
			deletedZoneID = zoneID
			return nil
		},
	}
	mf := &mockFWManifest{
		siteID: "site-1",
		setTailscaleZoneFn: func(zoneID, zoneName string, policyIDs []string, chainPrefix string) error {
			return errors.New("disk full")
		},
	}
	ops := &mockFWOps{}

	result := newTestOrch(ic, mf, ops).SetupTailscaleFirewall(context.Background())

	assert.False(t, result.ZoneCreated, "zone should be rolled back")
	assert.Empty(t, result.ZoneID)
	assert.False(t, result.PoliciesReady)
	assert.Nil(t, result.PolicyIDs)
	assert.False(t, result.OK())
	assert.Equal(t, "zone-ts", deletedZoneID, "rollback should delete the zone")
	assert.NotEmpty(t, deletedPolicies, "rollback should delete policies")
	assert.True(t, hasError(result, "manifest"))
}

func TestSetupTailscaleFirewall_RollbackFails_BestEffort(t *testing.T) {
	ic := &mockFWIntegration{
		hasAPIKey: true,
		ensureZoneFn: func(ctx context.Context, siteID, name string) (ZoneInfo, error) {
			return ZoneInfo{ZoneID: "zone-ts", ZoneName: "VPN Pack: Tailscale"}, nil
		},
		ensurePolicies: func(ctx context.Context, siteID, name, zoneID string) ([]string, error) {
			return nil, errors.New("policy error")
		},
		deleteZone: func(ctx context.Context, siteID, zoneID string) error {
			return errors.New("delete failed")
		},
	}
	mf := &mockFWManifest{siteID: "site-1"}
	ops := &mockFWOps{}

	result := newTestOrch(ic, mf, ops).SetupTailscaleFirewall(context.Background())

	assert.False(t, result.ZoneCreated)
	assert.True(t, hasError(result, "policies"))
	assert.Equal(t, "", mf.tailscaleZone.ZoneID, "manifest should not contain zone")
}

func TestRollbackZone_CancelledCtx_StillDeletes(t *testing.T) {
	var deletedZoneID string
	var deletedPolicies []string
	ic := &mockFWIntegration{
		hasAPIKey: true,
		deletePolicy: func(ctx context.Context, siteID, policyID string) error {
			deletedPolicies = append(deletedPolicies, policyID)
			return nil
		},
		deleteZone: func(ctx context.Context, siteID, zoneID string) error {
			deletedZoneID = zoneID
			return nil
		},
	}
	mf := &mockFWManifest{siteID: "site-1"}
	ops := &mockFWOps{}
	orch := newTestOrch(ic, mf, ops)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	orch.rollbackZone(ctx, "site-1", "zone-orphan", "test rollback", "pol-1", "pol-2")

	assert.Equal(t, "zone-orphan", deletedZoneID, "zone should be deleted despite cancelled ctx")
	assert.ElementsMatch(t, []string{"pol-1", "pol-2"}, deletedPolicies, "policies should be deleted despite cancelled ctx")
}

// --- SetupWgS2sZone Tests ---

func TestSetupWgS2sZone_PolicyFail_RollbackZone(t *testing.T) {
	var deletedZoneID string
	ic := &mockFWIntegration{
		hasAPIKey: true,
		ensureZoneFn: func(ctx context.Context, siteID, name string) (ZoneInfo, error) {
			return ZoneInfo{ZoneID: "zone-created", ZoneName: name}, nil
		},
		ensurePolicies: func(ctx context.Context, siteID, name, zoneID string) ([]string, error) {
			return nil, errors.New("policy error")
		},
		deleteZone: func(ctx context.Context, siteID, zoneID string) error {
			deletedZoneID = zoneID
			return nil
		},
	}
	mf := &mockFWManifest{siteID: "site-1"}
	ops := &mockFWOps{}

	result := newTestOrch(ic, mf, ops).SetupWgS2sZone(context.Background(), "tun-1", "", "Test Tunnel")

	assert.False(t, result.ZoneCreated, "zone should be rolled back")
	assert.Empty(t, result.ZoneID)
	assert.False(t, result.PoliciesReady)
	assert.True(t, hasError(result, "policies"))
	assert.Equal(t, "zone-created", deletedZoneID, "rollback should delete the zone")

	_, ok := mf.wgS2sZones["tun-1"]
	assert.False(t, ok, "manifest should not contain tunnel zone")
}

func TestSetupWgS2sZone_ManifestFail_RollbackZoneAndPolicies(t *testing.T) {
	var deletedZoneID string
	var deletedPolicies []string
	ic := &mockFWIntegration{
		hasAPIKey: true,
		ensureZoneFn: func(ctx context.Context, siteID, name string) (ZoneInfo, error) {
			return ZoneInfo{ZoneID: "zone-created", ZoneName: name}, nil
		},
		ensurePolicies: func(ctx context.Context, siteID, name, zoneID string) ([]string, error) {
			return []string{"pol-1", "pol-2"}, nil
		},
		deletePolicy: func(ctx context.Context, siteID, policyID string) error {
			deletedPolicies = append(deletedPolicies, policyID)
			return nil
		},
		deleteZone: func(ctx context.Context, siteID, zoneID string) error {
			deletedZoneID = zoneID
			return nil
		},
	}
	mf := &mockFWManifest{
		siteID: "site-1",
		setWgS2sZoneFn: func(tunnelID string, zs domain.ZoneManifest) error {
			return errors.New("disk full")
		},
	}
	ops := &mockFWOps{}

	result := newTestOrch(ic, mf, ops).SetupWgS2sZone(context.Background(), "tun-1", "", "Test Tunnel")

	assert.False(t, result.ZoneCreated, "zone should be rolled back")
	assert.Empty(t, result.ZoneID)
	assert.False(t, result.PoliciesReady)
	assert.Nil(t, result.PolicyIDs)
	assert.True(t, hasError(result, "manifest"))
	assert.Equal(t, "zone-created", deletedZoneID, "rollback should delete the zone")
	assert.NotEmpty(t, deletedPolicies, "rollback should delete policies")
}

func TestSetupWgS2sZone_ZoneReuse_NoRollback(t *testing.T) {
	ic := &mockFWIntegration{hasAPIKey: true}
	mf := &mockFWManifest{
		siteID: "site-1",
		wgS2sZones: map[string]domain.ZoneManifest{
			"tun-existing": {ZoneID: "zone-shared", ZoneName: "Shared", PolicyIDs: []string{"pol-1"}, ChainPrefix: "VPN"},
		},
	}
	ops := &mockFWOps{}

	result := newTestOrch(ic, mf, ops).SetupWgS2sZone(context.Background(), "tun-2", "zone-shared", "")

	assert.True(t, result.ZoneCreated)
	assert.Equal(t, "zone-shared", result.ZoneID)
	assert.True(t, result.PoliciesReady)
	zm, ok := mf.wgS2sZones["tun-2"]
	assert.True(t, ok)
	assert.Equal(t, "zone-shared", zm.ZoneID)
}

// --- TeardownWgS2sZone Tests ---

func TestTeardownWgS2sZone_LastTunnel_DeletesZoneAndPolicies(t *testing.T) {
	var deletedPolicies []string
	var deletedZoneID string
	ic := &mockFWIntegration{
		hasAPIKey: true,
		deletePolicy: func(ctx context.Context, siteID, policyID string) error {
			deletedPolicies = append(deletedPolicies, policyID)
			return nil
		},
		deleteZone: func(ctx context.Context, siteID, zoneID string) error {
			deletedZoneID = zoneID
			return nil
		},
	}
	mf := &mockFWManifest{
		siteID: "site-1",
		wgS2sZones: map[string]domain.ZoneManifest{
			"tun-1": {ZoneID: "zone-wg", ZoneName: "WG S2S", PolicyIDs: []string{"pol-a", "pol-b"}, ChainPrefix: "CUSTOM1"},
		},
	}
	ops := &mockFWOps{}

	newTestOrch(ic, mf, ops).TeardownWgS2sZone(context.Background(), "tun-1")

	_, ok := mf.wgS2sZones["tun-1"]
	assert.False(t, ok, "tunnel should be removed from manifest")
	assert.Equal(t, "zone-wg", deletedZoneID, "zone should be deleted")
	assert.ElementsMatch(t, []string{"pol-a", "pol-b"}, deletedPolicies, "all policies should be deleted")
}

func TestTeardownWgS2sZone_SharedZone_KeepsZone(t *testing.T) {
	var deletedZoneID string
	ic := &mockFWIntegration{
		hasAPIKey: true,
		deleteZone: func(ctx context.Context, siteID, zoneID string) error {
			deletedZoneID = zoneID
			return nil
		},
	}
	zm := domain.ZoneManifest{ZoneID: "zone-shared", ZoneName: "Shared", PolicyIDs: []string{"pol-1"}, ChainPrefix: "VPN"}
	mf := &mockFWManifest{
		siteID: "site-1",
		wgS2sZones: map[string]domain.ZoneManifest{
			"tun-1": zm,
			"tun-2": zm,
		},
	}
	ops := &mockFWOps{}

	newTestOrch(ic, mf, ops).TeardownWgS2sZone(context.Background(), "tun-1")

	_, ok := mf.wgS2sZones["tun-1"]
	assert.False(t, ok, "tun-1 should be removed from manifest")
	_, ok = mf.wgS2sZones["tun-2"]
	assert.True(t, ok, "tun-2 should remain")
	assert.Empty(t, deletedZoneID, "shared zone should NOT be deleted")
}

func TestTeardownWgS2sZone_NotFound_Noop(t *testing.T) {
	ic := &mockFWIntegration{hasAPIKey: true}
	mf := &mockFWManifest{siteID: "site-1"}
	ops := &mockFWOps{}

	// Should not panic
	newTestOrch(ic, mf, ops).TeardownWgS2sZone(context.Background(), "nonexistent")
}
