package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"

	"unifi-tailscale/manager/internal/wgs2s"
	"unifi-tailscale/manager/service"
)

func validBase64Key(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(key)
}

func createTunnelBody(t *testing.T) []byte {
	t.Helper()
	body, _ := json.Marshal(service.WgS2sCreateRequest{
		TunnelConfig: wgs2s.TunnelConfig{
			Name:          "test",
			ListenPort:    51820,
			TunnelAddress: "10.0.0.1/24",
			PeerPublicKey: validBase64Key(t),
			AllowedIPs:    []string{"10.0.0.0/24"},
		},
	})
	return body
}

func TestCreateTunnelFirewallOK(t *testing.T) {
	tunnel := &wgs2s.TunnelConfig{
		ID: "t1", Name: "test", InterfaceName: "wg-s2s0",
		ListenPort: 51820, TunnelAddress: "10.0.0.1/24",
		AllowedIPs: []string{"10.0.0.0/24"},
	}
	s := newTestServer(func(s *Server) {
		s.wgManager = &mockWgS2sControl{
			createTunnelFn: func(_ wgs2s.TunnelConfig, _ string) (*wgs2s.TunnelConfig, error) {
				return tunnel, nil
			},
			getPublicKeyFn: func(string) (string, error) { return "pubkey", nil },
		}
		s.fw = &mockFirewallService{
			integrationReadyFn: func() bool { return false },
			setupWgS2sFirewallFn: func(_ context.Context, _, _ string, _ []string) error {
				return nil
			},
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/wg-s2s/tunnels", bytes.NewReader(createTunnelBody(t)))
	w := httptest.NewRecorder()
	s.handleWgS2sCreateTunnel(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp["setupStatus"])
	assert.Nil(t, resp["firewall"])
}

func TestCreateTunnelFirewallPartial(t *testing.T) {
	tunnel := &wgs2s.TunnelConfig{
		ID: "t1", Name: "test", InterfaceName: "wg-s2s0",
		ListenPort: 51820, TunnelAddress: "10.0.0.1/24",
		AllowedIPs: []string{"10.0.0.0/24"},
	}
	s := newTestServer(func(s *Server) {
		s.wgManager = &mockWgS2sControl{
			createTunnelFn: func(_ wgs2s.TunnelConfig, _ string) (*wgs2s.TunnelConfig, error) {
				return tunnel, nil
			},
			getPublicKeyFn: func(string) (string, error) { return "pubkey", nil },
		}
		s.fw = &mockFirewallService{
			integrationReadyFn: func() bool { return false },
			setupWgS2sFirewallFn: func(_ context.Context, _, _ string, _ []string) error {
				return fmt.Errorf("UDAPI unreachable")
			},
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/wg-s2s/tunnels", bytes.NewReader(createTunnelBody(t)))
	w := httptest.NewRecorder()
	s.handleWgS2sCreateTunnel(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "partial", resp["setupStatus"])
	require.NotNil(t, resp["firewall"])
	fw := resp["firewall"].(map[string]any)
	errs := fw["errors"].([]any)
	assert.Contains(t, errs[0].(string), "UDAPI unreachable")
}

func TestEnableTunnelFirewallPartial(t *testing.T) {
	tunnel := wgs2s.TunnelConfig{
		ID: "t1", Name: "test", InterfaceName: "wg-s2s0", Enabled: true,
		ListenPort: 51820, AllowedIPs: []string{"10.0.0.0/24"},
	}
	s := newTestServer(func(s *Server) {
		s.wgManager = &mockWgS2sControl{
			enableTunnelFn: func(string) error { return nil },
			getTunnelsFn:   func() []wgs2s.TunnelConfig { return []wgs2s.TunnelConfig{tunnel} },
		}
		s.fw = &mockFirewallService{
			setupWgS2sFirewallFn: func(_ context.Context, _, _ string, _ []string) error {
				return fmt.Errorf("ipset failed")
			},
		}
	})

	req := httptest.NewRequest(http.MethodPost, "/api/wg-s2s/tunnels/t1/enable", nil)
	req.SetPathValue("id", "t1")
	w := httptest.NewRecorder()
	s.handleWgS2sEnableTunnel(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "partial", resp["setupStatus"])
}

// --- Group 1: Tailscale Core ---

func TestHandleUpSuccess(t *testing.T) {
	s := newTestServer(func(s *Server) {
		s.fw = &mockFirewallService{integrationReadyFn: func() bool { return true }}
		s.ts = &mockTailscaleControl{
			statusFn: func(context.Context) (*ipnstate.Status, error) {
				return &ipnstate.Status{BackendState: "Stopped"}, nil
			},
			editPrefsFn: func(_ context.Context, _ *ipn.MaskedPrefs) (*ipn.Prefs, error) {
				return &ipn.Prefs{}, nil
			},
		}
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tailscale/up", nil)
	w := httptest.NewRecorder()
	s.handleUp(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, true, body["ok"])
}

func TestHandleDown(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/api/tailscale/down", nil)
		w := httptest.NewRecorder()
		s.handleDown(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("upstream error", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.ts = &mockTailscaleControl{
				editPrefsFn: func(context.Context, *ipn.MaskedPrefs) (*ipn.Prefs, error) {
					return nil, fmt.Errorf("connection refused")
				},
			}
		})
		req := httptest.NewRequest(http.MethodPost, "/api/tailscale/down", nil)
		w := httptest.NewRecorder()
		s.handleDown(w, req)

		assert.Equal(t, http.StatusBadGateway, w.Code)
	})
}

func TestHandleLogin(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.fw = &mockFirewallService{integrationReadyFn: func() bool { return true }}
		})
		req := httptest.NewRequest(http.MethodPost, "/api/tailscale/login", nil)
		w := httptest.NewRecorder()
		s.handleLogin(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("precondition error", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/api/tailscale/login", nil)
		w := httptest.NewRecorder()
		s.handleLogin(w, req)

		assert.Equal(t, http.StatusPreconditionFailed, w.Code)
	})
}

func TestHandleLogout(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/api/tailscale/logout", nil)
		w := httptest.NewRecorder()
		s.handleLogout(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("upstream error", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.ts = &mockTailscaleControl{
				logoutFn: func(context.Context) error {
					return fmt.Errorf("not connected")
				},
			}
		})
		req := httptest.NewRequest(http.MethodPost, "/api/tailscale/logout", nil)
		w := httptest.NewRecorder()
		s.handleLogout(w, req)

		assert.Equal(t, http.StatusBadGateway, w.Code)
	})
}

// --- Group 2: Routing ---

func TestHandleGetRoutes(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.ts = &mockTailscaleControl{
				getPrefsFn: func(context.Context) (*ipn.Prefs, error) {
					return &ipn.Prefs{}, nil
				},
				statusFn: func(context.Context) (*ipnstate.Status, error) {
					return &ipnstate.Status{BackendState: "Running"}, nil
				},
			}
		})
		req := httptest.NewRequest(http.MethodGet, "/api/routes", nil)
		w := httptest.NewRecorder()
		s.handleGetRoutes(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Contains(t, body, "routes")
	})

	t.Run("upstream error", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.ts = &mockTailscaleControl{
				getPrefsFn: func(context.Context) (*ipn.Prefs, error) {
					return nil, fmt.Errorf("tailscaled down")
				},
			}
		})
		req := httptest.NewRequest(http.MethodGet, "/api/routes", nil)
		w := httptest.NewRecorder()
		s.handleGetRoutes(w, req)

		assert.Equal(t, http.StatusBadGateway, w.Code)
	})
}

