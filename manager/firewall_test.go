package main

import (
	"context"
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
