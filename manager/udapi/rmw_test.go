package udapi

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"unifi-tailscale/manager/internal/stress"
)

// TestEnsureZoneSubnets_ConcurrentNoLostUpdate exercises the GET-modify-PUT
// race on a single ipset. Each call adds a *distinct* CIDR; without
// per-set serialisation, two concurrent RMWs that read the same baseline
// drop one of the writes, and the lost CIDR is never re-added by any
// later iteration. With WithIpsetRMW wrapping the RMW pair, every
// distinct CIDR survives.
func TestEnsureZoneSubnets_ConcurrentNoLostUpdate(t *testing.T) {
	const goroutines = 8
	const iterPerG = 25
	const want = goroutines * iterPerG

	fake := newFakeUDAPISocket(t)
	fake.installSet("VPN_subnets", nil)
	cli := NewClient(fake.path)

	var counter atomic.Int64

	stress.Run(t, goroutines, iterPerG, func(g int) {
		i := counter.Add(1) - 1
		cidr := fmt.Sprintf("10.%d.%d.0/24", g, i)
		if err := EnsureZoneSubnets(context.Background(), cli, "VPN_subnets", []string{cidr}); err != nil {
			t.Errorf("EnsureZoneSubnets(%s): %v", cidr, err)
		}
	})

	got := fake.entries("VPN_subnets")
	if len(got) != want {
		t.Fatalf("expected %d distinct entries (RMW serialisation should keep all), got %d", want, len(got))
	}
}
