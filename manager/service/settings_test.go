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
	"tailscale.com/types/key"
	"tailscale.com/types/views"
)

func ptr[T any](v T) *T { return &v }

// --- Mocks ---

type mockTailscalePrefs struct {
	getPrefsFn  func(ctx context.Context) (*ipn.Prefs, error)
	editPrefsFn func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error)
	statusFn    func(ctx context.Context) (*ipnstate.Status, error)
}

func (m *mockTailscalePrefs) GetPrefs(ctx context.Context) (*ipn.Prefs, error) {
	if m.getPrefsFn != nil {
		return m.getPrefsFn(ctx)
	}
	return &ipn.Prefs{}, nil
}

func (m *mockTailscalePrefs) EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	if m.editPrefsFn != nil {
		return m.editPrefsFn(ctx, mp)
	}
	return &ipn.Prefs{Hostname: mp.Hostname}, nil
}

func (m *mockTailscalePrefs) Status(ctx context.Context) (*ipnstate.Status, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx)
	}
	return &ipnstate.Status{BackendState: "Running"}, nil
}

type mockSettingsFirewall struct {
	integrationReadyFn      func() bool
	ensureDNSForwardingFn   func(ctx context.Context, suffix string) error
	removeDNSForwardingFn   func(ctx context.Context) error
	openWanPortFn           func(ctx context.Context, port int, marker string) error
	closeWanPortFn          func(ctx context.Context, port int, marker string) error
	restoreRulesWithRetryFn func(ctx context.Context, retries int, delay time.Duration)
}

func (m *mockSettingsFirewall) IntegrationReady() bool {
	if m.integrationReadyFn != nil {
		return m.integrationReadyFn()
	}
	return false
}

func (m *mockSettingsFirewall) EnsureDNSForwarding(ctx context.Context, suffix string) error {
	if m.ensureDNSForwardingFn != nil {
		return m.ensureDNSForwardingFn(ctx, suffix)
	}
	return nil
}

func (m *mockSettingsFirewall) RemoveDNSForwarding(ctx context.Context) error {
	if m.removeDNSForwardingFn != nil {
		return m.removeDNSForwardingFn(ctx)
	}
	return nil
}

func (m *mockSettingsFirewall) OpenWanPort(ctx context.Context, port int, marker string) error {
	if m.openWanPortFn != nil {
		return m.openWanPortFn(ctx, port, marker)
	}
	return nil
}

func (m *mockSettingsFirewall) CloseWanPort(ctx context.Context, port int, marker string) error {
	if m.closeWanPortFn != nil {
		return m.closeWanPortFn(ctx, port, marker)
	}
	return nil
}

func (m *mockSettingsFirewall) RestoreRulesWithRetry(ctx context.Context, retries int, delay time.Duration) {
	if m.restoreRulesWithRetryFn != nil {
		m.restoreRulesWithRetryFn(ctx, retries, delay)
	}
}

type mockSettingsIntegration struct {
	hasAPIKeyFn func() bool
}

func (m *mockSettingsIntegration) HasAPIKey() bool {
	if m.hasAPIKeyFn != nil {
		return m.hasAPIKeyFn()
	}
	return false
}

type mockSettingsManifest struct {
	hasDNSPolicyFn func(marker string) bool
	wanPortFn      func(marker string) (int, bool)
}

func (m *mockSettingsManifest) HasDNSPolicy(marker string) bool {
	if m.hasDNSPolicyFn != nil {
		return m.hasDNSPolicyFn(marker)
	}
	return false
}

func (m *mockSettingsManifest) WanPort(marker string) (int, bool) {
	if m.wanPortFn != nil {
		return m.wanPortFn(marker)
	}
	return 0, false
}

type mockSettingsNotifier struct {
	restartCalled  bool
	dnsChangedWith *bool
}

func (m *mockSettingsNotifier) OnRestartRequired() {
	m.restartCalled = true
}

func (m *mockSettingsNotifier) OnDNSChanged(enabled bool) {
	m.dnsChangedWith = &enabled
}

// --- Test helpers ---

