package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
)

// fakeFwTables backs the probe seams on FirewallManager: it records which
// (chain, match) and (ipset, member) pairs are considered present.
type fakeFwTables struct {
	chainRules map[string]map[string]bool // chain → match → present
	ipsets     map[string]map[string]bool // setName → cidr → present
}

func newFakeFwTables() *fakeFwTables {
	return &fakeFwTables{
		chainRules: map[string]map[string]bool{},
		ipsets:     map[string]map[string]bool{},
	}
}

func (f *fakeFwTables) installChainRule(chain, match string) {
	if f.chainRules[chain] == nil {
		f.chainRules[chain] = map[string]bool{}
	}
	f.chainRules[chain][match] = true
}

func (f *fakeFwTables) installIPSet(setName string, members []string) {
	if f.ipsets[setName] == nil {
		f.ipsets[setName] = map[string]bool{}
	}
	for _, m := range members {
		f.ipsets[setName][m] = true
	}
}

func (f *fakeFwTables) hasChainRule(chain, match string) bool {
	return f.chainRules[chain][match]
}

// hasIPSetEntry mirrors the production probe: a textual substring check on
// the ipset's listed members.
func (f *fakeFwTables) hasIPSetEntry(setName, match string) bool {
	for m := range f.ipsets[setName] {
		if strings.Contains(m, match) {
			return true
		}
	}
	return false
}

func newFakeFirewallManager(t *testing.T) (*FirewallManager, *fakeFwTables) {
	t.Helper()
	fk := newFakeFwTables()
	fm := &FirewallManager{
		chainProbe: fk.hasChainRule,
		ipsetProbe: fk.hasIPSetEntry,
	}
	return fm, fk
}

func TestCheckWgS2sRulesPresent_DetectsMissingIpsetEntry(t *testing.T) {
	fm, fk := newFakeFirewallManager(t)
	fk.installChainRule(config.ChainForwardInUser, "-i wg-s2s0")
	fk.installChainRule(config.ChainInputUserHook, "-i wg-s2s0")
	fk.installChainRule(config.ChainOutputUserHook, "-o wg-s2s0")
	fk.installIPSet("UBIOS4VPN_S2S_A_subnets", []string{"10.20.0.0/24"})

	got := fm.CheckWgS2sRulesPresent(context.Background(), []domain.WgS2sCheckSpec{{
		InterfaceName: "wg-s2s0",
		ChainPrefix:   "VPN_S2S_A",
		Subnets:       []string{"10.20.0.0/24", "10.21.0.0/24"},
	}})

	if got["wg-s2s0"] {
		t.Fatal("expected false: 10.21.0.0/24 missing from ipset")
	}
}

func TestCheckWgS2sRulesPresent_PassesWhenAllPresent(t *testing.T) {
	fm, fk := newFakeFirewallManager(t)
	fk.installChainRule(config.ChainForwardInUser, "-i wg-s2s0")
	fk.installChainRule(config.ChainInputUserHook, "-i wg-s2s0")
	fk.installChainRule(config.ChainOutputUserHook, "-o wg-s2s0")
	fk.installIPSet("UBIOS4VPN_S2S_A_subnets", []string{"10.20.0.0/24"})

	got := fm.CheckWgS2sRulesPresent(context.Background(), []domain.WgS2sCheckSpec{{
		InterfaceName: "wg-s2s0",
		ChainPrefix:   "VPN_S2S_A",
		Subnets:       []string{"10.20.0.0/24"},
	}})

	if !got["wg-s2s0"] {
		t.Fatal("expected true: all chain rules + ipset entry installed")
	}
}

func TestCheckWgS2sRulesPresent_NoChainPrefixSkipsIpset(t *testing.T) {
	// Backwards-compatible: when the caller has no chain prefix (e.g. zone
	// not yet provisioned) we still report chain-rule presence and skip
	// ipset entirely.
	fm, fk := newFakeFirewallManager(t)
	fk.installChainRule(config.ChainForwardInUser, "-i wg-s2s0")
	fk.installChainRule(config.ChainInputUserHook, "-i wg-s2s0")
	fk.installChainRule(config.ChainOutputUserHook, "-o wg-s2s0")

	got := fm.CheckWgS2sRulesPresent(context.Background(), []domain.WgS2sCheckSpec{{
		InterfaceName: "wg-s2s0",
		Subnets:       []string{"10.20.0.0/24"},
	}})
	if !got["wg-s2s0"] {
		t.Fatal("expected true when ChainPrefix empty: chain rules alone suffice")
	}
}

