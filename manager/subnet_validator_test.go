package main

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()
	_, ipNet, err := net.ParseCIDR(s)
	require.NoError(t, err)
	return ipNet
}

func TestSubnetsOverlap(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "10.0.0.0/24", "10.0.0.0/24", true},
		{"A contains B", "10.0.0.0/16", "10.0.1.0/24", true},
		{"B contains A", "10.0.1.0/24", "10.0.0.0/16", true},
		{"adjacent no overlap", "10.0.0.0/24", "10.0.1.0/24", false},
		{"completely disjoint", "10.0.0.0/24", "192.168.1.0/24", false},
		{"/32 inside range", "10.0.0.5/32", "10.0.0.0/24", true},
		{"/32 outside range", "10.0.1.5/32", "10.0.0.0/24", false},
		{"superset covers subset", "192.168.0.0/16", "192.168.1.0/24", true},
		{"subset within superset", "192.168.1.128/25", "192.168.1.0/24", true},
		{"IPv6 overlap", "fd00::/64", "fd00::/48", true},
		{"IPv6 disjoint", "fd00::/64", "fd01::/64", false},
		{"IPv4 vs IPv6 never overlap", "10.0.0.0/24", "fd00::/64", false},
		{"/0 contains everything", "0.0.0.0/0", "10.0.0.0/24", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := parseCIDR(t, tt.a)
			b := parseCIDR(t, tt.b)
			got := subnetsOverlap(a, b)
			assert.Equal(t, tt.want, got, "subnetsOverlap(%s, %s)", tt.a, tt.b)
		})
	}
}

func TestValidateAllowedIPs_Block(t *testing.T) {
	sys := &SystemSubnets{
		Interfaces: []InterfaceSubnet{
			{CIDR: "192.168.1.0/24", Interface: "br0"},
			{CIDR: "10.128.228.0/24", Interface: "wg-s2s0"},
		},
	}

	tests := []struct {
		name    string
		cidrs   []string
		blocked int
		cidr    string
	}{
		{"exact LAN match (scenario A)", []string{"192.168.1.0/24"}, 1, "192.168.1.0/24"},
		{"exact tunnel match (scenario B)", []string{"10.128.228.0/24"}, 1, "10.128.228.0/24"},
		{"superset of LAN (scenario F)", []string{"192.168.0.0/16"}, 1, "192.168.0.0/16"},
		{"subset of LAN (scenario G)", []string{"192.168.1.128/25"}, 1, "192.168.1.128/25"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := ValidateAllowedIPs(tt.cidrs, sys)
			require.Len(t, vr.Blocked, tt.blocked)
			assert.Equal(t, tt.cidr, vr.Blocked[0].CIDR)
			assert.Equal(t, "block", vr.Blocked[0].Severity)
			assert.True(t, vr.HasBlocks())
		})
	}
}

func TestValidateAllowedIPs_Warn(t *testing.T) {
	sys := &SystemSubnets{
		Routes: []RouteSubnet{
			{CIDR: "10.0.0.0/8", Interface: "eth0", Gateway: "192.168.1.254", Protocol: "static"},
		},
	}

	tests := []struct {
		name  string
		cidrs []string
		warns int
	}{
		{"exact route match (scenario D)", []string{"10.0.0.0/8"}, 1},
		{"subset of route (scenario H)", []string{"10.1.0.0/16"}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vr := ValidateAllowedIPs(tt.cidrs, sys)
			assert.Empty(t, vr.Blocked)
			assert.Len(t, vr.Warnings, tt.warns)
			assert.Equal(t, "warn", vr.Warnings[0].Severity)
			assert.False(t, vr.HasBlocks())
			assert.False(t, vr.IsClean())
		})
	}
}