func newTestSettingsService(opts ...func(*SettingsService)) *SettingsService {
	svc := &SettingsService{
		ts:       &mockTailscalePrefs{},
		fw:       &mockSettingsFirewall{},
		ic:       &mockSettingsIntegration{},
		manifest: &mockSettingsManifest{},
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// --- Pure function tests (moved from api_settings_test.go) ---

func TestValidateControlURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{"empty string", "", false, ""},
		{"valid https", "https://example.com", false, ""},
		{"valid https with path", "https://ex.com/path", false, ""},
		{"http rejected", "http://ex.com", true, "HTTPS"},
		{"no scheme", "example.com", true, "HTTPS"},
		{"empty host", "https://", true, "host"},
		{"broken url", "://broken", true, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateControlURL(tt.input)
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

func TestParseAddrPorts(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantLen int
		wantErr bool
	}{
		{"empty string", "", 0, false},
		{"single v4", "1.2.3.4:4321", 1, false},
		{"two addrs", "1.2.3.4:80, 5.6.7.8:443", 2, false},
		{"trimmed spaces", " 1.2.3.4:80 ", 1, false},
		{"not an addr", "not-addr", 0, true},
		{"missing port", "1.2.3.4", 0, true},
		{"ipv6", "[::1]:8080", 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseAddrPorts(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.wantLen == 0 {
					assert.Nil(t, result)
				} else {
					assert.Len(t, result, tt.wantLen)
				}
			}
		})
	}
}

func TestFormatAddrPorts(t *testing.T) {
	tests := []struct {
		name string
		in   []netip.AddrPort
		want string
	}{
		{"nil", nil, ""},
		{"empty", []netip.AddrPort{}, ""},
		{"single", []netip.AddrPort{netip.MustParseAddrPort("1.2.3.4:80")}, "1.2.3.4:80"},
		{"two", []netip.AddrPort{
			netip.MustParseAddrPort("1.2.3.4:80"),
			netip.MustParseAddrPort("5.6.7.8:443"),
		}, "1.2.3.4:80, 5.6.7.8:443"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, FormatAddrPorts(tt.in))
		})
	}
}

