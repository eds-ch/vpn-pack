package service

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
	"unifi-tailscale/manager/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/views"
)

// --- Mocks ---

type mockRoutingTailscale struct {
	getPrefsFn  func(ctx context.Context) (*ipn.Prefs, error)
	editPrefsFn func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error)
	statusFn    func(ctx context.Context) (*ipnstate.Status, error)
	startFn     func(ctx context.Context, opts ipn.Options) error
}

func (m *mockRoutingTailscale) GetPrefs(ctx context.Context) (*ipn.Prefs, error) {
	if m.getPrefsFn != nil {
		return m.getPrefsFn(ctx)
	}
	return &ipn.Prefs{}, nil
}

func (m *mockRoutingTailscale) EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	if m.editPrefsFn != nil {
		return m.editPrefsFn(ctx, mp)
	}
	return &ipn.Prefs{}, nil
}

func (m *mockRoutingTailscale) Status(ctx context.Context) (*ipnstate.Status, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx)
	}
	return &ipnstate.Status{}, nil
}

func (m *mockRoutingTailscale) Start(ctx context.Context, opts ipn.Options) error {
	if m.startFn != nil {
		return m.startFn(ctx, opts)
	}
	return nil
}

type mockRoutingFirewall struct {
	integrationReadyFn           func() bool
	checkTailscaleRulesPresentFn func(ctx context.Context) (bool, bool, bool, bool)
}

func (m *mockRoutingFirewall) IntegrationReady() bool {
	if m.integrationReadyFn != nil {
		return m.integrationReadyFn()
	}
	return true
}

func (m *mockRoutingFirewall) CheckTailscaleRulesPresent(ctx context.Context) (bool, bool, bool, bool) {
	if m.checkTailscaleRulesPresentFn != nil {
		return m.checkTailscaleRulesPresentFn(ctx)
	}
	return true, true, true, true
}

type mockRoutingIntegration struct {
	hasAPIKeyFn func() bool
}

func (m *mockRoutingIntegration) HasAPIKey() bool {
	if m.hasAPIKeyFn != nil {
		return m.hasAPIKeyFn()
	}
	return true
}

type mockRoutingManifest struct {
	getTailscaleChainPrefixFn func() string
}

func (m *mockRoutingManifest) GetTailscaleChainPrefix() string {
	if m.getTailscaleChainPrefixFn != nil {
		return m.getTailscaleChainPrefixFn()
	}
	return "TS"
}

// --- Factory ---

func newTestRoutingService(opts ...func(*RoutingService)) *RoutingService {
	svc := &RoutingService{
		ts:       &mockRoutingTailscale{},
		fw:       &mockRoutingFirewall{},
		ic:       &mockRoutingIntegration{},
		manifest: &mockRoutingManifest{},
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// --- GetRoutes tests ---

func TestGetRoutes_Success(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.ts = &mockRoutingTailscale{
			getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
				return &ipn.Prefs{
					AdvertiseRoutes: []netip.Prefix{
						netip.MustParsePrefix("10.0.0.0/24"),
						netip.MustParsePrefix("192.168.1.0/24"),
					},
				}, nil
			},
			statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
				ips := views.SliceOf([]netip.Prefix{
					netip.MustParsePrefix("10.0.0.0/24"),
				})
				return &ipnstate.Status{
					Self: &ipnstate.PeerStatus{
						AllowedIPs: &ips,
					},
				}, nil
			},
		}
	})

	resp, err := svc.GetRoutes(context.Background())
	require.NoError(t, err)
	require.Len(t, resp.Routes, 2)
	assert.Equal(t, "10.0.0.0/24", resp.Routes[0].CIDR)
	assert.True(t, resp.Routes[0].Approved)
	assert.Equal(t, "192.168.1.0/24", resp.Routes[1].CIDR)
	assert.False(t, resp.Routes[1].Approved)
	assert.False(t, resp.ExitNode)
}

func TestGetRoutes_WithExitNode(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.ts = &mockRoutingTailscale{
			getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
				return &ipn.Prefs{
					AdvertiseRoutes: []netip.Prefix{
						netip.MustParsePrefix("10.0.0.0/24"),
						netip.MustParsePrefix("0.0.0.0/0"),
						netip.MustParsePrefix("::/0"),
					},
				}, nil
			},
		}
	})

	resp, err := svc.GetRoutes(context.Background())
	require.NoError(t, err)
	require.Len(t, resp.Routes, 1)
	assert.True(t, resp.ExitNode)
}

func TestGetRoutes_GetPrefsError(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.ts = &mockRoutingTailscale{
			getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
				return nil, errors.New("connection refused")
			},
		}
	})

	_, err := svc.GetRoutes(context.Background())
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrUpstream, se.Kind)
}

