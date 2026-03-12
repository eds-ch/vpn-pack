package wgs2s

import "testing"

func TestRouteRefCounter_SingleOwner(t *testing.T) {
	rc := newRouteRefCounter()

	if got := rc.add("10.0.0.0/24", "tun-a", 10); !got {
		t.Fatal("expected firstOwner=true")
	}

	remaining := rc.remove("10.0.0.0/24", "tun-a")
	if len(remaining) != 0 {
		t.Fatalf("expected no remaining owners, got %d", len(remaining))
	}
}

func TestRouteRefCounter_TwoOwnersSamePrefix(t *testing.T) {
	rc := newRouteRefCounter()

	if got := rc.add("10.0.0.0/24", "tun-a", 10); !got {
		t.Fatal("tun-a: expected firstOwner=true")
	}
	if got := rc.add("10.0.0.0/24", "tun-b", 20); got {
		t.Fatal("tun-b: expected firstOwner=false")
	}

	remaining := rc.remove("10.0.0.0/24", "tun-a")
	if len(remaining) != 1 {
		t.Fatalf("after removing tun-a: expected 1 remaining, got %d", len(remaining))
	}
	if remaining[0].tunnelID != "tun-b" || remaining[0].ifIndex != 20 {
		t.Fatalf("expected tun-b/20, got %s/%d", remaining[0].tunnelID, remaining[0].ifIndex)
	}

	remaining = rc.remove("10.0.0.0/24", "tun-b")
	if len(remaining) != 0 {
		t.Fatalf("after removing tun-b: expected 0 remaining, got %d", len(remaining))
	}
}

func TestRouteRefCounter_DifferentPrefixes(t *testing.T) {
	rc := newRouteRefCounter()

	rc.add("10.0.0.0/24", "tun-a", 10)
	rc.add("192.168.1.0/24", "tun-b", 20)

	remaining := rc.remove("10.0.0.0/24", "tun-a")
	if len(remaining) != 0 {
		t.Fatalf("10.0.0.0/24: expected 0 remaining, got %d", len(remaining))
	}

	remaining = rc.remove("192.168.1.0/24", "tun-b")
	if len(remaining) != 0 {
		t.Fatalf("192.168.1.0/24: expected 0 remaining, got %d", len(remaining))
	}
}

func TestRouteRefCounter_IdempotentAdd(t *testing.T) {
	rc := newRouteRefCounter()

	first := rc.add("10.0.0.0/24", "tun-a", 10)
	if !first {
		t.Fatal("first add: expected firstOwner=true")
	}

	second := rc.add("10.0.0.0/24", "tun-a", 10)
	if !second {
		t.Fatal("idempotent add of sole owner: expected firstOwner=true (still only owner)")
	}

	remaining := rc.remove("10.0.0.0/24", "tun-a")
	if len(remaining) != 0 {
		t.Fatalf("after remove: expected 0 remaining, got %d (duplicate entry leaked)", len(remaining))
	}
}

func TestRouteRefCounter_RemoveNonExistent(t *testing.T) {
	rc := newRouteRefCounter()

	remaining := rc.remove("10.0.0.0/24", "tun-x")
	if remaining != nil {
		t.Fatalf("expected nil for non-existent CIDR, got %v", remaining)
	}

	rc.add("10.0.0.0/24", "tun-a", 10)
	remaining = rc.remove("10.0.0.0/24", "tun-x")
	if len(remaining) != 1 || remaining[0].tunnelID != "tun-a" {
		t.Fatalf("expected [tun-a] unchanged, got %v", remaining)
	}
}

func TestRouteRefCounter_NormalizeCIDR(t *testing.T) {
	rc := newRouteRefCounter()

	if got := rc.add("10.0.0.1/24", "tun-a", 10); !got {
		t.Fatal("expected firstOwner=true")
	}
	if got := rc.add("10.0.0.0/24", "tun-b", 20); got {
		t.Fatal("normalized CIDRs should match: expected firstOwner=false")
	}

	remaining := rc.remove("10.0.0.99/24", "tun-a")
	if len(remaining) != 1 || remaining[0].tunnelID != "tun-b" {
		t.Fatalf("expected [tun-b], got %v", remaining)
	}
}

func TestRouteRefCounter_RecreateScenario(t *testing.T) {
	rc := newRouteRefCounter()

	rc.add("10.0.0.0/24", "tun-a", 10)
	rc.add("10.0.0.0/24", "tun-b", 20)

	// Tunnel A torn down then re-created with new ifIndex
	remaining := rc.remove("10.0.0.0/24", "tun-a")
	if len(remaining) != 1 || remaining[0].tunnelID != "tun-b" {
		t.Fatalf("after tun-a teardown: expected [tun-b], got %v", remaining)
	}

	if got := rc.add("10.0.0.0/24", "tun-a", 30); got {
		t.Fatal("re-add tun-a: expected firstOwner=false (tun-b still present)")
	}

	// Both tunnels are owners again, tun-a with new ifIndex
	remaining = rc.remove("10.0.0.0/24", "tun-b")
	if len(remaining) != 1 || remaining[0].tunnelID != "tun-a" || remaining[0].ifIndex != 30 {
		t.Fatalf("expected [tun-a/30], got %v", remaining)
	}
}

func TestRouteRefCounter_UpdateIfIndex(t *testing.T) {
	rc := newRouteRefCounter()

	rc.add("10.0.0.0/24", "tun-a", 10)
	rc.add("10.0.0.0/24", "tun-b", 20)

	// Re-add tun-a with updated ifIndex (e.g. interface recreated)
	rc.add("10.0.0.0/24", "tun-a", 50)

	remaining := rc.remove("10.0.0.0/24", "tun-b")
	if len(remaining) != 1 || remaining[0].ifIndex != 50 {
		t.Fatalf("expected tun-a with updated ifIndex=50, got %v", remaining)
	}
}
