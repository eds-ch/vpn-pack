package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	assert.Equal(t, "ok", resp["status"])
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
	assert.Equal(t, "partial", resp["status"])
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
	assert.Equal(t, "partial", resp["status"])
}