func TestValidateAllowedIPs_NoConflict(t *testing.T) {
	sys := &SystemSubnets{
		Interfaces: []InterfaceSubnet{
			{CIDR: "192.168.1.0/24", Interface: "br0"},
		},
		Routes: []RouteSubnet{
			{CIDR: "172.16.0.0/16", Interface: "eth0", Protocol: "static"},
		},
	}

	vr := ValidateAllowedIPs([]string{"10.100.0.0/24"}, sys)
	assert.True(t, vr.IsClean())
	assert.False(t, vr.HasBlocks())
}

func TestValidateAllowedIPs_MixedBlockAndWarn(t *testing.T) {
	sys := &SystemSubnets{
		Interfaces: []InterfaceSubnet{
			{CIDR: "192.168.1.0/24", Interface: "br0"},
		},
		Routes: []RouteSubnet{
			{CIDR: "10.0.0.0/8", Interface: "eth0", Protocol: "static"},
		},
	}

	vr := ValidateAllowedIPs([]string{"192.168.1.0/24", "10.5.0.0/16"}, sys)
	assert.Len(t, vr.Blocked, 1)
	assert.Len(t, vr.Warnings, 1)
	assert.True(t, vr.HasBlocks())
	assert.False(t, vr.IsClean())
}

func TestValidateAllowedIPs_EmptyCIDRs(t *testing.T) {
	sys := &SystemSubnets{
		Interfaces: []InterfaceSubnet{{CIDR: "192.168.1.0/24", Interface: "br0"}},
	}
	vr := ValidateAllowedIPs(nil, sys)
	assert.True(t, vr.IsClean())
}

func TestValidateAllowedIPs_EmptySystem(t *testing.T) {
	vr := ValidateAllowedIPs([]string{"10.0.0.0/24", "172.16.0.0/12"}, &SystemSubnets{})
	assert.True(t, vr.IsClean())
}

func TestValidateAllowedIPs_TailscaleSuperset(t *testing.T) {
	sys := &SystemSubnets{
		Interfaces: []InterfaceSubnet{
			{CIDR: "100.94.209.109/32", Interface: "tailscale0"},
		},
	}
	vr := ValidateAllowedIPs([]string{"100.64.0.0/10"}, sys)
	require.Len(t, vr.Blocked, 1)
	assert.Equal(t, "100.64.0.0/10", vr.Blocked[0].CIDR)
	assert.Contains(t, vr.Blocked[0].Interface, "tailscale0")
}

func TestValidateAllowedIPs_BlockedSkipsWarnCheck(t *testing.T) {
	sys := &SystemSubnets{
		Interfaces: []InterfaceSubnet{
			{CIDR: "10.0.0.0/24", Interface: "br0"},
		},
		Routes: []RouteSubnet{
			{CIDR: "10.0.0.0/8", Interface: "eth0", Protocol: "static"},
		},
	}
	vr := ValidateAllowedIPs([]string{"10.0.0.0/24"}, sys)
	assert.Len(t, vr.Blocked, 1)
	assert.Empty(t, vr.Warnings, "blocked CIDR should not also produce a warning")
}

func TestValidationResult_Helpers(t *testing.T) {
	t.Run("empty result", func(t *testing.T) {
		vr := &ValidationResult{}
		assert.False(t, vr.HasBlocks())
		assert.True(t, vr.IsClean())
	})
	t.Run("has blocks", func(t *testing.T) {
		vr := &ValidationResult{Blocked: []SubnetConflict{{CIDR: "x"}}}
		assert.True(t, vr.HasBlocks())
		assert.False(t, vr.IsClean())
	})
	t.Run("has warnings only", func(t *testing.T) {
		vr := &ValidationResult{Warnings: []SubnetConflict{{CIDR: "x"}}}
		assert.False(t, vr.HasBlocks())
		assert.False(t, vr.IsClean())
	})
}

func TestCollectSystemSubnets_Smoke(t *testing.T) {
	sys, err := CollectSystemSubnets()
	require.NoError(t, err)
	require.NotNil(t, sys)
	assert.NotEmpty(t, sys.Interfaces, "dev host should have at least one non-loopback interface")
}
