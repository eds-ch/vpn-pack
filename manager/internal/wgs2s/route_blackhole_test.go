package wgs2s

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"

	"github.com/jsimonetti/rtnetlink"
	"golang.org/x/sys/unix"
)

// fakeKernel is a minimal in-memory stand-in for the kernel route+link tables
// used in unit tests. It implements routeOps and exposes link bookkeeping via
// methods consumed by newTestManager.
type fakeKernel struct {
	mu         sync.Mutex
	routes     map[string]bool   // key: normalized cidr → present
	routeIface map[string]uint32 // normalized cidr → current oif
	ifaces     map[string]uint32 // name → idx
	byIdx      map[uint32]string // idx → name
	nextIdx    uint32

	failAdd bool
}

func newFakeKernel() *fakeKernel {
	return &fakeKernel{
		routes:     map[string]bool{},
		routeIface: map[string]uint32{},
		ifaces:     map[string]uint32{},
		byIdx:      map[uint32]string{},
		nextIdx:    100,
	}
}

func cidrFromMsg(msg *rtnetlink.RouteMessage) string {
	ip := net.IP(msg.Attributes.Dst)
	if msg.Family == unix.AF_INET {
		ip = ip.To4()
	}
	return (&net.IPNet{IP: ip, Mask: net.CIDRMask(int(msg.DstLength), int(len(ip)*8))}).String()
}

func (f *fakeKernel) Add(msg *rtnetlink.RouteMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cidr := cidrFromMsg(msg)
	if f.failAdd {
		return fmt.Errorf("fake kernel: add disabled")
	}
	if f.routes[cidr] {
		return unix.EEXIST
	}
	f.routes[cidr] = true
	f.routeIface[cidr] = msg.Attributes.OutIface
	return nil
}

func (f *fakeKernel) Delete(msg *rtnetlink.RouteMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cidr := cidrFromMsg(msg)
	if !f.routes[cidr] {
		return unix.ESRCH
	}
	delete(f.routes, cidr)
	delete(f.routeIface, cidr)
	return nil
}

func (f *fakeKernel) Replace(msg *rtnetlink.RouteMessage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cidr := cidrFromMsg(msg)
	f.routes[cidr] = true
	f.routeIface[cidr] = msg.Attributes.OutIface
	return nil
}

func (f *fakeKernel) hasRoute(cidr string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.routes[normalizeCIDR(cidr)]
}

// deleteRoute simulates an external actor removing a route from the kernel
// without touching ref-counter state.
func (f *fakeKernel) deleteRoute(cidr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.routes, normalizeCIDR(cidr))
	delete(f.routeIface, normalizeCIDR(cidr))
}

func (f *fakeKernel) routeIfaceFor(cidr string) (uint32, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	idx, ok := f.routeIface[normalizeCIDR(cidr)]
	return idx, ok
}

func (f *fakeKernel) createIface(name string) uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	if idx, ok := f.ifaces[name]; ok {
		return idx
	}
	f.nextIdx++
	idx := f.nextIdx
	f.ifaces[name] = idx
	f.byIdx[idx] = name
	return idx
}

func (f *fakeKernel) lookupIface(name string) (uint32, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	idx, ok := f.ifaces[name]
	return idx, ok
}

func (f *fakeKernel) deleteLink(idx uint32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	name, ok := f.byIdx[idx]
	if !ok {
		return fmt.Errorf("fake kernel: no such interface idx=%d", idx)
	}
	delete(f.byIdx, idx)
	delete(f.ifaces, name)
	return nil
}

// newTestManager builds a TunnelManager wired against a fakeKernel.
// Real netlink is never touched; tests can drive cleanup/claim/release paths
// directly through this manager.
func newTestManager(t *testing.T) (*TunnelManager, *fakeKernel) {
	t.Helper()
	fk := newFakeKernel()
	mgr := &TunnelManager{
		config:      &TunnelsConfig{},
		configDir:   t.TempDir(),
		routes:      fk,
		routeRefs:   newRouteRefCounter(),
		lookupIface: fk.lookupIface,
		deleteLink:  fk.deleteLink,
		log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	return mgr, fk
}

// mustBringUp simulates a successful tunnel bring-up: creates a fake interface
// in the kernel and registers AllowedIPs with the manager's route ref-counter.
func mustBringUp(t *testing.T, mgr *TunnelManager, fk *fakeKernel, cfg TunnelConfig) {
	t.Helper()
	idx := fk.createIface(cfg.InterfaceName)
	if err := mgr.claimRoutes(cfg.ID, idx, cfg.AllowedIPs, effectiveMetric(cfg.RouteMetric)); err != nil {
		t.Fatalf("claimRoutes(%s): %v", cfg.ID, err)
	}
}

func TestClaimRoutesReassertsOnSharedOwner(t *testing.T) {
	mgr, fk := newTestManager(t)
	cfgA := TunnelConfig{ID: "A", InterfaceName: "wg-s2s0", AllowedIPs: []string{"10.10.0.0/24"}}
	cfgB := TunnelConfig{ID: "B", InterfaceName: "wg-s2s1", AllowedIPs: []string{"10.10.0.0/24"}}

	mustBringUp(t, mgr, fk, cfgA)
	mustBringUp(t, mgr, fk, cfgB)

	// Simulate external force-delete of the kernel route while both owners
	// remain in the ref-counter — exactly the BUG-H4 scenario where a stale
	// cleanup or external actor removes the underlying kernel entry.
	fk.deleteRoute("10.10.0.0/24")
	if fk.hasRoute("10.10.0.0/24") {
		t.Fatal("setup: fake kernel route should be gone after deleteRoute")
	}

	// Re-claim from B (idempotent path: firstOwner=false because A is still in refs).
	idxB, _ := fk.lookupIface(cfgB.InterfaceName)
	if err := mgr.claimRoutes(cfgB.ID, idxB, cfgB.AllowedIPs, effectiveMetric(cfgB.RouteMetric)); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if !fk.hasRoute("10.10.0.0/24") {
		t.Fatal("claimRoutes did not re-assert kernel route on shared owner")
	}
}

func TestSharedRouteSurvivesPeerCleanup(t *testing.T) {
	mgr, fk := newTestManager(t)
	cfgA := TunnelConfig{ID: "A", InterfaceName: "wg-s2s0", AllowedIPs: []string{"10.10.0.0/24"}}
	cfgB := TunnelConfig{ID: "B", InterfaceName: "wg-s2s1", AllowedIPs: []string{"10.10.0.0/24"}}

	mustBringUp(t, mgr, fk, cfgA)
	mustBringUp(t, mgr, fk, cfgB)

	if err := mgr.cleanupExistingInterface(cfgB); err != nil {
		t.Fatalf("cleanup: %v", err)
	}

	if !mgr.routeRefs.owns("10.10.0.0/24", "A", effectiveMetric(0)) {
		t.Fatal("A lost ownership after cleanup of B")
	}
	if !fk.hasRoute("10.10.0.0/24") {
		t.Fatal("kernel route blackhole: shared route was removed by B cleanup")
	}
}