func TestValidateTag(t *testing.T) {
	tests := []struct {
		name    string
		tag     string
		wantErr bool
		errMsg  string
	}{
		{"valid", "tag:web", false, ""},
		{"valid with dash", "tag:web-server", false, ""},
		{"valid with number", "tag:server1", false, ""},
		{"no prefix", "web", true, "tag:"},
		{"empty name", "tag:", true, "empty"},
		{"starts with number", "tag:1abc", true, "letter"},
		{"invalid char", "tag:web_server", true, "letters, numbers, or dashes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTag(tt.tag)
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

func TestBuildMaskedPrefs(t *testing.T) {
	t.Run("empty request", func(t *testing.T) {
		req := &SettingsRequest{}
		mp := BuildMaskedPrefs(req, nil)
		assert.False(t, mp.HostnameSet)
		assert.False(t, mp.CorpDNSSet)
		assert.False(t, mp.RouteAllSet)
		assert.False(t, mp.ShieldsUpSet)
		assert.False(t, mp.RunSSHSet)
		assert.False(t, mp.ControlURLSet)
		assert.False(t, mp.NoSNATSet)
	})

	t.Run("hostname set", func(t *testing.T) {
		req := &SettingsRequest{Hostname: ptr("myhost")}
		mp := BuildMaskedPrefs(req, nil)
		assert.True(t, mp.HostnameSet)
		assert.Equal(t, "myhost", mp.Hostname)
	})

	t.Run("shieldsUp set", func(t *testing.T) {
		req := &SettingsRequest{ShieldsUp: ptr(true)}
		mp := BuildMaskedPrefs(req, nil)
		assert.True(t, mp.ShieldsUpSet)
		assert.True(t, mp.ShieldsUp)
	})

	t.Run("acceptDNS never maps to CorpDNS", func(t *testing.T) {
		req := &SettingsRequest{AcceptDNS: ptr(true)}
		mp := BuildMaskedPrefs(req, nil)
		assert.False(t, mp.CorpDNSSet, "CorpDNS must never be set via settings API")
	})

	t.Run("relay server port set", func(t *testing.T) {
		req := &SettingsRequest{RelayServerPort: ptr(3478)}
		mp := BuildMaskedPrefs(req, nil)
		assert.True(t, mp.RelayServerPortSet)
		require.NotNil(t, mp.RelayServerPort)
		assert.Equal(t, uint16(3478), *mp.RelayServerPort)
	})

	t.Run("relay server port disabled", func(t *testing.T) {
		req := &SettingsRequest{RelayServerPort: ptr(-1)}
		mp := BuildMaskedPrefs(req, nil)
		assert.True(t, mp.RelayServerPortSet)
		assert.Nil(t, mp.RelayServerPort)
	})

	t.Run("advertise tags set", func(t *testing.T) {
		req := &SettingsRequest{AdvertiseTags: &[]string{"tag:web", "tag:prod"}}
		mp := BuildMaskedPrefs(req, nil)
		assert.True(t, mp.AdvertiseTagsSet)
		assert.Equal(t, []string{"tag:web", "tag:prod"}, mp.AdvertiseTags)
	})

	t.Run("relay endpoints set", func(t *testing.T) {
		eps := []netip.AddrPort{netip.MustParseAddrPort("1.2.3.4:3478")}
		req := &SettingsRequest{RelayServerEndpoints: ptr("1.2.3.4:3478")}
		mp := BuildMaskedPrefs(req, eps)
		assert.True(t, mp.RelayServerStaticEndpointsSet)
		assert.Equal(t, eps, mp.RelayServerStaticEndpoints)
	})
}

// --- Service method tests ---

func TestGetSettings(t *testing.T) {
	t.Run("happy path", func(t *testing.T) {
		svc := newTestSettingsService(func(s *SettingsService) {
			s.ts = &mockTailscalePrefs{
				getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
					return &ipn.Prefs{Hostname: "test-host", ControlURL: "https://login.tailscale.com"}, nil
				},
			}
		})
		resp, err := svc.GetSettings(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "test-host", resp.Hostname)
		assert.Equal(t, "https://login.tailscale.com", resp.ControlURL)
	})

	t.Run("tailscale error", func(t *testing.T) {
		svc := newTestSettingsService(func(s *SettingsService) {
			s.ts = &mockTailscalePrefs{
				getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
					return nil, errors.New("connection refused")
				},
			}
		})
		_, err := svc.GetSettings(context.Background())
		require.Error(t, err)
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Equal(t, ErrUpstream, se.Kind)
	})

	t.Run("acceptDNS from manifest", func(t *testing.T) {
		svc := newTestSettingsService(func(s *SettingsService) {
			s.ts = &mockTailscalePrefs{
				getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
					return &ipn.Prefs{CorpDNS: false}, nil
				},
			}
			s.manifest = &mockSettingsManifest{
				hasDNSPolicyFn: func(marker string) bool {
					return marker == config.DNSMarkerTailscale
				},
			}
		})
		resp, err := svc.GetSettings(context.Background())
		require.NoError(t, err)
		assert.True(t, resp.AcceptDNS, "AcceptDNS should come from manifest, not prefs")
	})
}