func TestHandleSetRoutes(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.ts = &mockTailscaleControl{
				editPrefsFn: func(_ context.Context, _ *ipn.MaskedPrefs) (*ipn.Prefs, error) {
					return &ipn.Prefs{}, nil
				},
				statusFn: func(context.Context) (*ipnstate.Status, error) {
					return &ipnstate.Status{BackendState: "Running"}, nil
				},
			}
		})
		body, _ := json.Marshal(service.SetRoutesRequest{Routes: []string{"10.0.0.0/24"}})
		req := httptest.NewRequest(http.MethodPost, "/api/routes", bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.handleSetRoutes(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/api/routes", strings.NewReader("{bad"))
		w := httptest.NewRecorder()
		s.handleSetRoutes(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleAuthKey(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.fw = &mockFirewallService{integrationReadyFn: func() bool { return true }}
			s.ts = &mockTailscaleControl{
				editPrefsFn: func(_ context.Context, _ *ipn.MaskedPrefs) (*ipn.Prefs, error) {
					return &ipn.Prefs{}, nil
				},
				startFn: func(_ context.Context, _ ipn.Options) error { return nil },
			}
		})
		body, _ := json.Marshal(map[string]string{"authKey": "tskey-auth-abc123"})
		req := httptest.NewRequest(http.MethodPost, "/api/tailscale/auth-key", bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.handleAuthKey(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("empty key", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.fw = &mockFirewallService{integrationReadyFn: func() bool { return true }}
		})
		body, _ := json.Marshal(map[string]string{"authKey": ""})
		req := httptest.NewRequest(http.MethodPost, "/api/tailscale/auth-key", bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.handleAuthKey(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleGetSubnets(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/subnets", nil)
	w := httptest.NewRecorder()
	s.handleGetSubnets(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body, "subnets")
}

// --- Group 3: Settings & Diagnostics ---

func TestHandleGetSettings(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
		w := httptest.NewRecorder()
		s.handleGetSettings(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("upstream error", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.ts = &mockTailscaleControl{
				getPrefsFn: func(context.Context) (*ipn.Prefs, error) {
					return nil, fmt.Errorf("not connected")
				},
			}
		})
		req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
		w := httptest.NewRecorder()
		s.handleGetSettings(w, req)

		assert.Equal(t, http.StatusBadGateway, w.Code)
	})
}

func TestHandleSetSettings(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		hostname := "test-host"
		s := newTestServer()
		body, _ := json.Marshal(service.SettingsRequest{Hostname: &hostname})
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.handleSetSettings(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/api/settings", strings.NewReader("not json"))
		w := httptest.NewRecorder()
		s.handleSetSettings(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("validation error", func(t *testing.T) {
		badURL := "http://not-https.example.com"
		s := newTestServer()
		body, _ := json.Marshal(service.SettingsRequest{ControlURL: &badURL})
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.handleSetSettings(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleDiagnostics(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/diagnostics", nil)
	w := httptest.NewRecorder()
	s.handleDiagnostics(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body, "ipForwarding")
}

func TestHandleBugReport(t *testing.T) {
	t.Run("with note", func(t *testing.T) {
		s := newTestServer()
		body, _ := json.Marshal(map[string]string{"note": "test bug"})
		req := httptest.NewRequest(http.MethodPost, "/api/bugreport", bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		w := httptest.NewRecorder()
		s.handleBugReport(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.NotEmpty(t, resp["marker"])
	})

	t.Run("without body", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/api/bugreport", nil)
		req.ContentLength = 0
		w := httptest.NewRecorder()
		s.handleBugReport(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestHandleLogs(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil)
	w := httptest.NewRecorder()
	s.handleLogs(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body, "lines")
}

// --- Group 4: Integration API ---

func TestHandleSetIntegrationKey(t *testing.T) {
	t.Run("validation failure", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.ic = &mockIntegrationAPI{
				validateFn: func(context.Context) (*AppInfo, error) {
					return nil, fmt.Errorf("unauthorized")
				},
			}
		})
		body, _ := json.Marshal(map[string]string{"apiKey": "bad-key"})
		req := httptest.NewRequest(http.MethodPost, "/api/integration/api-key", bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.handleSetIntegrationKey(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("empty key", func(t *testing.T) {
		s := newTestServer()
		body, _ := json.Marshal(map[string]string{"apiKey": ""})
		req := httptest.NewRequest(http.MethodPost, "/api/integration/api-key", bytes.NewReader(body))
		w := httptest.NewRecorder()
		s.handleSetIntegrationKey(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid JSON", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodPost, "/api/integration/api-key", strings.NewReader("{bad"))
		w := httptest.NewRecorder()
		s.handleSetIntegrationKey(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleDeleteIntegrationKey(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodDelete, "/api/integration/api-key", nil)
		w := httptest.NewRecorder()
		s.handleDeleteIntegrationKey(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("returns OK on success", func(t *testing.T) {
		s := newTestServer()
		req := httptest.NewRequest(http.MethodDelete, "/api/integration/api-key", nil)
		w := httptest.NewRecorder()
		s.handleDeleteIntegrationKey(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, true, body["ok"])
	})
}

func TestHandleTestIntegrationKey(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodPost, "/api/integration/test", nil)
	w := httptest.NewRecorder()
	s.handleTestIntegrationKey(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body, "ok")
}

// --- Group 5: WgS2s Remaining ---

func TestHandleWgS2sListTunnels(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.wgManager = &mockWgS2sControl{
				getTunnelsFn: func() []wgs2s.TunnelConfig {
					return []wgs2s.TunnelConfig{{ID: "t1", Name: "test"}}
				},
			}
		})
		req := httptest.NewRequest(http.MethodGet, "/api/wg-s2s/tunnels", nil)
		w := httptest.NewRecorder()
		s.handleWgS2sListTunnels(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("unavailable", func(t *testing.T) {
		s := newTestServer() // wgManager is nil → Available() returns false
		req := httptest.NewRequest(http.MethodGet, "/api/wg-s2s/tunnels", nil)
		w := httptest.NewRecorder()
		s.handleWgS2sListTunnels(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

func TestHandleWgS2sUpdateTunnel(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.wgManager = &mockWgS2sControl{
				updateTunnelFn: func(id string, u wgs2s.TunnelConfig) (*wgs2s.TunnelConfig, error) {
					u.ID = id
					return &u, nil
				},
				getTunnelsFn: func() []wgs2s.TunnelConfig {
					return []wgs2s.TunnelConfig{{ID: "t1", Name: "test", Enabled: true, InterfaceName: "wg-s2s0"}}
				},
			}
		})
		body, _ := json.Marshal(wgs2s.TunnelConfig{Name: "updated"})
		req := httptest.NewRequest(http.MethodPatch, "/api/wg-s2s/tunnels/t1", bytes.NewReader(body))
		req.SetPathValue("id", "t1")
		w := httptest.NewRecorder()
		s.handleWgS2sUpdateTunnel(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("upstream error", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.wgManager = &mockWgS2sControl{
				updateTunnelFn: func(string, wgs2s.TunnelConfig) (*wgs2s.TunnelConfig, error) {
					return nil, fmt.Errorf("tunnel not found")
				},
			}
		})
		body, _ := json.Marshal(wgs2s.TunnelConfig{Name: "x"})
		req := httptest.NewRequest(http.MethodPatch, "/api/wg-s2s/tunnels/nope", bytes.NewReader(body))
		req.SetPathValue("id", "nope")
		w := httptest.NewRecorder()
		s.handleWgS2sUpdateTunnel(w, req)

		assert.Equal(t, http.StatusBadGateway, w.Code)
	})

	t.Run("conflict", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.wgManager = &mockWgS2sControl{
				getTunnelsFn: func() []wgs2s.TunnelConfig {
					return []wgs2s.TunnelConfig{{ID: "t1", Name: "test", InterfaceName: "wg-s2s0"}}
				},
			}
		})

		// Inject subnet validator that returns blocking conflict
		s.wgS2sSvc = service.NewWgS2sService(service.WgS2sConfig{
			WG:       s.wgManager,
			Manifest: &wgS2sManifestAdapter{ms: s.manifest},
			Logger:   &wgS2sLogAdapter{buf: s.logBuf},
			ValidateSubnets: func(allowedIPs []string, excludeIfaces ...string) (warnings, blocks []service.SubnetConflict) {
				return nil, []service.SubnetConflict{{CIDR: "10.0.0.0/8", ConflictsWith: "wg-s2s1", Severity: "block", Message: "overlap"}}
			},
		})

		body, _ := json.Marshal(wgs2s.TunnelConfig{AllowedIPs: []string{"10.0.0.0/8"}})
		req := httptest.NewRequest(http.MethodPatch, "/api/wg-s2s/tunnels/t1", bytes.NewReader(body))
		req.SetPathValue("id", "t1")
		w := httptest.NewRecorder()
		s.handleWgS2sUpdateTunnel(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)
		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp, "conflicts")
	})
}

func TestHandleWgS2sDeleteTunnel(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		wg := &mockWgS2sControl{
			getTunnelsFn: func() []wgs2s.TunnelConfig {
				return []wgs2s.TunnelConfig{{ID: "t1", Name: "test"}}
			},
		}
		s := newTestServer(func(s *Server) {
			s.wgManager = wg
			s.fw = nil // skip firewall teardown
		})
		// Rebuild wgS2sSvc without firewall adapter to avoid nil fwOrch panic
		s.wgS2sSvc = service.NewWgS2sService(service.WgS2sConfig{
			WG:       wg,
			Manifest: &wgS2sManifestAdapter{ms: s.manifest},
			Logger:   &wgS2sLogAdapter{buf: s.logBuf},
		})

		req := httptest.NewRequest(http.MethodDelete, "/api/wg-s2s/tunnels/t1", nil)
		req.SetPathValue("id", "t1")
		w := httptest.NewRecorder()
		s.handleWgS2sDeleteTunnel(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("not found", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.wgManager = &mockWgS2sControl{
				getTunnelsFn: func() []wgs2s.TunnelConfig { return nil },
			}
		})
		req := httptest.NewRequest(http.MethodDelete, "/api/wg-s2s/tunnels/nope", nil)
		req.SetPathValue("id", "nope")
		w := httptest.NewRecorder()
		s.handleWgS2sDeleteTunnel(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleWgS2sDisableTunnel(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.wgManager = &mockWgS2sControl{
				getTunnelsFn: func() []wgs2s.TunnelConfig {
					return []wgs2s.TunnelConfig{{ID: "t1", Name: "test"}}
				},
			}
		})
		req := httptest.NewRequest(http.MethodPost, "/api/wg-s2s/tunnels/t1/disable", nil)
		req.SetPathValue("id", "t1")
		w := httptest.NewRecorder()
		s.handleWgS2sDisableTunnel(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("not found", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.wgManager = &mockWgS2sControl{
				getTunnelsFn: func() []wgs2s.TunnelConfig { return nil },
			}
		})
		req := httptest.NewRequest(http.MethodPost, "/api/wg-s2s/tunnels/nope/disable", nil)
		req.SetPathValue("id", "nope")
		w := httptest.NewRecorder()
		s.handleWgS2sDisableTunnel(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleWgS2sGenerateKeypair(t *testing.T) {
	s := newTestServer(func(s *Server) {
		s.wgManager = &mockWgS2sControl{}
	})
	req := httptest.NewRequest(http.MethodPost, "/api/wg-s2s/generate-keypair", nil)
	w := httptest.NewRecorder()
	s.handleWgS2sGenerateKeypair(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"))

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.NotEmpty(t, body["publicKey"])
	assert.NotEmpty(t, body["privateKey"])
}

func TestHandleWgS2sGetConfig(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.wgManager = &mockWgS2sControl{
				getTunnelsFn: func() []wgs2s.TunnelConfig {
					return []wgs2s.TunnelConfig{{
						ID: "t1", Name: "test", InterfaceName: "wg-s2s0",
						ListenPort: 51820, TunnelAddress: "10.0.0.1/24",
						PeerPublicKey: validBase64Key(t),
						AllowedIPs:    []string{"10.0.0.0/24"},
					}}
				},
				getPublicKeyFn: func(string) (string, error) { return "pubkey", nil },
			}
		})
		req := httptest.NewRequest(http.MethodGet, "/api/wg-s2s/tunnels/t1/config", nil)
		req.SetPathValue("id", "t1")
		w := httptest.NewRecorder()
		s.handleWgS2sGetConfig(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var body map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.NotEmpty(t, body["config"])
	})

	t.Run("not found", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.wgManager = &mockWgS2sControl{
				getTunnelsFn: func() []wgs2s.TunnelConfig { return nil },
			}
		})
		req := httptest.NewRequest(http.MethodGet, "/api/wg-s2s/tunnels/nope/config", nil)
		req.SetPathValue("id", "nope")
		w := httptest.NewRecorder()
		s.handleWgS2sGetConfig(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestHandleWgS2sWanIP(t *testing.T) {
	s := newTestServer(func(s *Server) {
		s.wgManager = &mockWgS2sControl{}
	})
	req := httptest.NewRequest(http.MethodGet, "/api/wg-s2s/wan-ip", nil)
	w := httptest.NewRecorder()
	s.handleWgS2sWanIP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body, "ip")
}

func TestHandleWgS2sLocalSubnets(t *testing.T) {
	s := newTestServer(func(s *Server) {
		s.wgManager = &mockWgS2sControl{}
	})
	req := httptest.NewRequest(http.MethodGet, "/api/wg-s2s/local-subnets", nil)
	w := httptest.NewRecorder()
	s.handleWgS2sLocalSubnets(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- Group 6: Utility + Error Mapping ---

func TestHandleWgS2sListZones(t *testing.T) {
	s := newTestServer(func(s *Server) {
		s.wgManager = &mockWgS2sControl{}
	})
	req := httptest.NewRequest(http.MethodGet, "/api/wg-s2s/zones", nil)
	w := httptest.NewRecorder()
	s.handleWgS2sListZones(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHandleUpdateCheck(t *testing.T) {
	s := newTestServer()
	// Pre-populate cache to avoid real HTTP call
	s.updater.mu.Lock()
	s.updater.info = &UpdateInfo{Available: false, CurrentVersion: "1.0.0-test"}
	s.updater.checkedAt = time.Now()
	s.updater.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/update-check", nil)
	w := httptest.NewRecorder()
	s.handleUpdateCheck(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, false, body["available"])
	assert.Equal(t, "1.0.0-test", body["currentVersion"])
}

func TestHandleFirewallStatus(t *testing.T) {
	s := newTestServer()
	req := httptest.NewRequest(http.MethodGet, "/api/firewall", nil)
	w := httptest.NewRecorder()
	s.handleFirewallStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body, "watcherRunning")
}

func TestWriteServiceErrorMapping(t *testing.T) {
	tests := []struct {
		kind       service.ErrorKind
		wantStatus int
	}{
		{service.ErrValidation, http.StatusBadRequest},
		{service.ErrUpstream, http.StatusBadGateway},
		{service.ErrPrecondition, http.StatusPreconditionFailed},
		{service.ErrNotFound, http.StatusNotFound},
		{service.ErrUnavailable, http.StatusServiceUnavailable},
		{service.ErrorKind(99), http.StatusInternalServerError}, // unknown kind
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("kind_%d", tt.kind), func(t *testing.T) {
			w := httptest.NewRecorder()
			writeServiceError(w, &service.Error{Kind: tt.kind, Message: "test"})
			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}

	t.Run("non-service error", func(t *testing.T) {
		w := httptest.NewRecorder()
		writeServiceError(w, errors.New("raw error"))
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestWriteWgS2sErrorConflict(t *testing.T) {
	w := httptest.NewRecorder()
	writeWgS2sError(w, &service.SubnetConflictError{
		Msg:       "overlap detected",
		Conflicts: []service.SubnetConflict{{CIDR: "10.0.0.0/8", ConflictsWith: "wg-s2s1"}},
	})
	assert.Equal(t, http.StatusConflict, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "overlap detected", body["error"])
	assert.NotNil(t, body["conflicts"])
}

func TestReadJSONInvalidBody(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{bad"))
	var v map[string]string
	err := readJSON(w, req, &v)
	assert.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleHealth(t *testing.T) {
	s := newTestServer()
	s.health.RecordSuccess(WatcherTailscale)
	s.health.SetDegraded(WatcherFirewall, "key_expired")

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var snap HealthSnapshot
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &snap))
	assert.Equal(t, StatusDegraded, snap.Status)
	assert.Contains(t, snap.Watchers, "tailscale")
	assert.Contains(t, snap.Watchers, "firewall")
	assert.Equal(t, StatusHealthy, snap.Watchers["tailscale"].Status)
	assert.Equal(t, StatusDegraded, snap.Watchers["firewall"].Status)
	assert.Equal(t, "key_expired", snap.Watchers["firewall"].DegradedReason)
}
