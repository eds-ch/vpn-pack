package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unifi-tailscale/manager/internal/wgs2s"
)

func testBase64Key(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(key)
}

func TestValidateCreateRequest(t *testing.T) {
	validReq := func(t *testing.T) *WgS2sCreateRequest {
		t.Helper()
		return &WgS2sCreateRequest{
			TunnelConfig: wgs2s.TunnelConfig{
				Name:          "test-tunnel",
				ListenPort:    51820,
				TunnelAddress: "10.0.0.1/24",
				PeerPublicKey: testBase64Key(t),
				AllowedIPs:    []string{"10.0.0.0/24"},
			},
		}
	}

	tests := []struct {
		name    string
		modify  func(*WgS2sCreateRequest)
		wantErr bool
		errMsg  string
	}{
		{"valid minimal", func(r *WgS2sCreateRequest) {}, false, ""},
		{"missing name", func(r *WgS2sCreateRequest) { r.Name = "" }, true, "name"},
		{"zero port", func(r *WgS2sCreateRequest) { r.ListenPort = 0 }, true, "listenPort"},
		{"negative port", func(r *WgS2sCreateRequest) { r.ListenPort = -1 }, true, "listenPort"},
		{"port exceeds 65535", func(r *WgS2sCreateRequest) { r.ListenPort = 65536 }, true, "listenPort"},
		{"port at max", func(r *WgS2sCreateRequest) { r.ListenPort = 65535 }, false, ""},
		{"invalid tunnelAddress", func(r *WgS2sCreateRequest) { r.TunnelAddress = "not-cidr" }, true, "tunnelAddress"},
		{"short peerKey", func(r *WgS2sCreateRequest) { r.PeerPublicKey = "abc" }, true, "peerPublicKey"},
		{"bad base64 peerKey", func(r *WgS2sCreateRequest) { r.PeerPublicKey = "!!!not-base64-at-all-but-is-44-chars-long!==" }, true, "peerPublicKey"},
		{"invalid allowedIP", func(r *WgS2sCreateRequest) { r.AllowedIPs = []string{"bad"} }, true, "allowedIP"},
		{"valid multiple CIDRs", func(r *WgS2sCreateRequest) { r.AllowedIPs = []string{"10.0.0.0/24", "192.168.1.0/24"} }, false, ""},
		{"zero routeMetric (default)", func(r *WgS2sCreateRequest) { r.RouteMetric = 0 }, false, ""},
		{"valid routeMetric", func(r *WgS2sCreateRequest) { r.RouteMetric = 200 }, false, ""},
		{"routeMetric at max", func(r *WgS2sCreateRequest) { r.RouteMetric = 9999 }, false, ""},
		{"negative routeMetric", func(r *WgS2sCreateRequest) { r.RouteMetric = -1 }, true, "routeMetric"},
		{"routeMetric exceeds max", func(r *WgS2sCreateRequest) { r.RouteMetric = 10000 }, true, "routeMetric"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validReq(t)
			tt.modify(req)
			err := validateCreateRequest(req)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHumanizeWgS2sError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"not found", errors.New("tunnel not found"), "Tunnel not found"},
		{"port in use", errors.New("port in use"), "already in use"},
		{"permission denied", errors.New("permission denied"), "Insufficient permissions"},
		{"unknown", errors.New("something else"), "WG S2S error:"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := humanizeWgS2sError(tt.err)
			if tt.err == nil {
				assert.Equal(t, "", result)
			} else {
				assert.Contains(t, result, tt.want)
			}
		})
	}
}