func TestCheckWgS2sRulesPresent_DetectsMissingChainRule(t *testing.T) {
	fm, fk := newFakeFirewallManager(t)
	// Forward chain rule missing — ipset side intentionally complete.
	fk.installChainRule(config.ChainInputUserHook, "-i wg-s2s0")
	fk.installChainRule(config.ChainOutputUserHook, "-o wg-s2s0")
	fk.installIPSet("UBIOS4VPN_S2S_A_subnets", []string{"10.20.0.0/24"})

	got := fm.CheckWgS2sRulesPresent(context.Background(), []domain.WgS2sCheckSpec{{
		InterfaceName: "wg-s2s0",
		ChainPrefix:   "VPN_S2S_A",
		Subnets:       []string{"10.20.0.0/24"},
	}})
	if got["wg-s2s0"] {
		t.Fatal("expected false: forward chain rule missing")
	}
}

// fakeUDAPIOps records calls and optionally fails the ipset fill step.
type fakeUDAPIOps struct {
	addCalls      []string
	removeCalls   []string
	ensureCalls   []string
	removeSubnet  []string
	failIpsetFill bool
}

func (f *fakeUDAPIOps) install(fm *FirewallManager) {
	fm.addInterfaceRules = func(_ context.Context, iface, marker, chainPrefix string) error {
		f.addCalls = append(f.addCalls, iface+"/"+marker+"/"+chainPrefix)
		return nil
	}
	fm.removeInterfaceRules = func(_ context.Context, iface, marker string) error {
		f.removeCalls = append(f.removeCalls, iface+"/"+marker)
		return nil
	}
	fm.ensureZoneSubnets = func(_ context.Context, setName string, cidrs []string) error {
		f.ensureCalls = append(f.ensureCalls, setName+":"+strings.Join(cidrs, ","))
		if f.failIpsetFill {
			return fmt.Errorf("simulated ipset fill failure")
		}
		return nil
	}
	fm.removeZoneSubnet = func(_ context.Context, setName, cidr string) error {
		f.removeSubnet = append(f.removeSubnet, setName+":"+cidr)
		return nil
	}
}

// stubManifest is the minimum ManifestStore the wg-s2s firewall path needs:
// chain prefix lookup + zone lookup. Other methods panic if accidentally called.
type stubManifest struct {
	ManifestStore
	chainPrefix string
	zone        ZoneManifest
	zoneOK      bool
}

func (s *stubManifest) GetWgS2sChainPrefix(string) string { return s.chainPrefix }
func (s *stubManifest) GetWgS2sZone(string) (ZoneManifest, bool) {
	return s.zone, s.zoneOK
}

// TestSetupWgS2sFirewall_ReportsIpsetFailure covers BUG-M4. When the ipset
// fill step fails after the chain rules were already installed, the saga
// must roll back the chain rules and report the error to the caller.
func TestSetupWgS2sFirewall_ReportsIpsetFailure(t *testing.T) {
	fm, _ := newFakeFirewallManager(t)
	fm.manifest = &stubManifest{chainPrefix: "VPN_S2S_A"}
	ops := &fakeUDAPIOps{failIpsetFill: true}
	ops.install(fm)

	err := fm.SetupWgS2sFirewall(context.Background(), "tunnel-A", "wg-s2s0", []string{"10.20.0.0/24"})
	if err == nil {
		t.Fatal("expected error when ipset fill fails")
	}
	if len(ops.addCalls) == 0 {
		t.Fatal("chain rules should have been installed before ipset fill")
	}
	if len(ops.removeCalls) == 0 {
		t.Fatal("chain rules must be rolled back on ipset failure")
	}
}

