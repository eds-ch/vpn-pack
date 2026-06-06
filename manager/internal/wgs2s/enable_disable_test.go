package wgs2s

import (
	"errors"
	"testing"
)

// snapshotTunnels copies the slice of tunnels so a later mutation in m.config
// can't retroactively flip an earlier saved snapshot's Enabled flag.
func snapshotTunnels(src []TunnelConfig) []TunnelConfig {
	out := make([]TunnelConfig, len(src))
	copy(out, src)
	return out
}

// TestEnable_KernelFailureLeavesDiskDisabled covers BUG-M11: when the kernel
// bring-up step fails, no save() call must have observed Enabled=true for
// the tunnel. The contract is kernel-first: persist enabled only after the
// kernel side succeeds. The prior bug wrote Enabled=true to disk first and
// only tried to revert on failure.
func TestEnable_KernelFailureLeavesDiskDisabled(t *testing.T) {
	mgr, _ := newTestManager(t)
	cfg := TunnelConfig{ID: "A", InterfaceName: "wg-s2s0", Enabled: false}
	mgr.config.Tunnels = []TunnelConfig{cfg}

	mgr.bringUpForTest = func(_ TunnelConfig) error {
		return errors.New("simulated kernel bring-up failure")
	}

	var saveSnapshots [][]TunnelConfig
	mgr.saveOverride = func() error {
		saveSnapshots = append(saveSnapshots, snapshotTunnels(mgr.config.Tunnels))
		return nil
	}

	if err := mgr.EnableTunnel("A"); err == nil {
		t.Fatal("expected error from kernel bring-up failure")
	}

	for i, snap := range saveSnapshots {
		for _, t2 := range snap {
			if t2.ID == "A" && t2.Enabled {
				t.Fatalf("save call %d observed tunnel A Enabled=true after kernel failure; saves should not have run", i)
			}
		}
	}
}

// TestDisable_KernelTearDownFailureAbortsSaga covers BUG-L10 follow-up:
// if the kernel deleteLink step fails (e.g. EBUSY), tearDown must surface
// that error so the saga aborts BEFORE persisting Enabled=false to disk.
// Previously tearDown swallowed deleteLink errors and DisableTunnel still
// reached the save step, leaving disk and kernel inconsistent.
func TestDisable_KernelTearDownFailureAbortsSaga(t *testing.T) {
	mgr, fk := newTestManager(t)
	cfg := TunnelConfig{ID: "A", InterfaceName: "wg-s2s0", Enabled: true}
	mgr.config.Tunnels = []TunnelConfig{cfg}
	fk.createIface(cfg.InterfaceName)

	mgr.deleteLink = func(uint32) error {
		return errors.New("simulated EBUSY: device or resource busy")
	}

	var saveSnapshots [][]TunnelConfig
	mgr.saveOverride = func() error {
		saveSnapshots = append(saveSnapshots, snapshotTunnels(mgr.config.Tunnels))
		return nil
	}

	if err := mgr.DisableTunnel("A"); err == nil {
		t.Fatal("expected error from kernel teardown failure")
	}

	if len(saveSnapshots) > 0 {
		t.Fatalf("save must NOT run when kernel teardown fails; got %d snapshots", len(saveSnapshots))
	}
	if !mgr.config.Tunnels[0].Enabled {
		t.Fatal("in-memory Enabled was flipped to false despite kernel teardown failure")
	}
}

// TestUpdate_KernelFailureKeepsOldDiskState covers BUG-M11 for UpdateTunnel:
// when the kernel recreate step fails, the disk must keep the prior tunnel
// configuration. Before the kernel-first refactor, UpdateTunnel mutated
// m.config + saved merged config BEFORE attempting the kernel apply.
func TestUpdate_KernelFailureKeepsOldDiskState(t *testing.T) {
	mgr, _ := newTestManager(t)
	cfg := TunnelConfig{
		ID: "A", InterfaceName: "wg-s2s0", Enabled: true,
		ListenPort: 51820, TunnelAddress: "10.0.0.1/24",
		PeerPublicKey: "test-key",
	}
	mgr.config.Tunnels = []TunnelConfig{cfg}

	mgr.bringUpForTest = func(_ TunnelConfig) error {
		return errors.New("simulated kernel bring-up failure")
	}

	var saveSnapshots [][]TunnelConfig
	mgr.saveOverride = func() error {
		saveSnapshots = append(saveSnapshots, snapshotTunnels(mgr.config.Tunnels))
		return nil
	}

	// TunnelAddress change forces the recreate path (needsRecreate returns true).
	updates := TunnelConfig{TunnelAddress: "10.0.0.2/24"}
	if _, err := mgr.UpdateTunnel("A", updates); err == nil {
		t.Fatal("expected error from kernel recreate failure")
	}

	for i, snap := range saveSnapshots {
		for _, t2 := range snap {
			if t2.ID == "A" && t2.TunnelAddress == "10.0.0.2/24" {
				t.Fatalf("save call %d observed merged TunnelAddress after kernel failure; disk should remain at old config", i)
			}
		}
	}
	if got := mgr.config.Tunnels[0].TunnelAddress; got != "10.0.0.1/24" {
		t.Fatalf("in-memory config mutated after kernel failure: got %q, want 10.0.0.1/24", got)
	}
}

// TestDisable_KernelTeardownBeforeDiskSave covers BUG-L10: Disable must tear
// down the kernel interface BEFORE persisting Enabled=false to disk. If the
// kernel teardown returns nil (success) the saga then persists; if it ever
// returns an error in the future, no save call should observe Enabled=false
// without kernel side already gone.
func TestDisable_KernelTeardownBeforeDiskSave(t *testing.T) {
	mgr, fk := newTestManager(t)
	cfg := TunnelConfig{ID: "A", InterfaceName: "wg-s2s0", Enabled: true}
	mgr.config.Tunnels = []TunnelConfig{cfg}
	// Place the interface in the fake kernel so tearDown can remove it.
	fk.createIface(cfg.InterfaceName)

	var order []string
	mgr.saveOverride = func() error {
		order = append(order, "save")
		return nil
	}
	// Hook the kernel teardown via the lookupIface side effect — when tearDown
	// runs it'll call lookupIface, which is fk.lookupIface here. We record the
	// kernel call by wrapping deleteLink.
	origDelete := mgr.deleteLink
	mgr.deleteLink = func(idx uint32) error {
		order = append(order, "kernel-delete")
		return origDelete(idx)
	}

	if err := mgr.DisableTunnel("A"); err != nil {
		t.Fatalf("DisableTunnel: %v", err)
	}

	if len(order) < 2 {
		t.Fatalf("expected both kernel-delete and save; got %v", order)
	}
	if order[0] != "kernel-delete" {
		t.Fatalf("kernel teardown must precede disk save; got order=%v", order)
	}
}
