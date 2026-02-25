package udapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFirewallFilterPath(t *testing.T) {
	tests := []struct {
		name  string
		chain string
		parts []string
		want  string
	}{
		{
			name:  "chain only",
			chain: "FORWARD_IN",
			want:  "/firewall/filter/FORWARD_IN",
		},
		{
			name:  "chain with rule",
			chain: "FORWARD_IN",
			parts: []string{"rule"},
			want:  "/firewall/filter/FORWARD_IN/rule",
		},
		{
			name:  "chain with multiple parts",
			chain: "INPUT",
			parts: []string{"rule", "42"},
			want:  "/firewall/filter/INPUT/rule/42",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firewallFilterPath(tt.chain, tt.parts...)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestZoneRules(t *testing.T) {
	tests := []struct {
		name        string
		iface       string
		marker      string
		chainPrefix string
		wantLen     int
		wantChains  []string
		wantTargets []string
		wantDirs    []string
	}{
		{
			name:        "tailscale interface",
			iface:       "tailscale0",
			marker:      "vpn-pack:tailscale",
			chainPrefix: "TS_ZONE",
			wantLen:     3,
			wantChains:  []string{"FORWARD_IN", "INPUT", "OUTPUT"},
			wantTargets: []string{"TS_ZONE_IN", "TS_ZONE_LOCAL", "LOCAL_TS_ZONE"},
			wantDirs:    []string{"inInterface", "inInterface", "outInterface"},
		},
		{
			name:        "wg-s2s interface",
			iface:       "wg0",
			marker:      "vpn-pack:wg-s2s:wg0",
			chainPrefix: "WG_S2S",
			wantLen:     3,
			wantChains:  []string{"FORWARD_IN", "INPUT", "OUTPUT"},
			wantTargets: []string{"WG_S2S_IN", "WG_S2S_LOCAL", "LOCAL_WG_S2S"},
			wantDirs:    []string{"inInterface", "inInterface", "outInterface"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rules := zoneRules(tt.iface, tt.marker, tt.chainPrefix)
			assert.Len(t, rules, tt.wantLen)

			for i, r := range rules {
				assert.Equal(t, tt.wantChains[i], r.Chain, "rule %d chain", i)
				assert.Equal(t, tt.wantTargets[i], r.Target, "rule %d target", i)
				assert.Equal(t, tt.iface, r.Interface, "rule %d interface", i)
				assert.Equal(t, tt.marker, r.Marker, "rule %d marker", i)
				assert.Equal(t, tt.wantDirs[i], r.Direction, "rule %d direction", i)
				assert.Contains(t, r.Desc, tt.iface)
				assert.Contains(t, r.Desc, tt.marker)
			}
		})
	}
}

func TestZoneRulesDescriptionFormat(t *testing.T) {
	rules := zoneRules("tailscale0", "vpn-pack:ts", "TS")
	assert.Equal(t, "tailscale0 TS_IN (vpn-pack:ts)", rules[0].Desc)
	assert.Equal(t, "tailscale0 TS_LOCAL (vpn-pack:ts)", rules[1].Desc)
	assert.Equal(t, "tailscale0 LOCAL_TS (vpn-pack:ts)", rules[2].Desc)
}
