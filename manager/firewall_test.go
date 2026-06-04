package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

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
	fm.addInterfaceRules = func(iface, marker, chainPrefix string) error {
		f.addCalls = append(f.addCalls, iface+"/"+marker+"/"+chainPrefix)
		return nil
	}
	fm.removeInterfaceRules = func(iface, marker string) error {
		f.removeCalls = append(f.removeCalls, iface+"/"+marker)
		return nil
	}
	fm.ensureZoneSubnets = func(setName string, cidrs []string) error {
		f.ensureCalls = append(f.ensureCalls, setName+":"+strings.Join(cidrs, ","))
		if f.failIpsetFill {
			return fmt.Errorf("simulated ipset fill failure")
		}
		return nil
	}
	fm.removeZoneSubnet = func(setName, cidr string) error {
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