func TestSetSettings(t *testing.T) {
	t.Run("simple hostname change", func(t *testing.T) {
		svc := newTestSettingsService(func(s *SettingsService) {
			s.ts = &mockTailscalePrefs{
				editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
					return &ipn.Prefs{Hostname: mp.Hostname}, nil
				},
			}
		})
		req := &SettingsRequest{Hostname: ptr("new-host")}
		result, err := svc.SetSettings(context.Background(), req)
		require.NoError(t, err)
		assert.Equal(t, "new-host", result.Response.Hostname)
		assert.False(t, result.NeedsRestart)
		assert.False(t, result.DNSChanged)
	})

	t.Run("validation error: invalid port", func(t *testing.T) {
		svc := newTestSettingsService()
		req := &SettingsRequest{UDPPort: ptr(0)}
		_, err := svc.SetSettings(context.Background(), req)
		require.Error(t, err)
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Equal(t, ErrValidation, se.Kind)
		assert.Contains(t, se.Message, "UDP port")
	})

	t.Run("validation error: acceptDNS without integration", func(t *testing.T) {
		svc := newTestSettingsService()
		req := &SettingsRequest{AcceptDNS: ptr(true)}
		_, err := svc.SetSettings(context.Background(), req)
		require.Error(t, err)
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Equal(t, ErrValidation, se.Kind)
		assert.Contains(t, se.Message, "Integration API")
	})

	t.Run("DNS forwarding toggled sets DNSChanged", func(t *testing.T) {
		svc := newTestSettingsService(func(s *SettingsService) {
			s.fw = &mockSettingsFirewall{
				integrationReadyFn: func() bool { return true },
				ensureDNSForwardingFn: func(ctx context.Context, suffix string) error {
					return nil
				},
			}
			s.ts = &mockTailscalePrefs{
				statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
					return &ipnstate.Status{
						CurrentTailnet: &ipnstate.TailnetStatus{MagicDNSSuffix: "example.ts.net"},
					}, nil
				},
				editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
					return &ipn.Prefs{}, nil
				},
			}
		})
		req := &SettingsRequest{AcceptDNS: ptr(true)}
		result, err := svc.SetSettings(context.Background(), req)
		require.NoError(t, err)
		assert.True(t, result.DNSChanged)
	})

	t.Run("control URL change requires restart", func(t *testing.T) {
		svc := newTestSettingsService(func(s *SettingsService) {
			s.ts = &mockTailscalePrefs{
				getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
					return &ipn.Prefs{ControlURL: "https://old.example.com"}, nil
				},
				statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
					return &ipnstate.Status{BackendState: "Stopped"}, nil
				},
				editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
					return &ipn.Prefs{ControlURL: mp.ControlURL}, nil
				},
			}
		})
		req := &SettingsRequest{ControlURL: ptr("https://new.example.com")}
		result, err := svc.SetSettings(context.Background(), req)
		require.NoError(t, err)
		assert.True(t, result.NeedsRestart)
	})

	t.Run("control URL same no restart", func(t *testing.T) {
		svc := newTestSettingsService(func(s *SettingsService) {
			s.ts = &mockTailscalePrefs{
				getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
					return &ipn.Prefs{ControlURL: "https://same.example.com"}, nil
				},
				statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
					return &ipnstate.Status{BackendState: "Stopped"}, nil
				},
				editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
					return &ipn.Prefs{ControlURL: mp.ControlURL}, nil
				},
			}
		})
		req := &SettingsRequest{ControlURL: ptr("https://same.example.com")}
		result, err := svc.SetSettings(context.Background(), req)
		require.NoError(t, err)
		assert.False(t, result.NeedsRestart)
	})

	t.Run("must logout before changing control URL while running", func(t *testing.T) {
		svc := newTestSettingsService(func(s *SettingsService) {
			s.ts = &mockTailscalePrefs{
				getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
					return &ipn.Prefs{ControlURL: "https://old.example.com"}, nil
				},
				statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
					return &ipnstate.Status{BackendState: "Running"}, nil
				},
			}
		})
		req := &SettingsRequest{ControlURL: ptr("https://new.example.com")}
		_, err := svc.SetSettings(context.Background(), req)
		require.Error(t, err)
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Equal(t, ErrValidation, se.Kind)
		assert.Contains(t, se.Message, "log out")
	})

	t.Run("DNS disable forwarding", func(t *testing.T) {
		removeCalled := false
		svc := newTestSettingsService(func(s *SettingsService) {
			s.fw = &mockSettingsFirewall{
				removeDNSForwardingFn: func(ctx context.Context) error {
					removeCalled = true
					return nil
				},
			}
			s.ts = &mockTailscalePrefs{
				editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
					return &ipn.Prefs{}, nil
				},
			}
		})
		req := &SettingsRequest{AcceptDNS: ptr(false)}
		result, err := svc.SetSettings(context.Background(), req)
		require.NoError(t, err)
		assert.True(t, removeCalled)
		assert.True(t, result.DNSChanged)
	})

	t.Run("invalid tag rejected", func(t *testing.T) {
		svc := newTestSettingsService()
		req := &SettingsRequest{AdvertiseTags: &[]string{"invalid"}}
		_, err := svc.SetSettings(context.Background(), req)
		require.Error(t, err)
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Equal(t, ErrValidation, se.Kind)
	})

	t.Run("invalid relay endpoints rejected", func(t *testing.T) {
		svc := newTestSettingsService()
		req := &SettingsRequest{RelayServerEndpoints: ptr("not-valid")}
		_, err := svc.SetSettings(context.Background(), req)
		require.Error(t, err)
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Equal(t, ErrValidation, se.Kind)
	})

	t.Run("relay port validation", func(t *testing.T) {
		svc := newTestSettingsService()
		req := &SettingsRequest{RelayServerPort: ptr(-2)}
		_, err := svc.SetSettings(context.Background(), req)
		require.Error(t, err)
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Equal(t, ErrValidation, se.Kind)
		assert.Contains(t, se.Message, "Relay server port")
	})
}

