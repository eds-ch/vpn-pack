package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"tailscale.com/tailcfg"
	"unifi-tailscale/manager/internal/wgs2s"
)

// --- Mocks ---

type mockDiagnosticsTailscale struct {
	checkIPForwardingFn func(ctx context.Context) error
	currentDERPMapFn    func(ctx context.Context) (*tailcfg.DERPMap, error)
	bugReportFn         func(ctx context.Context, note string) (string, error)
}

func (m *mockDiagnosticsTailscale) CheckIPForwarding(ctx context.Context) error {
	if m.checkIPForwardingFn != nil {
		return m.checkIPForwardingFn(ctx)
	}
	return nil
}

func (m *mockDiagnosticsTailscale) CurrentDERPMap(ctx context.Context) (*tailcfg.DERPMap, error) {
	if m.currentDERPMapFn != nil {
		return m.currentDERPMapFn(ctx)
	}
	return &tailcfg.DERPMap{}, nil
}

func (m *mockDiagnosticsTailscale) BugReport(ctx context.Context, note string) (string, error) {
	if m.bugReportFn != nil {
		return m.bugReportFn(ctx, note)
	}
	return "BUG-test-marker", nil
}

type mockDiagnosticsFirewall struct {
	checkWgS2sRulesPresentFn func(ctx context.Context, ifaces []string) map[string]bool
}

func (m *mockDiagnosticsFirewall) CheckWgS2sRulesPresent(ctx context.Context, ifaces []string) map[string]bool {
	if m.checkWgS2sRulesPresentFn != nil {
		return m.checkWgS2sRulesPresentFn(ctx, ifaces)
	}
	return nil
}

type mockDiagnosticsWgS2s struct {
	getTunnelsFn  func() []wgs2s.TunnelConfig
	getStatusesFn func() []wgs2s.WgS2sStatus
}

func (m *mockDiagnosticsWgS2s) GetTunnels() []wgs2s.TunnelConfig {
	if m.getTunnelsFn != nil {
		return m.getTunnelsFn()
	}
	return nil
}

func (m *mockDiagnosticsWgS2s) GetStatuses() []wgs2s.WgS2sStatus {
	if m.getStatusesFn != nil {
		return m.getStatusesFn()
	}
	return nil
}

// --- Factory ---

