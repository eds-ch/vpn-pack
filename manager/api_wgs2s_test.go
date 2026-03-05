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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unifi-tailscale/manager/internal/wgs2s"
)

func validBase64Key(t *testing.T) string {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(key)
}

func TestValidateWgS2sCreateRequest(t *testing.T) {
	validReq := func(t *testing.T) *wgS2sCreateRequest {
		t.Helper()
		return &wgS2sCreateRequest{
			TunnelConfig: wgs2s.TunnelConfig{
				Name:          "test-tunnel",
				ListenPort:    51820,
				TunnelAddress: "10.0.0.1/24",
				PeerPublicKey: validBase64Key(t),
				AllowedIPs:    []string{"10.0.0.0/24"},
			},
		}
	}

	tests := []struct {
		name    string
		modify  func(*wgS2sCreateRequest)
		wantErr bool
		errMsg  string
	}{
		{"valid minimal", func(r *wgS2sCreateRequest) {}, false, ""},
		{"missing name", func(r *wgS2sCreateRequest) { r.Name = "" }, true, "name"},
		{"zero port", func(r *wgS2sCreateRequest) { r.ListenPort = 0 }, true, "listenPort"},
		{"negative port", func(r *wgS2sCreateRequest) { r.ListenPort = -1 }, true, "listenPort"},
		{"port exceeds 65535", func(r *wgS2sCreateRequest) { r.ListenPort = 65536 }, true, "listenPort"},
		{"port at max", func(r *wgS2sCreateRequest) { r.ListenPort = 65535 }, false, ""},
		{"invalid tunnelAddress", func(r *wgS2sCreateRequest) { r.TunnelAddress = "not-cidr" }, true, "tunnelAddress"},
		{"short peerKey", func(r *wgS2sCreateRequest) { r.PeerPublicKey = "abc" }, true, "peerPublicKey"},
		{"bad base64 peerKey", func(r *wgS2sCreateRequest) { r.PeerPublicKey = "!!!not-base64-at-all-but-is-44-chars-long!==" }, true, "peerPublicKey"},
		{"invalid allowedIP", func(r *wgS2sCreateRequest) { r.AllowedIPs = []string{"bad"} }, true, "allowedIP"},
		{"valid multiple CIDRs", func(r *wgS2sCreateRequest) { r.AllowedIPs = []string{"10.0.0.0/24", "192.168.1.0/24"} }, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validReq(t)
			tt.modify(req)
			err := validateWgS2sCreateRequest(req)
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

func TestValidateWgS2sUpdateRequest(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*wgs2s.TunnelConfig)
		wantErr bool
		errMsg  string
	}{
		{"empty update", func(c *wgs2s.TunnelConfig) {}, false, ""},
		{"valid tunnelAddress", func(c *wgs2s.TunnelConfig) { c.TunnelAddress = "10.0.0.1/24" }, false, ""},
		{"invalid tunnelAddress", func(c *wgs2s.TunnelConfig) { c.TunnelAddress = "not-cidr" }, true, "tunnelAddress"},
		{"valid peerPublicKey", func(c *wgs2s.TunnelConfig) { c.PeerPublicKey = validBase64Key(t) }, false, ""},
		{"invalid peerPublicKey", func(c *wgs2s.TunnelConfig) { c.PeerPublicKey = "short" }, true, "peerPublicKey"},
		{"valid allowedIPs", func(c *wgs2s.TunnelConfig) { c.AllowedIPs = []string{"10.0.0.0/24"} }, false, ""},
		{"invalid allowedIP", func(c *wgs2s.TunnelConfig) { c.AllowedIPs = []string{"bad"} }, true, "allowedIP"},
		{"negative port", func(c *wgs2s.TunnelConfig) { c.ListenPort = -1 }, true, "listenPort"},
		{"port exceeds 65535", func(c *wgs2s.TunnelConfig) { c.ListenPort = 65536 }, true, "listenPort"},
		{"zero port (no change)", func(c *wgs2s.TunnelConfig) { c.ListenPort = 0 }, false, ""},
		{"positive port", func(c *wgs2s.TunnelConfig) { c.ListenPort = 51821 }, false, ""},
		{"port at max", func(c *wgs2s.TunnelConfig) { c.ListenPort = 65535 }, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := wgs2s.TunnelConfig{}
			tt.modify(&cfg)
			err := validateWgS2sUpdateRequest(&cfg)
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

func createTunnelBody(t *testing.T) []byte {
	t.Helper()
	body, _ := json.Marshal(wgS2sCreateRequest{
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
	var resp TunnelResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp.Status)
	assert.Nil(t, resp.Firewall)
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
	var resp TunnelResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "partial", resp.Status)
	require.NotNil(t, resp.Firewall)
	assert.Contains(t, resp.Firewall.Errors[0], "UDAPI unreachable")
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