func TestApplyDNSForwarding(t *testing.T) {
	t.Run("enable requires connected tailnet", func(t *testing.T) {
		svc := newTestSettingsService(func(s *SettingsService) {
			s.ts = &mockTailscalePrefs{
				statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
					return &ipnstate.Status{}, nil // no CurrentTailnet
				},
			}
		})
		err := svc.applyDNSForwarding(context.Background(), true)
		require.Error(t, err)
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Equal(t, ErrValidation, se.Kind)
		assert.Contains(t, se.Message, "DNS suffix")
	})

	t.Run("enable success", func(t *testing.T) {
		ensureCalled := false
		svc := newTestSettingsService(func(s *SettingsService) {
			s.ts = &mockTailscalePrefs{
				statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
					return &ipnstate.Status{
						CurrentTailnet: &ipnstate.TailnetStatus{MagicDNSSuffix: "example.ts.net"},
					}, nil
				},
			}
			s.fw = &mockSettingsFirewall{
				ensureDNSForwardingFn: func(ctx context.Context, suffix string) error {
					assert.Equal(t, "example.ts.net", suffix)
					ensureCalled = true
					return nil
				},
			}
		})
		err := svc.applyDNSForwarding(context.Background(), true)
		require.NoError(t, err)
		assert.True(t, ensureCalled)
	})

	t.Run("disable success", func(t *testing.T) {
		removeCalled := false
		svc := newTestSettingsService(func(s *SettingsService) {
			s.fw = &mockSettingsFirewall{
				removeDNSForwardingFn: func(ctx context.Context) error {
					removeCalled = true
					return nil
				},
			}
		})
		err := svc.applyDNSForwarding(context.Background(), false)
		require.NoError(t, err)
		assert.True(t, removeCalled)
	})

	t.Run("firewall error", func(t *testing.T) {
		svc := newTestSettingsService(func(s *SettingsService) {
			s.ts = &mockTailscalePrefs{
				statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
					return &ipnstate.Status{
						CurrentTailnet: &ipnstate.TailnetStatus{MagicDNSSuffix: "example.ts.net"},
					}, nil
				},
			}
			s.fw = &mockSettingsFirewall{
				ensureDNSForwardingFn: func(ctx context.Context, suffix string) error {
					return errors.New("firewall error")
				},
			}
		})
		err := svc.applyDNSForwarding(context.Background(), true)
		require.Error(t, err)
		var se *Error
		require.ErrorAs(t, err, &se)
		assert.Equal(t, ErrInternal, se.Kind)
	})
}