// SEC-C15: ts-forward must run AFTER UBIOS_FORWARD_JUMP so UniFi zone
// policies evaluate before Tailscale's fallback ACCEPT. The auditor must
// recognise misplacement so the watcher can repair it.
func TestAuditTsForwardOrder(t *testing.T) {
	misplaced := `*filter
:FORWARD ACCEPT [0:0]
-A FORWARD -j ts-forward
-A FORWARD -j UBIOS_FORWARD_JUMP
COMMIT
`
	res := auditTsForwardOrder(misplaced)
	if !res.Misplaced() {
		t.Fatalf("expected Misplaced=true for %+v", res)
	}
	if res.TSForwardPos != 1 || res.UBIOSPos != 2 {
		t.Fatalf("positions wrong: %+v", res)
	}

	correct := `*filter
:FORWARD ACCEPT [0:0]
-A FORWARD -j ts-mark
-A FORWARD -j UBIOS_FORWARD_JUMP
-A FORWARD -j ts-forward
COMMIT
`
	res = auditTsForwardOrder(correct)
	if res.Misplaced() {
		t.Fatalf("expected Misplaced=false for %+v", res)
	}
	if res.TSForwardPos != 3 || res.UBIOSPos != 2 {
		t.Fatalf("positions wrong: %+v", res)
	}

	tsOnly := `*filter
-A FORWARD -j ts-forward
COMMIT
`
	res = auditTsForwardOrder(tsOnly)
	if res.Misplaced() {
		t.Fatalf("ts-forward only must not be misplaced: %+v", res)
	}

	ubiosOnly := `*filter
-A FORWARD -j UBIOS_FORWARD_JUMP
COMMIT
`
	res = auditTsForwardOrder(ubiosOnly)
	if res.Misplaced() {
		t.Fatalf("UBIOS only must not be misplaced: %+v", res)
	}
}

// SEC-C15: when ts-forward is misplaced, AuditAndFixTsForwardOrder must
// (1) delete the misplaced rule and (2) re-insert it at the position
// immediately following UBIOS_FORWARD_JUMP. This locks the exact iptables
// command shape the patch contract requires.
func TestAuditAndFixTsForwardOrder_DeletesAndReinsertsAtCorrectPosition(t *testing.T) {
	misplaced := `*filter
:FORWARD ACCEPT [0:0]
-A FORWARD -j ts-forward
-A FORWARD -j UBIOS_FORWARD_JUMP
-A FORWARD -j SOME_OTHER
COMMIT
`
	fm := &FirewallManager{}
	fm.filterCache = misplaced
	fm.filterTime = time.Now()

	var calls [][]string
	orig := iptablesRunHook
	iptablesRunHook = func(_ context.Context, args ...string) error {
		calls = append(calls, append([]string(nil), args...))
		return nil
	}
	t.Cleanup(func() { iptablesRunHook = orig })

	if err := fm.AuditAndFixTsForwardOrder(context.Background()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 iptables calls (delete then insert); got %d: %+v", len(calls), calls)
	}
	wantDel := []string{"-w", "2", "-t", "filter", "-D", "FORWARD", "-j", "ts-forward"}
	if !equalArgs(calls[0], wantDel) {
		t.Fatalf("delete args wrong:\nwant %q\ngot  %q", wantDel, calls[0])
	}
	// ts at 1, UBIOS at 2: after deletion UBIOS shifts to pos 1; new
	// ts-forward slot is at UBIOSPos (original index 2). The original
	// UBIOSPos is preserved in the result because UBIOS-after-delete
	// occupies index 1 and inserting at 2 puts ts AFTER it.
	wantIns := []string{"-w", "2", "-t", "filter", "-I", "FORWARD", "2", "-j", "ts-forward"}
	if !equalArgs(calls[1], wantIns) {
		t.Fatalf("insert args wrong:\nwant %q\ngot  %q", wantIns, calls[1])
	}
}

// SEC-C15: when ordering is already correct, no iptables mutations must
// run. Idempotency is what makes safely calling the audit on every
// reconcile tick acceptable.
func TestAuditAndFixTsForwardOrder_NoopWhenCorrect(t *testing.T) {
	correct := `*filter
-A FORWARD -j UBIOS_FORWARD_JUMP
-A FORWARD -j ts-forward
COMMIT
`
	fm := &FirewallManager{}
	fm.filterCache = correct
	fm.filterTime = time.Now()

	var calls int
	orig := iptablesRunHook
	iptablesRunHook = func(_ context.Context, _ ...string) error {
		calls++
		return nil
	}
	t.Cleanup(func() { iptablesRunHook = orig })

	if err := fm.AuditAndFixTsForwardOrder(context.Background()); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if calls != 0 {
		t.Fatalf("expected 0 iptables calls on correct ordering, got %d", calls)
	}
}

func equalArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