func TestGetRoutes_EmptyRoutes(t *testing.T) {
	svc := newTestRoutingService()

	resp, err := svc.GetRoutes(context.Background())
	require.NoError(t, err)
	assert.Empty(t, resp.Routes)
	assert.False(t, resp.ExitNode)
}

// --- SetRoutes tests ---

func TestSetRoutes_Success(t *testing.T) {
	editCalled := false
	svc := newTestRoutingService(func(s *RoutingService) {
		s.ts = &mockRoutingTailscale{
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				editCalled = true
				assert.True(t, mp.AdvertiseRoutesSet)
				assert.Len(t, mp.Prefs.AdvertiseRoutes, 1)
				assert.Equal(t, "10.0.0.0/24", mp.Prefs.AdvertiseRoutes[0].String())
				return &ipn.Prefs{}, nil
			},
		}
	})

	result, err := svc.SetRoutes(context.Background(), &SetRoutesRequest{
		Routes: []string{"10.0.0.0/24"},
	}, nil)
	require.NoError(t, err)
	assert.True(t, result.OK)
	assert.True(t, editCalled)
	assert.Empty(t, result.Warning)
}

func TestSetRoutes_InvalidCIDR(t *testing.T) {
	svc := newTestRoutingService()

	_, err := svc.SetRoutes(context.Background(), &SetRoutesRequest{
		Routes: []string{"not-a-cidr"},
	}, nil)
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrValidation, se.Kind)
}

func TestSetRoutes_ExitNodeWithVPNClients(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.ts = &mockRoutingTailscale{
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				hasV4 := false
				hasV6 := false
				for _, p := range mp.Prefs.AdvertiseRoutes {
					if p.String() == "0.0.0.0/0" {
						hasV4 = true
					}
					if p.String() == "::/0" {
						hasV6 = true
					}
				}
				assert.True(t, hasV4, "should include 0.0.0.0/0")
				assert.True(t, hasV6, "should include ::/0")
				return &ipn.Prefs{}, nil
			},
		}
	})

	result, err := svc.SetRoutes(context.Background(), &SetRoutesRequest{
		ExitNode: true,
	}, []string{"wgclt1", "wgclt2"})
	require.NoError(t, err)
	assert.True(t, result.OK)
	assert.Contains(t, result.Warning, "wgclt1")
	assert.Contains(t, result.Warning, "wgclt2")
}

func TestSetRoutes_ExitNodeNoVPNClients(t *testing.T) {
	svc := newTestRoutingService()

	result, err := svc.SetRoutes(context.Background(), &SetRoutesRequest{
		ExitNode: true,
	}, nil)
	require.NoError(t, err)
	assert.Empty(t, result.Warning)
}

func TestSetRoutes_EditPrefsError(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.ts = &mockRoutingTailscale{
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				return nil, errors.New("api error")
			},
		}
	})

	_, err := svc.SetRoutes(context.Background(), &SetRoutesRequest{
		Routes: []string{"10.0.0.0/24"},
	}, nil)
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrUpstream, se.Kind)
}

// --- ActivateWithKey tests ---

func TestActivateWithKey_Success(t *testing.T) {
	editCalled := false
	startCalled := false
	svc := newTestRoutingService(func(s *RoutingService) {
		s.ts = &mockRoutingTailscale{
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				editCalled = true
				assert.True(t, mp.CorpDNSSet)
				assert.False(t, mp.Prefs.CorpDNS)
				return &ipn.Prefs{}, nil
			},
			startFn: func(ctx context.Context, opts ipn.Options) error {
				startCalled = true
				assert.Equal(t, "tskey-abc123", opts.AuthKey)
				return nil
			},
		}
	})

	err := svc.ActivateWithKey(context.Background(), "tskey-abc123")
	require.NoError(t, err)
	assert.True(t, editCalled)
	assert.True(t, startCalled)
}

func TestActivateWithKey_NoIntegration(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.fw = &mockRoutingFirewall{
			integrationReadyFn: func() bool { return false },
		}
	})

	err := svc.ActivateWithKey(context.Background(), "tskey-abc123")
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrPrecondition, se.Kind)
}

func TestActivateWithKey_NilFirewall(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.fw = nil
	})

	err := svc.ActivateWithKey(context.Background(), "tskey-abc123")
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrPrecondition, se.Kind)
}

func TestActivateWithKey_EmptyKey(t *testing.T) {
	svc := newTestRoutingService()

	err := svc.ActivateWithKey(context.Background(), "")
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrValidation, se.Kind)
}

func TestActivateWithKey_InvalidPrefix(t *testing.T) {
	svc := newTestRoutingService()

	err := svc.ActivateWithKey(context.Background(), "invalid-key")
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrValidation, se.Kind)
}