func TestWanPortRules(t *testing.T) {
	t.Run("relay port update calls swap", func(t *testing.T) {
		var closedPort, openedPort int
		svc := newTestSettingsService(func(s *SettingsService) {
			s.ic = &mockSettingsIntegration{hasAPIKeyFn: func() bool { return true }}
			s.fw = &mockSettingsFirewall{
				closeWanPortFn: func(ctx context.Context, port int, marker string) error {
					closedPort = port
					return nil
				},
				openWanPortFn: func(ctx context.Context, port int, marker string) error {
					openedPort = port
					return nil
				},
				restoreRulesWithRetryFn: func(ctx context.Context, retries int, delay time.Duration) {},
			}
		})
		oldRelay := uint16(3478)
		svc.updateRelayPortRules(context.Background(), ptr(4000), &oldRelay)
		assert.Equal(t, 3478, closedPort)
		assert.Equal(t, 4000, openedPort)
	})

	t.Run("no API key skips update", func(t *testing.T) {
		called := false
		svc := newTestSettingsService(func(s *SettingsService) {
			s.fw = &mockSettingsFirewall{
				openWanPortFn: func(ctx context.Context, port int, marker string) error {
					called = true
					return nil
				},
			}
		})
		svc.updateRelayPortRules(context.Background(), ptr(4000), nil)
		assert.False(t, called)
	})

	t.Run("tailscale WG port skips if same", func(t *testing.T) {
		called := false
		svc := newTestSettingsService(func(s *SettingsService) {
			s.ic = &mockSettingsIntegration{hasAPIKeyFn: func() bool { return true }}
			s.manifest = &mockSettingsManifest{
				wanPortFn: func(marker string) (int, bool) { return 41641, true },
			}
			s.fw = &mockSettingsFirewall{
				openWanPortFn: func(ctx context.Context, port int, marker string) error {
					called = true
					return nil
				},
			}
		})
		svc.updateTailscaleWgPortRules(context.Background(), ptr(41641))
		assert.False(t, called)
	})
}

// --- Notifier tests ---

func TestSetSettings_NotifiesOnRestart(t *testing.T) {
	n := &mockSettingsNotifier{}
	svc := newTestSettingsService(func(s *SettingsService) {
		s.notify = n
		s.ts = &mockTailscalePrefs{
			getPrefsFn: func(ctx context.Context) (*ipn.Prefs, error) {
				return &ipn.Prefs{ControlURL: "https://old.example.com"}, nil
			},
			statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
				return &ipnstate.Status{BackendState: "Stopped"}, nil
			},
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				return &ipn.Prefs{ControlURL: mp.ControlURL}, nil
			},
		}
	})
	req := &SettingsRequest{ControlURL: ptr("https://new.example.com")}
	_, err := svc.SetSettings(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, n.restartCalled)
	assert.Nil(t, n.dnsChangedWith)
}

func TestSetSettings_NotifiesOnDNSChange(t *testing.T) {
	n := &mockSettingsNotifier{}
	svc := newTestSettingsService(func(s *SettingsService) {
		s.notify = n
		s.fw = &mockSettingsFirewall{
			integrationReadyFn: func() bool { return true },
			ensureDNSForwardingFn: func(ctx context.Context, suffix string) error {
				return nil
			},
		}
		s.ts = &mockTailscalePrefs{
			statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
				return &ipnstate.Status{
					CurrentTailnet: &ipnstate.TailnetStatus{MagicDNSSuffix: "example.ts.net"},
				}, nil
			},
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				return &ipn.Prefs{}, nil
			},
		}
	})
	req := &SettingsRequest{AcceptDNS: ptr(true)}
	_, err := svc.SetSettings(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, n.restartCalled)
	require.NotNil(t, n.dnsChangedWith)
}

func TestSetSettings_NilNotifierSafe(t *testing.T) {
	svc := newTestSettingsService()
	req := &SettingsRequest{Hostname: ptr("test")}
	_, err := svc.SetSettings(context.Background(), req)
	require.NoError(t, err)
}

// --- Error type tests ---