func newTestDiagnosticsService(opts ...func(*DiagnosticsService)) *DiagnosticsService {
	svc := &DiagnosticsService{
		ts: &mockDiagnosticsTailscale{},
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// --- Tests ---

func TestGetDiagnostics_AllSuccess(t *testing.T) {
	svc := newTestDiagnosticsService(func(s *DiagnosticsService) {
		s.ts = &mockDiagnosticsTailscale{
			currentDERPMapFn: func(ctx context.Context) (*tailcfg.DERPMap, error) {
				return &tailcfg.DERPMap{
					Regions: map[int]*tailcfg.DERPRegion{
						1: {RegionID: 1, RegionCode: "nyc", RegionName: "New York"},
					},
				}, nil
			},
		}
	})

	resp, err := svc.GetDiagnostics(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "enabled", resp.IPForwarding)
	assert.True(t, resp.FwmarkPatched)
	assert.Equal(t, "0x800000", resp.FwmarkValue)
	assert.Nil(t, resp.WgS2s)
	assert.Len(t, resp.DERPRegions, 1)
	assert.Equal(t, "nyc", resp.DERPRegions[0].RegionCode)
}

func TestGetDiagnostics_IPForwardingError(t *testing.T) {
	svc := newTestDiagnosticsService(func(s *DiagnosticsService) {
		s.ts = &mockDiagnosticsTailscale{
			checkIPForwardingFn: func(ctx context.Context) error {
				return errors.New("ip forwarding disabled")
			},
		}
	})

	resp, err := svc.GetDiagnostics(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "ip forwarding disabled", resp.IPForwarding)
}

func TestGetDiagnostics_NilWgManager(t *testing.T) {
	svc := newTestDiagnosticsService()
	resp, err := svc.GetDiagnostics(context.Background())
	require.NoError(t, err)
	assert.Nil(t, resp.WgS2s)
}

func TestGetDiagnostics_WithWgManager(t *testing.T) {
	svc := newTestDiagnosticsService(func(s *DiagnosticsService) {
		s.wg = &mockDiagnosticsWgS2s{
			getTunnelsFn: func() []wgs2s.TunnelConfig {
				return []wgs2s.TunnelConfig{
					{ID: "t1", Name: "tunnel1", InterfaceName: "wg0", Enabled: true, AllowedIPs: []string{"10.0.0.0/24"}},
					{ID: "t2", Name: "tunnel2", InterfaceName: "wg1", Enabled: false},
				}
			},
			getStatusesFn: func() []wgs2s.WgS2sStatus {
				return []wgs2s.WgS2sStatus{
					{ID: "t1", Connected: true, Endpoint: "1.2.3.4:51820"},
				}
			},
		}
		s.fw = &mockDiagnosticsFirewall{
			checkWgS2sRulesPresentFn: func(ctx context.Context, ifaces []string) map[string]bool {
				return map[string]bool{"wg0": true}
			},
		}
	})

	resp, err := svc.GetDiagnostics(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp.WgS2s)
	assert.Len(t, resp.WgS2s.Tunnels, 1)
	assert.Equal(t, "t1", resp.WgS2s.Tunnels[0].ID)
	assert.True(t, resp.WgS2s.Tunnels[0].ForwardINOk)
	assert.True(t, resp.WgS2s.Tunnels[0].Connected)
	assert.Equal(t, "1.2.3.4:51820", resp.WgS2s.Tunnels[0].Endpoint)
}

func TestGetDiagnostics_DERPMapError(t *testing.T) {
	svc := newTestDiagnosticsService(func(s *DiagnosticsService) {
		s.ts = &mockDiagnosticsTailscale{
			currentDERPMapFn: func(ctx context.Context) (*tailcfg.DERPMap, error) {
				return nil, errors.New("derp unavailable")
			},
		}
	})

	resp, err := svc.GetDiagnostics(context.Background())
	require.NoError(t, err)
	assert.Empty(t, resp.DERPRegions)
}

func TestBugReport_Success(t *testing.T) {
	svc := newTestDiagnosticsService(func(s *DiagnosticsService) {
		s.ts = &mockDiagnosticsTailscale{
			bugReportFn: func(ctx context.Context, note string) (string, error) {
				return "BUG-12345", nil
			},
		}
	})

	marker, err := svc.BugReport(context.Background(), "test note")
	require.NoError(t, err)
	assert.Equal(t, "BUG-12345", marker)
}

func TestBugReport_Error(t *testing.T) {
	svc := newTestDiagnosticsService(func(s *DiagnosticsService) {
		s.ts = &mockDiagnosticsTailscale{
			bugReportFn: func(ctx context.Context, note string) (string, error) {
				return "", errors.New("connection refused")
			},
		}
	})

	_, err := svc.BugReport(context.Background(), "")
	require.Error(t, err)
	var se *Error
	require.True(t, errors.As(err, &se))
	assert.Equal(t, ErrUpstream, se.Kind)
}

func TestBuildDERPRegions_Sorting(t *testing.T) {
	derpMap := &tailcfg.DERPMap{
		Regions: map[int]*tailcfg.DERPRegion{
			1: {RegionID: 1, RegionCode: "nyc", RegionName: "New York"},
			2: {RegionID: 2, RegionCode: "sfo", RegionName: "San Francisco"},
			3: {RegionID: 3, RegionCode: "tok", RegionName: "Tokyo"},
		},
	}
	latency := map[string]int64{
		"1": 50_000_000,
		"2": 30_000_000,
	}

	regions := BuildDERPRegions(derpMap, nil, latency, 2)

	require.Len(t, regions, 3)
	assert.Equal(t, "sfo", regions[0].RegionCode)
	assert.Equal(t, "nyc", regions[1].RegionCode)
	assert.Equal(t, "tok", regions[2].RegionCode)
	assert.True(t, regions[0].Preferred)
	assert.InDelta(t, 30.0, regions[0].LatencyMs, 0.01)
	assert.InDelta(t, 50.0, regions[1].LatencyMs, 0.01)
	assert.Equal(t, float64(0), regions[2].LatencyMs)
}

func TestBuildDERPRegions_NilMap(t *testing.T) {
	regions := BuildDERPRegions(nil, nil, nil, 0)
	assert.Empty(t, regions)
}

func TestBuildDERPRegions_Error(t *testing.T) {
	regions := BuildDERPRegions(&tailcfg.DERPMap{}, errors.New("fail"), nil, 0)
	assert.Empty(t, regions)
}

func TestGetDiagnostics_WgNilFirewall(t *testing.T) {
	svc := newTestDiagnosticsService(func(s *DiagnosticsService) {
		s.wg = &mockDiagnosticsWgS2s{
			getTunnelsFn: func() []wgs2s.TunnelConfig {
				return []wgs2s.TunnelConfig{
					{ID: "t1", Name: "tunnel1", InterfaceName: "wg0", Enabled: true},
				}
			},
			getStatusesFn: func() []wgs2s.WgS2sStatus { return nil },
		}
	})

	resp, err := svc.GetDiagnostics(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp.WgS2s)
	assert.Len(t, resp.WgS2s.Tunnels, 1)
	assert.False(t, resp.WgS2s.Tunnels[0].ForwardINOk)
}
