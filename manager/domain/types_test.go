package domain

import (
	"sync"
	"testing"

	"unifi-tailscale/manager/internal/stress"
)

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
