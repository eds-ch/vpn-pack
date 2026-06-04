package wgs2s

import (
	"sort"
	"testing"
)

// TestRestoreAll_IdempotentAgainstSameNameOrphan covers the BUG-L12 sibling
// scenario: an interface with the same name as a configured tunnel exists in
// the kernel but has no entry in routeRefs (e.g. crash recovery before any
// ref-counter state was rebuilt). RestoreAll must not panic, must not error,
// and routeRefs must match the post-restore config exactly.
func TestRestoreAll_IdempotentAgainstSameNameOrphan(t *testing.T) {
	mgr, fk := newTestManager(t)

	cfgs := []TunnelConfig{
		{ID: "A", Name: "a", InterfaceName: "wg-s2s0", AllowedIPs: []string{"10.10.0.0/24"}, Enabled: true},
	}
	mgr.config.Tunnels = cfgs

	// Pre-create the interface with the SAME name as configured tunnel A,
	// simulating a crash where the kernel state survived but the ref-counter
	// is empty. (Different idx from what the bring-up would assign.)
	staleIdx := fk.createIface("wg-s2s0")

	mgr.bringUpForTest = func(cfg TunnelConfig) error {
		// production bringUp would call cleanupExistingInterface first; do
		// the equivalent here so the test exercises that path through the
		// real cleanup logic.
		if err := mgr.cleanupExistingInterface(cfg); err != nil {
			return err
		}
		newIdx := fk.createIface(cfg.InterfaceName)
		return mgr.claimRoutes(cfg.ID, newIdx, cfg.AllowedIPs, effectiveMetric(cfg.RouteMetric))
	}

	if err := mgr.RestoreAll(); err != nil {
		t.Fatalf("RestoreAll: %v", err)
	}

	// The same-name interface that existed before must have been replaced;
	// stale idx is gone, a new idx is present under the same name.
	if _, ok := fk.byIdx[staleIdx]; ok {
		t.Fatal("stale-idx mapping survived: cleanupExistingInterface did not run")
	}
	if _, ok := fk.lookupIface("wg-s2s0"); !ok {
		t.Fatal("wg-s2s0 missing after RestoreAll: bring-up didn't recreate it")
	}

	// Ref-counter must own exactly the configured CIDR for A.
	if !mgr.routeRefs.owns("10.10.0.0/24", "A", effectiveMetric(0)) {
		t.Fatal("routeRefs: A missing as owner of 10.10.0.0/24")
	}
}

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