func TestValidateUpdateRequest(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*wgs2s.TunnelConfig)
		wantErr bool
		errMsg  string
	}{
		{"empty update", func(c *wgs2s.TunnelConfig) {}, false, ""},
		{"valid tunnelAddress", func(c *wgs2s.TunnelConfig) { c.TunnelAddress = "10.0.0.1/24" }, false, ""},
		{"invalid tunnelAddress", func(c *wgs2s.TunnelConfig) { c.TunnelAddress = "not-cidr" }, true, "tunnelAddress"},
		{"valid peerPublicKey", func(c *wgs2s.TunnelConfig) { c.PeerPublicKey = testBase64Key(t) }, false, ""},
		{"invalid peerPublicKey", func(c *wgs2s.TunnelConfig) { c.PeerPublicKey = "short" }, true, "peerPublicKey"},
		{"valid allowedIPs", func(c *wgs2s.TunnelConfig) { c.AllowedIPs = []string{"10.0.0.0/24"} }, false, ""},
		{"invalid allowedIP", func(c *wgs2s.TunnelConfig) { c.AllowedIPs = []string{"bad"} }, true, "allowedIP"},
		{"negative port", func(c *wgs2s.TunnelConfig) { c.ListenPort = -1 }, true, "listenPort"},
		{"port exceeds 65535", func(c *wgs2s.TunnelConfig) { c.ListenPort = 65536 }, true, "listenPort"},
		{"zero port (no change)", func(c *wgs2s.TunnelConfig) { c.ListenPort = 0 }, false, ""},
		{"positive port", func(c *wgs2s.TunnelConfig) { c.ListenPort = 51821 }, false, ""},
		{"port at max", func(c *wgs2s.TunnelConfig) { c.ListenPort = 65535 }, false, ""},
		{"zero routeMetric (no change)", func(c *wgs2s.TunnelConfig) { c.RouteMetric = 0 }, false, ""},
		{"valid routeMetric", func(c *wgs2s.TunnelConfig) { c.RouteMetric = 500 }, false, ""},
		{"negative routeMetric", func(c *wgs2s.TunnelConfig) { c.RouteMetric = -1 }, true, "routeMetric"},
		{"routeMetric exceeds max", func(c *wgs2s.TunnelConfig) { c.RouteMetric = 10000 }, true, "routeMetric"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := wgs2s.TunnelConfig{}
			tt.modify(&cfg)
			err := validateUpdateRequest(&cfg)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- Mock types for service-level tests ---

type mockWgS2sWireGuard struct {
	createTunnelFn  func(wgs2s.TunnelConfig, string) (*wgs2s.TunnelConfig, error)
	deleteTunnelFn  func(string) error
	enableTunnelFn  func(string) error
	disableTunnelFn func(string) error
	updateTunnelFn  func(string, wgs2s.TunnelConfig) (*wgs2s.TunnelConfig, error)
	getTunnelsFn    func() []wgs2s.TunnelConfig
	getStatusesFn   func() []wgs2s.WgS2sStatus
	getPublicKeyFn  func(string) (string, error)
}

func (m *mockWgS2sWireGuard) CreateTunnel(cfg wgs2s.TunnelConfig, pk string) (*wgs2s.TunnelConfig, error) {
	if m.createTunnelFn != nil {
		return m.createTunnelFn(cfg, pk)
	}
	return &cfg, nil
}

func (m *mockWgS2sWireGuard) DeleteTunnel(id string) error {
	if m.deleteTunnelFn != nil {
		return m.deleteTunnelFn(id)
	}
	return nil
}

func (m *mockWgS2sWireGuard) EnableTunnel(id string) error {
	if m.enableTunnelFn != nil {
		return m.enableTunnelFn(id)
	}
	return nil
}

func (m *mockWgS2sWireGuard) DisableTunnel(id string) error {
	if m.disableTunnelFn != nil {
		return m.disableTunnelFn(id)
	}
	return nil
}

func (m *mockWgS2sWireGuard) UpdateTunnel(id string, updates wgs2s.TunnelConfig) (*wgs2s.TunnelConfig, error) {
	if m.updateTunnelFn != nil {
		return m.updateTunnelFn(id, updates)
	}
	return &updates, nil
}

func (m *mockWgS2sWireGuard) GetTunnels() []wgs2s.TunnelConfig {
	if m.getTunnelsFn != nil {
		return m.getTunnelsFn()
	}
	return nil
}

func (m *mockWgS2sWireGuard) GetStatuses() []wgs2s.WgS2sStatus {
	if m.getStatusesFn != nil {
		return m.getStatusesFn()
	}
	return nil
}

func (m *mockWgS2sWireGuard) GetPublicKey(id string) (string, error) {
	if m.getPublicKeyFn != nil {
		return m.getPublicKeyFn(id)
	}
	return "", nil
}

type mockWgS2sFirewall struct {
	setupZoneFn        func(context.Context, string, string, string) *ZoneSetupResult
	setupFirewallFn    func(context.Context, string, string, []string) error
	removeFirewallFn   func(context.Context, string, string, []string)
	removeIPSetFn      func(context.Context, string, []string)
	teardownZoneFn     func(context.Context, string)
	openWanPortFn      func(context.Context, int, string)
	closeWanPortFn     func(context.Context, int, string)
	checkRulesFn       func(context.Context, []string) map[string]bool
	integrationReadyFn func() bool
}

func (m *mockWgS2sFirewall) SetupZone(ctx context.Context, tid, zid, zname string) *ZoneSetupResult {
	if m.setupZoneFn != nil {
		return m.setupZoneFn(ctx, tid, zid, zname)
	}
	return nil
}
func (m *mockWgS2sFirewall) SetupFirewall(ctx context.Context, tid, iface string, ips []string) error {
	if m.setupFirewallFn != nil {
		return m.setupFirewallFn(ctx, tid, iface, ips)
	}
	return nil
}
func (m *mockWgS2sFirewall) RemoveFirewall(ctx context.Context, tid, iface string, ips []string) {
	if m.removeFirewallFn != nil {
		m.removeFirewallFn(ctx, tid, iface, ips)
	}
}
func (m *mockWgS2sFirewall) RemoveIPSetEntries(ctx context.Context, tid string, cidrs []string) {
	if m.removeIPSetFn != nil {
		m.removeIPSetFn(ctx, tid, cidrs)
	}
}
func (m *mockWgS2sFirewall) TeardownZone(ctx context.Context, tid string) {
	if m.teardownZoneFn != nil {
		m.teardownZoneFn(ctx, tid)
	}
}
func (m *mockWgS2sFirewall) OpenWanPort(ctx context.Context, port int, iface string) {
	if m.openWanPortFn != nil {
		m.openWanPortFn(ctx, port, iface)
	}
}
func (m *mockWgS2sFirewall) CloseWanPort(ctx context.Context, port int, iface string) {
	if m.closeWanPortFn != nil {
		m.closeWanPortFn(ctx, port, iface)
	}
}
func (m *mockWgS2sFirewall) CheckRulesPresent(ctx context.Context, ifaces []string) map[string]bool {
	if m.checkRulesFn != nil {
		return m.checkRulesFn(ctx, ifaces)
	}
	return nil
}
func (m *mockWgS2sFirewall) IntegrationReady() bool {
	if m.integrationReadyFn != nil {
		return m.integrationReadyFn()
	}
	return false
}

type mockWgS2sManifest struct {
	getZoneFn  func(string) (ZoneInfo, bool)
	getZonesFn func() []WgS2sZoneEntry
}

func (m *mockWgS2sManifest) GetZone(tid string) (ZoneInfo, bool) {
	if m.getZoneFn != nil {
		return m.getZoneFn(tid)
	}
	return ZoneInfo{}, false
}
func (m *mockWgS2sManifest) GetZones() []WgS2sZoneEntry {
	if m.getZonesFn != nil {
		return m.getZonesFn()
	}
	return nil
}

func newTestWgS2sService(wg WgS2sWireGuard, opts ...func(*WgS2sService)) *WgS2sService {
	svc := NewWgS2sService(WgS2sConfig{
		WG:       wg,
		Manifest: &mockWgS2sManifest{},
	})
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

func TestCreateTunnelValidation(t *testing.T) {
	svc := newTestWgS2sService(&mockWgS2sWireGuard{})
	_, err := svc.CreateTunnel(context.Background(), &WgS2sCreateRequest{})
	require.Error(t, err)
	var se *Error
	require.True(t, errors.As(err, &se))
	assert.Equal(t, ErrValidation, se.Kind)
}

func TestCreateTunnelSuccess(t *testing.T) {
	tunnel := &wgs2s.TunnelConfig{
		ID: "t1", Name: "test", InterfaceName: "wg-s2s0",
		ListenPort: 51820, TunnelAddress: "10.0.0.1/24",
		AllowedIPs: []string{"10.0.0.0/24"},
	}
	svc := newTestWgS2sService(&mockWgS2sWireGuard{
		createTunnelFn: func(_ wgs2s.TunnelConfig, _ string) (*wgs2s.TunnelConfig, error) {
			return tunnel, nil
		},
		getPublicKeyFn: func(string) (string, error) { return "pubkey", nil },
	})

	resp, err := svc.CreateTunnel(context.Background(), &WgS2sCreateRequest{
		TunnelConfig: wgs2s.TunnelConfig{
			Name:          "test",
			ListenPort:    51820,
			TunnelAddress: "10.0.0.1/24",
			PeerPublicKey: testBase64Key(t),
			AllowedIPs:    []string{"10.0.0.0/24"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "t1", resp.ID)
	assert.Equal(t, "pubkey", resp.PublicKey)
	assert.Equal(t, "ok", resp.SetupStatus)
	assert.Nil(t, resp.Firewall)
}

func TestCreateTunnelFirewallPartial(t *testing.T) {
	tunnel := &wgs2s.TunnelConfig{
		ID: "t1", Name: "test", InterfaceName: "wg-s2s0",
		ListenPort: 51820, TunnelAddress: "10.0.0.1/24",
		AllowedIPs: []string{"10.0.0.0/24"},
	}
	svc := newTestWgS2sService(
		&mockWgS2sWireGuard{
			createTunnelFn: func(_ wgs2s.TunnelConfig, _ string) (*wgs2s.TunnelConfig, error) {
				return tunnel, nil
			},
			getPublicKeyFn: func(string) (string, error) { return "pubkey", nil },
		},
		func(s *WgS2sService) {
			s.fw = &mockWgS2sFirewall{
				setupFirewallFn: func(context.Context, string, string, []string) error {
					return fmt.Errorf("UDAPI unreachable")
				},
				integrationReadyFn: func() bool { return false },
			}
		},
	)

	resp, err := svc.CreateTunnel(context.Background(), &WgS2sCreateRequest{
		TunnelConfig: wgs2s.TunnelConfig{
			Name:          "test",
			ListenPort:    51820,
			TunnelAddress: "10.0.0.1/24",
			PeerPublicKey: testBase64Key(t),
			AllowedIPs:    []string{"10.0.0.0/24"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "partial", resp.SetupStatus)
	require.NotNil(t, resp.Firewall)
	assert.Contains(t, resp.Firewall.Errors[0], "UDAPI unreachable")
}

func TestDeleteTunnelNotFound(t *testing.T) {
	svc := newTestWgS2sService(&mockWgS2sWireGuard{})
	err := svc.DeleteTunnel(context.Background(), "nonexistent")
	require.Error(t, err)
	var se *Error
	require.True(t, errors.As(err, &se))
	assert.Equal(t, ErrNotFound, se.Kind)
}

func TestDeleteTunnelSuccess(t *testing.T) {
	tunnel := wgs2s.TunnelConfig{
		ID: "t1", InterfaceName: "wg-s2s0",
		ListenPort: 51820, AllowedIPs: []string{"10.0.0.0/24"},
	}

	var deletedID string
	var removedFW, closedWan, tornDownZone bool

	wg := &mockWgS2sWireGuard{
		getTunnelsFn:   func() []wgs2s.TunnelConfig { return []wgs2s.TunnelConfig{tunnel} },
		deleteTunnelFn: func(id string) error { deletedID = id; return nil },
	}
	fw := &mockWgS2sFirewall{
		removeFirewallFn: func(_ context.Context, id, iface string, ips []string) {
			removedFW = true
			assert.Equal(t, "t1", id)
			assert.Equal(t, "wg-s2s0", iface)
			assert.Equal(t, []string{"10.0.0.0/24"}, ips)
		},
		closeWanPortFn: func(_ context.Context, port int, iface string) {
			closedWan = true
			assert.Equal(t, 51820, port)
		},
		teardownZoneFn: func(_ context.Context, id string) {
			tornDownZone = true
			assert.Equal(t, "t1", id)
		},
	}

	svc := newTestWgS2sService(wg, func(s *WgS2sService) { s.fw = fw })
	err := svc.DeleteTunnel(context.Background(), "t1")

	require.NoError(t, err)
	assert.Equal(t, "t1", deletedID)
	assert.True(t, removedFW, "RemoveFirewall should be called")
	assert.True(t, closedWan, "CloseWanPort should be called")
	assert.True(t, tornDownZone, "TeardownZone should be called")
}

func TestDeleteTunnelWgError(t *testing.T) {
	tunnel := wgs2s.TunnelConfig{
		ID: "t1", InterfaceName: "wg-s2s0",
		ListenPort: 51820, AllowedIPs: []string{"10.0.0.0/24"},
	}

	var fwCalled bool
	wg := &mockWgS2sWireGuard{
		getTunnelsFn:   func() []wgs2s.TunnelConfig { return []wgs2s.TunnelConfig{tunnel} },
		deleteTunnelFn: func(string) error { return fmt.Errorf("wg: interface busy") },
	}
	fw := &mockWgS2sFirewall{
		removeFirewallFn: func(context.Context, string, string, []string) { fwCalled = true },
		closeWanPortFn:   func(context.Context, int, string) { fwCalled = true },
		teardownZoneFn:   func(context.Context, string) { fwCalled = true },
	}

	svc := newTestWgS2sService(wg, func(s *WgS2sService) { s.fw = fw })
	err := svc.DeleteTunnel(context.Background(), "t1")

	require.Error(t, err)
	var se *Error
	require.True(t, errors.As(err, &se))
	assert.Equal(t, ErrUpstream, se.Kind)
	assert.False(t, fwCalled, "firewall should NOT be called when wg.DeleteTunnel fails")
}

func TestEnableTunnelFirewallPartial(t *testing.T) {
	tunnel := wgs2s.TunnelConfig{
		ID: "t1", Name: "test", InterfaceName: "wg-s2s0", Enabled: true,
		ListenPort: 51820, AllowedIPs: []string{"10.0.0.0/24"},
	}
	svc := newTestWgS2sService(
		&mockWgS2sWireGuard{
			enableTunnelFn: func(string) error { return nil },
			getTunnelsFn:   func() []wgs2s.TunnelConfig { return []wgs2s.TunnelConfig{tunnel} },
		},
		func(s *WgS2sService) {
			s.fw = &mockWgS2sFirewall{
				setupFirewallFn: func(context.Context, string, string, []string) error {
					return fmt.Errorf("ipset failed")
				},
			}
		},
	)

	resp, err := svc.EnableTunnel(context.Background(), "t1")
	require.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, "partial", resp.SetupStatus)
	require.NotNil(t, resp.Firewall)
	assert.Contains(t, resp.Firewall.Errors[0], "ipset failed")
}

func TestGenerateKeypair(t *testing.T) {
	svc := newTestWgS2sService(&mockWgS2sWireGuard{})
	kp, err := svc.GenerateKeypair()
	require.NoError(t, err)
	assert.Len(t, kp.PublicKey, 44)
	assert.Len(t, kp.PrivateKey, 44)
}

func TestListTunnels(t *testing.T) {
	tunnels := []wgs2s.TunnelConfig{
		{ID: "t1", Name: "tunnel-1", InterfaceName: "wg-s2s0"},
		{ID: "t2", Name: "tunnel-2", InterfaceName: "wg-s2s1"},
	}
	svc := newTestWgS2sService(&mockWgS2sWireGuard{
		getTunnelsFn:   func() []wgs2s.TunnelConfig { return tunnels },
		getPublicKeyFn: func(id string) (string, error) { return "pk-" + id, nil },
	})

	result := svc.ListTunnels(context.Background())
	require.Len(t, result, 2)
	assert.Equal(t, "t1", result[0].ID)
	assert.Equal(t, "pk-t1", result[0].PublicKey)
}

func TestSubnetConflictError(t *testing.T) {
	svc := newTestWgS2sService(&mockWgS2sWireGuard{})
	svc.validateSubnets = func(ips []string, _ ...string) ([]SubnetConflict, []SubnetConflict) {
		return nil, []SubnetConflict{{CIDR: "10.0.0.0/24", Message: "overlaps with br0"}}
	}

	_, err := svc.CreateTunnel(context.Background(), &WgS2sCreateRequest{
		TunnelConfig: wgs2s.TunnelConfig{
			Name:          "test",
			ListenPort:    51820,
			TunnelAddress: "10.0.0.1/24",
			PeerPublicKey: testBase64Key(t),
			AllowedIPs:    []string{"10.0.0.0/24"},
		},
	})
	require.Error(t, err)
	var sce *SubnetConflictError
	require.True(t, errors.As(err, &sce))
	assert.Len(t, sce.Conflicts, 1)
}
