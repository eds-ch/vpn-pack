package udapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
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

func TestMarkerFromDescription(t *testing.T) {
	assert.Equal(t, "wg-s2s-manager:wg-s2s1", markerFromDescription("wg-s2s1 X_IN (wg-s2s-manager:wg-s2s1)"))
	assert.Equal(t, "wg-s2s-manager:wg-s2s10", markerFromDescription("wg-s2s10 X_IN (wg-s2s-manager:wg-s2s10)"))
	assert.Equal(t, "", markerFromDescription("no parens here"))
}

// BUG-S1: RemoveInterfaceRules matched the marker against the rule description
// with strings.Contains. The marker "wg-s2s-manager:wg-s2s1" is a substring of
// "wg-s2s-manager:wg-s2s10", so removing wg-s2s1 also deleted the live
// wg-s2s10 rule in every chain, silently tearing down the tenth tunnel's
// isolation. This test drives the real removal path over a mock UDAPI socket
// and asserts the wg-s2s10 rule (id 10) is never deleted. Fails on the pre-fix
// Contains-based code.
func TestRemoveInterfaceRules_MarkerNoOverDeletion(t *testing.T) {
	const (
		marker1  = "wg-s2s-manager:wg-s2s1"
		marker10 = "wg-s2s-manager:wg-s2s10"
	)

	var mu sync.Mutex
	var deleted []int

	sock := startMockUDAPI(t, func(env envelope) *Response {
		resp := func(body string) *Response {
			return &Response{ID: env.ID, Version: "v1.0", Method: env.Method, Entity: env.Entity, Response: json.RawMessage(body)}
		}
		switch env.Method {
		case "GET":
			body := fmt.Sprintf(
				`{"rules":[{"id":1,"description":"wg-s2s1 Z_IN (%s)"},{"id":10,"description":"wg-s2s10 Z_IN (%s)"}]}`,
				marker1, marker10)
			return resp(body)
		case "DELETE":
			b, _ := json.Marshal(env.Request)
			var req struct {
				ID int `json:"id"`
			}
			_ = json.Unmarshal(b, &req)
			mu.Lock()
			deleted = append(deleted, req.ID)
			mu.Unlock()
			return resp(`{"meta":{"rc":"ok"}}`)
		default:
			return resp(`{}`)
		}
	})

	c := NewClient(sock)
	if err := RemoveInterfaceRules(context.Background(), c, "wg-s2s1", marker1); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, id := range deleted {
		if id == 10 {
			t.Fatal("BUG-S1: wg-s2s10 rule deleted while removing wg-s2s1")
		}
		if id != 1 {
			t.Fatalf("unexpected rule id deleted: %d", id)
		}
	}
	// wg-s2s1's rule (id 1) must be deleted once per chain (FORWARD_IN, INPUT,
	// OUTPUT) = 3 deletions.
	if len(deleted) != 3 {
		t.Fatalf("expected 3 deletions of wg-s2s1 (one per chain), got %d: %v", len(deleted), deleted)
	}
}