func TestActivateWithKey_EditPrefsFails(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.ts = &mockRoutingTailscale{
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				return nil, errors.New("edit failed")
			},
		}
	})

	err := svc.ActivateWithKey(context.Background(), "tskey-abc123")
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrUpstream, se.Kind)
}

func TestActivateWithKey_StartFails(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.ts = &mockRoutingTailscale{
			startFn: func(ctx context.Context, opts ipn.Options) error {
				return errors.New("start failed")
			},
		}
	})

	err := svc.ActivateWithKey(context.Background(), "tskey-abc123")
	require.Error(t, err)
	var se *Error
	require.ErrorAs(t, err, &se)
	assert.Equal(t, ErrUpstream, se.Kind)
}

// --- GetSubnets tests ---

func TestGetSubnets_WithProvider(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.subnets = func() []SubnetEntry {
			return []SubnetEntry{
				{CIDR: "10.0.0.0/24", Name: "LAN", Type: "local"},
				{CIDR: "192.168.1.0/24", Name: "Guest", Type: "local"},
			}
		}
	})

	result := svc.GetSubnets()
	assert.Len(t, result, 2)
	assert.Equal(t, "10.0.0.0/24", result[0].CIDR)
	assert.Equal(t, "LAN", result[0].Name)
}

func TestGetSubnets_NilProvider(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.subnets = nil
	})

	result := svc.GetSubnets()
	assert.Empty(t, result)
}

// --- GetFirewallStatus tests ---

func TestGetFirewallStatus_WithFirewall(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.fw = &mockRoutingFirewall{
			checkTailscaleRulesPresentFn: func(ctx context.Context) (bool, bool, bool, bool) {
				return true, true, false, true
			},
		}
		s.manifest = &mockRoutingManifest{
			getTailscaleChainPrefixFn: func() string { return "CUSTOM" },
		}
		s.ic = &mockRoutingIntegration{
			hasAPIKeyFn: func() bool { return true },
		}
	})

	now := time.Now()
	state := FirewallState{
		WatcherRunning: true,
		LastRestore:    &now,
		UDAPIReachable: true,
	}

	resp := svc.GetFirewallStatus(context.Background(), state)
	assert.True(t, resp.IntegrationAPI)
	assert.Equal(t, "CUSTOM", resp.ChainPrefix)
	assert.True(t, resp.WatcherRunning)
	assert.Equal(t, &now, resp.LastRestore)
	assert.True(t, resp.RulesPresent["forward"])
	assert.True(t, resp.RulesPresent["input"])
	assert.False(t, resp.RulesPresent["output"])
	assert.True(t, resp.RulesPresent["ipset"])
	assert.True(t, resp.UDAPIReachable)
}

func TestGetFirewallStatus_NilFirewall(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.fw = nil
		s.ic = nil
	})

	resp := svc.GetFirewallStatus(context.Background(), FirewallState{})
	assert.False(t, resp.IntegrationAPI)
	assert.False(t, resp.RulesPresent["forward"])
	assert.False(t, resp.RulesPresent["input"])
	assert.False(t, resp.RulesPresent["output"])
	assert.False(t, resp.RulesPresent["ipset"])
}

func TestGetFirewallStatus_NilManifest(t *testing.T) {
	svc := newTestRoutingService(func(s *RoutingService) {
		s.manifest = nil
	})

	resp := svc.GetFirewallStatus(context.Background(), FirewallState{})
	assert.Equal(t, config.DefaultChainPrefix, resp.ChainPrefix)
}

// --- BuildRouteStatuses tests ---

func TestBuildRouteStatuses_MixedRoutes(t *testing.T) {
	routes := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/24"),
		netip.MustParsePrefix("172.16.0.0/12"),
	}
	allowed := map[string]bool{
		"10.0.0.0/24": true,
	}

	result, isExit := BuildRouteStatuses(routes, allowed)
	require.Len(t, result, 2)
	assert.Equal(t, "10.0.0.0/24", result[0].CIDR)
	assert.True(t, result[0].Approved)
	assert.Equal(t, "172.16.0.0/12", result[1].CIDR)
	assert.False(t, result[1].Approved)
	assert.False(t, isExit)
}

func TestBuildRouteStatuses_ExitNode(t *testing.T) {
	routes := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/24"),
		netip.MustParsePrefix("0.0.0.0/0"),
		netip.MustParsePrefix("::/0"),
	}

	result, isExit := BuildRouteStatuses(routes, nil)
	assert.Len(t, result, 1)
	assert.True(t, isExit)
}

func TestBuildRouteStatuses_Empty(t *testing.T) {
	result, isExit := BuildRouteStatuses(nil, nil)
	assert.Empty(t, result)
	assert.False(t, isExit)
}