func TestErrorTypes(t *testing.T) {
	t.Run("validation error", func(t *testing.T) {
		err := validationError("bad input")
		assert.Equal(t, ErrValidation, err.Kind)
		assert.Equal(t, "bad input", err.Error())
		assert.Nil(t, err.Unwrap())
	})

	t.Run("upstream error with cause", func(t *testing.T) {
		cause := errors.New("connection refused")
		err := upstreamError("humanized message", cause)
		assert.Equal(t, ErrUpstream, err.Kind)
		assert.Equal(t, "humanized message", err.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("errors.As works", func(t *testing.T) {
		var e error = validationError("test")
		var se *Error
		assert.True(t, errors.As(e, &se))
		assert.Equal(t, ErrValidation, se.Kind)
	})
}

// --- Accept-routes S2S conflict validation ---

func peerWithRoutes(hostname string, routes ...string) *ipnstate.PeerStatus {
	prefixes := make([]netip.Prefix, len(routes))
	for i, r := range routes {
		prefixes[i] = netip.MustParsePrefix(r)
	}
	pr := views.SliceOf(prefixes)
	return &ipnstate.PeerStatus{
		HostName:      hostname,
		PrimaryRoutes: &pr,
	}
}

func TestValidateAcceptRoutes_ConflictDetected(t *testing.T) {
	svc := newTestSettingsService(func(s *SettingsService) {
		s.s2sTunnels = func(_ context.Context) []S2sTunnelInfo {
			return []S2sTunnelInfo{
				{Name: "office-vpn", AllowedIPs: []string{"10.20.0.0/24", "172.16.0.0/16"}},
			}
		}
		s.ts = &mockTailscalePrefs{
			statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
				return &ipnstate.Status{
					Peer: map[key.NodePublic]*ipnstate.PeerStatus{
						{}: peerWithRoutes("peer-a", "10.20.0.0/24"),
					},
				}, nil
			},
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				return &ipn.Prefs{RouteAll: true}, nil
			},
		}
	})
	req := &SettingsRequest{AcceptRoutes: ptr(true)}
	result, err := svc.SetSettings(context.Background(), req)
	require.NoError(t, err)

	require.Len(t, result.AcceptRoutesWarnings, 1)
	w := result.AcceptRoutesWarnings[0]
	assert.Equal(t, "10.20.0.0/24", w.CIDR)
	assert.Contains(t, w.ConflictsWith, "office-vpn")
	assert.Contains(t, w.Message, "table 52")
	assert.Contains(t, w.Message, "peer-a")
	assert.Equal(t, "warn", w.Severity)

	assert.Equal(t, result.Response.Warnings, result.AcceptRoutesWarnings)
}

func TestValidateAcceptRoutes_SupersetConflict(t *testing.T) {
	svc := newTestSettingsService(func(s *SettingsService) {
		s.s2sTunnels = func(_ context.Context) []S2sTunnelInfo {
			return []S2sTunnelInfo{
				{Name: "branch", AllowedIPs: []string{"10.20.5.0/24"}},
			}
		}
		s.ts = &mockTailscalePrefs{
			statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
				return &ipnstate.Status{
					Peer: map[key.NodePublic]*ipnstate.PeerStatus{
						{}: peerWithRoutes("peer-b", "10.20.0.0/16"),
					},
				}, nil
			},
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				return &ipn.Prefs{RouteAll: true}, nil
			},
		}
	})
	req := &SettingsRequest{AcceptRoutes: ptr(true)}
	result, err := svc.SetSettings(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, result.AcceptRoutesWarnings, 1)
	assert.Equal(t, "10.20.0.0/16", result.AcceptRoutesWarnings[0].CIDR)
	assert.Contains(t, result.AcceptRoutesWarnings[0].ConflictsWith, "branch")
}

func TestValidateAcceptRoutes_MultipleConflicts(t *testing.T) {
	k1 := key.NewNode().Public()
	k2 := key.NewNode().Public()
	svc := newTestSettingsService(func(s *SettingsService) {
		s.s2sTunnels = func(_ context.Context) []S2sTunnelInfo {
			return []S2sTunnelInfo{
				{Name: "tunnel-a", AllowedIPs: []string{"10.20.0.0/24"}},
				{Name: "tunnel-b", AllowedIPs: []string{"192.168.50.0/24"}},
			}
		}
		s.ts = &mockTailscalePrefs{
			statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
				return &ipnstate.Status{
					Peer: map[key.NodePublic]*ipnstate.PeerStatus{
						k1: peerWithRoutes("peer-1", "10.20.0.0/24"),
						k2: peerWithRoutes("peer-2", "192.168.50.0/24"),
					},
				}, nil
			},
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				return &ipn.Prefs{RouteAll: true}, nil
			},
		}
	})
	req := &SettingsRequest{AcceptRoutes: ptr(true)}
	result, err := svc.SetSettings(context.Background(), req)
	require.NoError(t, err)
	assert.Len(t, result.AcceptRoutesWarnings, 2)
}

