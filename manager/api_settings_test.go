package main

import (
	"net/netip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
			err := validateControlURL(tt.input)
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
			result, err := parseAddrPorts(tt.input)
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
			assert.Equal(t, tt.want, formatAddrPorts(tt.in))
		})
	}
}

func TestBuildMaskedPrefs(t *testing.T) {
	t.Run("empty request", func(t *testing.T) {
		req := &settingsRequest{}
		mp := buildMaskedPrefs(req, nil)
		assert.False(t, mp.HostnameSet)
		assert.False(t, mp.RouteAllSet)
		assert.False(t, mp.ShieldsUpSet)
		assert.False(t, mp.RunSSHSet)
		assert.False(t, mp.ControlURLSet)
		assert.False(t, mp.NoSNATSet)
	})

	t.Run("hostname set", func(t *testing.T) {
		req := &settingsRequest{Hostname: ptr("myhost")}
		mp := buildMaskedPrefs(req, nil)
		assert.True(t, mp.HostnameSet)
		assert.Equal(t, "myhost", mp.Hostname)
	})

	t.Run("shieldsUp set", func(t *testing.T) {
		req := &settingsRequest{ShieldsUp: ptr(true)}
		mp := buildMaskedPrefs(req, nil)
		assert.True(t, mp.ShieldsUpSet)
		assert.True(t, mp.ShieldsUp)
	})
}
