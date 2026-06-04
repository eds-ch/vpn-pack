package wgs2s

import (
	"sort"
	"testing"
)

// TestRestoreAll_RemovesOrphanInterfaces covers BUG-L12: after a crash a
// previous-incarnation interface (e.g. wg-s2s9 from a deleted tunnel) can
// remain in the kernel. RestoreAll must scrub interfaces named wg-s2s* that
// aren't in the current config, so subsequent bring-up isn't sabotaged by
// stale state. Tunnels in the config must still come up normally.
func TestRestoreAll_RemovesOrphanInterfaces(t *testing.T) {
	mgr, fk := newTestManager(t)

	// Two tunnels are in the config; one orphan exists in the kernel only.
	cfgs := []TunnelConfig{
		{ID: "A", Name: "a", InterfaceName: "wg-s2s0", AllowedIPs: []string{"10.10.0.0/24"}, Enabled: true},
		{ID: "B", Name: "b", InterfaceName: "wg-s2s1", AllowedIPs: []string{"10.11.0.0/24"}, Enabled: true},
	}
	mgr.config.Tunnels = cfgs
	// Pre-create the orphan and a configured interface in the fake kernel.
	fk.createIface("wg-s2s0")
	fk.createIface("wg-s2s9") // orphan: no matching config entry

	// Stub keypair load: RestoreAll needs to read a private key per tunnel.
	// Generate one on the fly via the production helper.
	for _, c := range cfgs {
		if _, err := generateKeypair(mgr.configDir, c.ID); err != nil {
			t.Fatalf("generate keypair: %v", err)
		}
	}

	// Force bringUp to be a no-op so we don't depend on real wg/netlink: stub
	// the kernel link/route paths via interface seams already in place. The
	// orphan-removal pass is what we're testing, so we only care that the
	// orphan disappears and configured names survive.
	mgr.bringUpForTest = func(cfg TunnelConfig) error {
		fk.createIface(cfg.InterfaceName)
		return nil
	}

	if err := mgr.RestoreAll(); err != nil {
		t.Fatalf("RestoreAll: %v", err)
	}

	// wg-s2s9 must be gone; wg-s2s0 / wg-s2s1 must remain.
	var names []string
	for name := range fk.ifaces {
		names = append(names, name)
	}
	sort.Strings(names)
	want := []string{"wg-s2s0", "wg-s2s1"}
	if len(names) != len(want) {
		t.Fatalf("expected exactly %v; got %v", want, names)
	}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("interface[%d]=%q want %q (full: %v)", i, names[i], n, names)
		}
	}
}