func TestValidateAcceptRoutes_NoConflict(t *testing.T) {
	svc := newTestSettingsService(func(s *SettingsService) {
		s.s2sTunnels = func(_ context.Context) []S2sTunnelInfo {
			return []S2sTunnelInfo{
				{Name: "tunnel-x", AllowedIPs: []string{"10.20.0.0/24"}},
			}
		}
		s.ts = &mockTailscalePrefs{
			statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
				return &ipnstate.Status{
					Peer: map[key.NodePublic]*ipnstate.PeerStatus{
						{}: peerWithRoutes("peer-c", "172.16.0.0/16"),
					},
				}, nil
			},
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				return &ipn.Prefs{RouteAll: true}, nil
			},
		}
	})
	req := &SettingsRequest{AcceptRoutes: ptr(true)}
	result, err := svc.SetSettings(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, result.AcceptRoutesWarnings)
	assert.Empty(t, result.Response.Warnings)
}

func TestValidateAcceptRoutes_NoS2sTunnels(t *testing.T) {
	svc := newTestSettingsService(func(s *SettingsService) {
		s.s2sTunnels = func(_ context.Context) []S2sTunnelInfo { return nil }
	})
	req := &SettingsRequest{AcceptRoutes: ptr(true)}
	result, err := svc.SetSettings(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, result.AcceptRoutesWarnings)
}

func TestValidateAcceptRoutes_NilProvider(t *testing.T) {
	svc := newTestSettingsService()
	req := &SettingsRequest{AcceptRoutes: ptr(true)}
	result, err := svc.SetSettings(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, result.AcceptRoutesWarnings)
}

func TestValidateAcceptRoutes_DisablingSkipsValidation(t *testing.T) {
	called := false
	svc := newTestSettingsService(func(s *SettingsService) {
		s.s2sTunnels = func(_ context.Context) []S2sTunnelInfo {
			called = true
			return []S2sTunnelInfo{{Name: "t", AllowedIPs: []string{"10.0.0.0/8"}}}
		}
	})
	req := &SettingsRequest{AcceptRoutes: ptr(false)}
	_, err := svc.SetSettings(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, called, "S2S tunnel provider should not be called when disabling accept-routes")
}

func TestValidateAcceptRoutes_NoPeersAdvertisingRoutes(t *testing.T) {
	svc := newTestSettingsService(func(s *SettingsService) {
		s.s2sTunnels = func(_ context.Context) []S2sTunnelInfo {
			return []S2sTunnelInfo{
				{Name: "tunnel", AllowedIPs: []string{"10.20.0.0/24"}},
			}
		}
		s.ts = &mockTailscalePrefs{
			statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
				return &ipnstate.Status{
					Peer: map[key.NodePublic]*ipnstate.PeerStatus{
						{}: {HostName: "peer-no-routes"},
					},
				}, nil
			},
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				return &ipn.Prefs{RouteAll: true}, nil
			},
		}
	})
	req := &SettingsRequest{AcceptRoutes: ptr(true)}
	result, err := svc.SetSettings(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, result.AcceptRoutesWarnings)
}

func TestValidateAcceptRoutes_StatusErrorGraceful(t *testing.T) {
	svc := newTestSettingsService(func(s *SettingsService) {
		s.s2sTunnels = func(_ context.Context) []S2sTunnelInfo {
			return []S2sTunnelInfo{
				{Name: "tunnel", AllowedIPs: []string{"10.20.0.0/24"}},
			}
		}
		s.ts = &mockTailscalePrefs{
			statusFn: func(ctx context.Context) (*ipnstate.Status, error) {
				return nil, errors.New("connection refused")
			},
			editPrefsFn: func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				return &ipn.Prefs{RouteAll: true}, nil
			},
		}
	})
	req := &SettingsRequest{AcceptRoutes: ptr(true)}
	result, err := svc.SetSettings(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, result.AcceptRoutesWarnings, "status error should not block accept-routes")
}
