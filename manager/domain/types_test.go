package domain

import (
	"encoding/json"
	"sync"
	"testing"

	"unifi-tailscale/manager/internal/stress"
)

func TestPolicyUnmarshalParsesDerivedOrigin(t *testing.T) {
	const body = `{"id":"e7e860e3","name":"VPN Pack: Allow Tailscale to Internal (Return)","metadata":{"origin":"DERIVED"}}`

	var p Policy
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Metadata == nil {
		t.Fatalf("Metadata not parsed; got nil")
	}
	if p.Metadata.Origin != "DERIVED" {
		t.Fatalf("Origin = %q, want %q", p.Metadata.Origin, "DERIVED")
	}
}

func TestPolicyUnmarshalNoMetadataLeavesNil(t *testing.T) {
	const body = `{"id":"abc","name":"VPN Pack: WAN port"}`

	var p Policy
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Metadata != nil {
		t.Fatalf("Metadata = %+v, want nil", p.Metadata)
	}
}

func TestPolicyMarshalOmitsMetadataWhenNil(t *testing.T) {
	b, err := json.Marshal(Policy{ID: "x", Name: "VPN Pack: test"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(b); contains(got, "metadata") {
		t.Fatalf("create request must not emit metadata; got %s", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSnapshotDeepCopiesSelf(t *testing.T) {
	ts := NewTailscaleState()
	ts.Update(func(d *StateData) {
		d.Self = &SelfNode{TxBytes: 1, RxBytes: 2, Online: true}
	})

	var mu sync.Mutex
	corrupted := false
	stress.Run(t, 4, 5000, func(g int) {
		if g%2 == 0 {
			snap := ts.Snapshot()
			if snap.Self == nil {
				return
			}
			tx, rx, online := snap.Self.TxBytes, snap.Self.RxBytes, snap.Self.Online
			_, _, _ = tx, rx, online
		} else {
			ts.Update(func(d *StateData) {
				if d.Self != nil {
					d.Self.TxBytes++
					d.Self.RxBytes++
					d.Self.Online = !d.Self.Online
				}
			})
		}
	})

	mu.Lock()
	defer mu.Unlock()
	if corrupted {
		t.Fatalf("observed inconsistent Self under concurrent mutation")
	}
}

func TestSnapshotClonesIntegrationStatus(t *testing.T) {
	ts := NewTailscaleState()
	ts.Update(func(d *StateData) {
		st := IntegrationStatus{Reason: "ok"}
		d.IntegrationStatus = &st
	})
	stress.Run(t, 4, 5000, func(g int) {
		if g%2 == 0 {
			snap := ts.Snapshot()
			if snap.IntegrationStatus != nil {
				_ = snap.IntegrationStatus.Reason
			}
		} else {
			ts.Update(func(d *StateData) {
				if d.IntegrationStatus != nil {
					d.IntegrationStatus.Reason = "rotated"
				}
			})
		}
	})
}

func TestSnapshotClonesFirewallHealth(t *testing.T) {
	ts := NewTailscaleState()
	ts.Update(func(d *StateData) {
		fh := FirewallHealth{UDAPIReachable: true, ZoneActive: true}
		d.FirewallHealth = &fh
	})
	stress.Run(t, 4, 5000, func(g int) {
		if g%2 == 0 {
			snap := ts.Snapshot()
			if snap.FirewallHealth != nil {
				_ = snap.FirewallHealth.UDAPIReachable
				_ = snap.FirewallHealth.ZoneActive
			}
		} else {
			ts.Update(func(d *StateData) {
				if d.FirewallHealth != nil {
					d.FirewallHealth.UDAPIReachable = !d.FirewallHealth.UDAPIReachable
					d.FirewallHealth.ZoneActive = !d.FirewallHealth.ZoneActive
				}
			})
		}
	})
}
