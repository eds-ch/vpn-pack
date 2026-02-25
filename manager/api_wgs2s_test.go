package main

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
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
