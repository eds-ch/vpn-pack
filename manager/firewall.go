package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"unifi-tailscale/manager/udapi"
)

var errIntegrationNotConfigured = errors.New("integration API not configured")

type FirewallManager struct {
	udapi    *udapi.UDAPIClient
	ic       *IntegrationClient
	manifest ManifestStore
}

func (fm *FirewallManager) requireIntegration() error {
	if fm.ic == nil || !fm.ic.HasAPIKey() || fm.manifest == nil || !fm.manifest.HasSiteID() {
		return errIntegrationNotConfigured
	}
	return nil
}

func (fm *FirewallManager) IntegrationReady() bool {
	return fm.requireIntegration() == nil
}

func NewFirewallManager(socketPath string, ic *IntegrationClient, manifest ManifestStore) *FirewallManager {
	return &FirewallManager{
		udapi:    udapi.NewClient(socketPath),
		ic:       ic,
		manifest: manifest,
	}
}

func (fm *FirewallManager) SetupTailscaleFirewall(ctx context.Context) *FirewallSetupResult {
	result := &FirewallSetupResult{ChainPrefix: defaultChainPrefix}

	if err := fm.requireIntegration(); err != nil {
		slog.Info("skipping tailscale firewall setup: integration not configured")
		return result
	}

	siteID := fm.manifest.GetSiteID()
	zone, err := fm.ic.EnsureZone(ctx, siteID, "VPN Pack: Tailscale")
	if err != nil {
		slog.Warn("Integration API zone setup failed, using UDAPI only", "err", err)
		result.addError("zone", err)
	} else {
		result.ZoneCreated = true
		result.ZoneID = zone.ID
		result.ZoneName = zone.Name
		slog.Info("integration zone ready", "zoneId", zone.ID, "name", zone.Name)

		policyIDs, err := fm.ic.EnsurePolicies(ctx, siteID, "Tailscale", zone.ID)
		if err != nil {
			slog.Warn("integration policy setup had errors, will retry on next restart", "err", err)
			result.addError("policies", err)
		} else {
			result.PoliciesReady = true
			result.PolicyIDs = policyIDs
			slog.Info("integration policies ready", "count", len(policyIDs))
		}

		discovered := fm.discoverChainPrefix(zone.ID)
		if discovered != "" {
			result.ChainPrefix = discovered
		}

		if err := fm.manifest.SetTailscaleZone(zone.ID, zone.Name, policyIDs, result.ChainPrefix); err != nil {
			slog.Warn("manifest save failed", "err", err)
			result.addError("manifest", err)
		}
	}

	marker := firewallMarker
	oldPrefix := fm.manifest.GetTailscaleChainPrefix()
	if result.ChainPrefix != oldPrefix {
		_ = udapi.RemoveInterfaceRules(fm.udapi, tailscaleInterface, marker)
	}

	if err := fm.ensureTailscaleRules(result.ChainPrefix); err != nil {
		result.addError("udapi", err)
		return result
	}
	result.UDAPIApplied = true

	slog.Info("tailscale firewall setup complete", "chainPrefix", result.ChainPrefix)
	return result
}

func (fm *FirewallManager) SetupWgS2sZone(ctx context.Context, tunnelID, zoneID, zoneName string) *FirewallSetupResult {
	result := &FirewallSetupResult{ChainPrefix: defaultChainPrefix}

	if err := fm.requireIntegration(); err != nil {
		result.addError("integration", err)
		return result
	}
	siteID := fm.manifest.GetSiteID()

	if zoneID != "" {
		for _, zm := range fm.manifest.GetWgS2sSnapshot() {
			if zm.ZoneID == zoneID {
				if err := fm.manifest.SetWgS2sZone(tunnelID, zm); err != nil {
					result.addError("manifest", err)
					return result
				}
				result.ZoneCreated = true
				result.ZoneID = zm.ZoneID
				result.ZoneName = zm.ZoneName
				result.PoliciesReady = len(zm.PolicyIDs) > 0
				result.PolicyIDs = zm.PolicyIDs
				result.ChainPrefix = zm.ChainPrefix
				return result
			}
		}
		result.addError("zone", fmt.Errorf("zone %s not found in manifest", zoneID))
		return result
	}

	if zoneName == "" {
		zoneName = "WireGuard S2S"
	}
	zoneDisplayName := "VPN Pack: " + zoneName

	zone, err := fm.ic.EnsureZone(ctx, siteID, zoneDisplayName)
	if err != nil {
		result.addError("zone", fmt.Errorf("ensure zone %q: %w", zoneDisplayName, err))
		return result
	}
	result.ZoneCreated = true
	result.ZoneID = zone.ID
	result.ZoneName = zone.Name
	slog.Info("wg-s2s integration zone ready", "zoneId", zone.ID, "name", zone.Name)

	policyIDs, err := fm.ic.EnsurePolicies(ctx, siteID, zoneName, zone.ID)
	if err != nil {
		slog.Warn("wg-s2s integration policy setup had errors", "err", err)
		result.addError("policies", err)
	} else {
		result.PoliciesReady = true
		result.PolicyIDs = policyIDs
		slog.Info("wg-s2s integration policies ready", "count", len(policyIDs))
	}

	chainPrefix := fm.discoverChainPrefix(zone.ID)
	if chainPrefix == "" {
		chainPrefix = defaultChainPrefix
	}
	result.ChainPrefix = chainPrefix

	if err := fm.manifest.SetWgS2sZone(tunnelID, ZoneManifest{ZoneID: zone.ID, ZoneName: zone.Name, PolicyIDs: policyIDs, ChainPrefix: chainPrefix}); err != nil {
		result.addError("manifest", fmt.Errorf("save manifest: %w", err))
		return result
	}
	return result
}

func (fm *FirewallManager) SetupWgS2sFirewall(ctx context.Context, tunnelID, iface string, allowedIPs []string) error {
	chainPrefix := fm.manifest.GetWgS2sChainPrefix(tunnelID)

	if chainPrefix == defaultChainPrefix {
		if zm, ok := fm.manifest.GetWgS2sZone(tunnelID); ok && zm.ZoneID != "" {
			if rediscovered := fm.discoverChainPrefix(zm.ZoneID); rediscovered != "" {
				chainPrefix = rediscovered
				if err := fm.manifest.SetWgS2sZone(tunnelID, ZoneManifest{ZoneID: zm.ZoneID, ZoneName: zm.ZoneName, PolicyIDs: zm.PolicyIDs, ChainPrefix: chainPrefix}); err != nil {
					slog.Warn("manifest save failed", "err", err)
				}
			}
		}
	}

	marker := "wg-s2s-manager:" + iface
	if err := udapi.AddInterfaceRulesForZone(fm.udapi, iface, marker, chainPrefix); err != nil {
		return err
	}

	if len(allowedIPs) > 0 {
		ipsetName := zoneIPSetName(chainPrefix)
		blocked := make(map[string]bool)
		if sys, err := CollectSystemSubnets(iface); err == nil {
			result := ValidateAllowedIPs(allowedIPs, sys)
			for _, b := range result.Blocked {
				slog.Warn("skipping conflicting ipset entry", "cidr", b.CIDR, "conflictsWith", b.ConflictsWith, "iface", b.Interface)
				blocked[b.CIDR] = true
			}
		}
		for _, cidr := range allowedIPs {
			if blocked[cidr] {
				continue
			}
			if err := udapi.EnsureZoneSubnet(fm.udapi, ipsetName, cidr); err != nil {
				slog.Warn("wg-s2s zone ipset failed", "ipset", ipsetName, "cidr", cidr, "err", err)
			}
		}
	}

	return nil
}

func (fm *FirewallManager) RemoveWgS2sFirewall(ctx context.Context, tunnelID, iface string, allowedIPs []string) {
	marker := "wg-s2s-manager:" + iface
	if err := udapi.RemoveInterfaceRules(fm.udapi, iface, marker); err != nil {
		slog.Warn("wg-s2s firewall rule removal failed", "iface", iface, "err", err)
	}
	fm.RemoveWgS2sIPSetEntries(ctx, tunnelID, allowedIPs)
}

func (fm *FirewallManager) RemoveWgS2sIPSetEntries(ctx context.Context, tunnelID string, cidrs []string) {
	chainPrefix := fm.manifest.GetWgS2sChainPrefix(tunnelID)
	if chainPrefix == defaultChainPrefix || len(cidrs) == 0 {
		return
	}
	ipsetName := zoneIPSetName(chainPrefix)
	for _, cidr := range cidrs {
		if err := udapi.RemoveZoneSubnet(fm.udapi, ipsetName, cidr); err != nil {
			slog.Warn("wg-s2s ipset entry removal failed", "ipset", ipsetName, "cidr", cidr, "err", err)
		}
	}
}

func (fm *FirewallManager) OpenWanPort(ctx context.Context, port int, marker string) error {
	if err := fm.requireIntegration(); err != nil {
		return err
	}

	if existing := fm.manifest.GetWanPortPolicyID(marker); existing != "" {
		return nil
	}

	siteID := fm.manifest.GetSiteID()
	extID, gwID, err := fm.resolveSystemZones(ctx, siteID)
	if err != nil {
		return fmt.Errorf("resolve system zones: %w", err)
	}

	name := wanPortPolicyName(port, marker)
	policyID, err := fm.ic.EnsureWanPortPolicy(ctx, siteID, port, name, extID, gwID)
	if err != nil {
		return fmt.Errorf("ensure WAN port policy: %w", err)
	}

	if err := fm.manifest.SetWanPort(marker, policyID, name, port); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	slog.Info("WAN port policy created", "port", port, "marker", marker, "policyId", policyID)
	return nil
}

func (fm *FirewallManager) CloseWanPort(ctx context.Context, port int, marker string) error {
	if err := fm.requireIntegration(); err != nil {
		return err
	}

	policyID := fm.manifest.GetWanPortPolicyID(marker)
	if policyID == "" {
		return nil
	}

	siteID := fm.manifest.GetSiteID()
	if err := fm.ic.DeletePolicy(ctx, siteID, policyID); err != nil {
		if errors.Is(err, ErrNotFound) {
			slog.Info("WAN port policy already gone from API", "marker", marker, "policyId", policyID)
		} else {
			return fmt.Errorf("delete WAN port policy: %w", err)
		}
	}

	if err := fm.manifest.RemoveWanPort(marker); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	slog.Info("WAN port policy deleted", "port", port, "marker", marker)
	return nil
}

func (fm *FirewallManager) resolveSystemZones(ctx context.Context, siteID string) (string, string, error) {
	if extID, gwID := fm.manifest.GetSystemZoneIDs(); extID != "" && gwID != "" {
		return extID, gwID, nil
	}

	extID, gwID, err := fm.ic.FindSystemZoneIDs(ctx, siteID)
	if err != nil {
		return "", "", fmt.Errorf("find system zones: %w", err)
	}

	if err := fm.manifest.SetSystemZoneIDs(extID, gwID); err != nil {
		return "", "", fmt.Errorf("save manifest: %w", err)
	}

	return extID, gwID, nil
}

func (fm *FirewallManager) EnsureDNSForwarding(ctx context.Context, magicDNSSuffix string) error {
	if err := fm.requireIntegration(); err != nil {
		return err
	}

	if entry, ok := fm.manifest.GetDNSPolicy(dnsMarkerTailscale); ok {
		if entry.Domain == magicDNSSuffix {
			return nil
		}
		siteID := fm.manifest.GetSiteID()
		if err := fm.ic.DeleteDNSPolicy(ctx, siteID, entry.PolicyID); err != nil && !errors.Is(err, ErrNotFound) {
			slog.Warn("failed to delete old DNS forwarding policy", "domain", entry.Domain, "err", err)
		}
		if err := fm.manifest.RemoveDNSPolicy(dnsMarkerTailscale); err != nil {
			slog.Warn("failed to remove old DNS policy from manifest", "err", err)
		}
	}

	siteID := fm.manifest.GetSiteID()
	pol, err := fm.ic.EnsureDNSForwardDomain(ctx, siteID, magicDNSSuffix, tailscaleDNSResolverIP)
	if err != nil {
		return fmt.Errorf("create DNS forward domain: %w", err)
	}

	if err := fm.manifest.SetDNSPolicy(dnsMarkerTailscale, pol.ID, magicDNSSuffix, tailscaleDNSResolverIP); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	slog.Info("DNS forwarding policy created", "domain", magicDNSSuffix, "resolver", tailscaleDNSResolverIP, "policyId", pol.ID)
	return nil
}

func (fm *FirewallManager) RemoveDNSForwarding(ctx context.Context) error {
	entry, ok := fm.manifest.GetDNSPolicy(dnsMarkerTailscale)
	if !ok {
		return nil
	}

	if err := fm.requireIntegration(); err == nil {
		siteID := fm.manifest.GetSiteID()
		if err := fm.ic.DeleteDNSPolicy(ctx, siteID, entry.PolicyID); err != nil && !errors.Is(err, ErrNotFound) {
			slog.Warn("failed to delete DNS forwarding policy from API", "policyId", entry.PolicyID, "err", err)
		}
	}

	if err := fm.manifest.RemoveDNSPolicy(dnsMarkerTailscale); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	slog.Info("DNS forwarding policy removed", "domain", entry.Domain)
	return nil
}

func (fm *FirewallManager) RestoreTailscaleRules(ctx context.Context) error {
	if err := fm.requireIntegration(); err != nil {
		return nil
	}

	chainPrefix := fm.manifest.GetTailscaleChainPrefix()

	marker := firewallMarker

	ts := fm.manifest.GetTailscaleZone()
	if chainPrefix == defaultChainPrefix && ts.ZoneID != "" {
		if rediscovered := fm.discoverChainPrefix(ts.ZoneID); rediscovered != "" {
			_ = udapi.RemoveInterfaceRules(fm.udapi, tailscaleInterface, marker)
			chainPrefix = rediscovered
			if err := fm.manifest.SetTailscaleZone(ts.ZoneID, ts.ZoneName, ts.PolicyIDs, rediscovered); err != nil {
				slog.Warn("manifest save failed", "err", err)
			}
			slog.Info("tailscale chain prefix re-discovered", "prefix", rediscovered)
		}
	}

	return fm.ensureTailscaleRules(chainPrefix)
}

func (fm *FirewallManager) ensureTailscaleRules(chainPrefix string) error {
	if chainPrefix != defaultChainPrefix {
		fwd := hasChainRule(chainForwardInUser, "-i "+tailscaleInterface)
		inp := hasChainRule(chainInputUserHook, "-i "+tailscaleInterface)
		out := hasChainRule(chainOutputUserHook, "-o "+tailscaleInterface)
		ipsetOK := hasIPSetEntry(fmt.Sprintf("UBIOS4%s_subnets", chainPrefix), tailscaleCGNAT)
		if fwd && inp && out && ipsetOK {
			return nil
		}
	}

	marker := firewallMarker
	if err := udapi.AddInterfaceRulesForZone(fm.udapi, tailscaleInterface, marker, chainPrefix); err != nil {
		return err
	}

	ipsetName := zoneIPSetName(chainPrefix)
	if err := udapi.EnsureZoneSubnet(fm.udapi, ipsetName, tailscaleCGNAT); err != nil {
		slog.Warn("zone ipset failed", "ipset", ipsetName, "err", err)
	}
	return nil
}

func (fm *FirewallManager) CheckTailscaleRulesPresent(ctx context.Context) (forward, input, output, ipset bool) {
	prefix := fm.manifest.GetTailscaleChainPrefix()
	forward = hasChainRule(chainForwardInUser, "-i "+tailscaleInterface) ||
		hasChainRule(fmt.Sprintf("UBIOS_%s_IN", prefix), "-i "+tailscaleInterface)
	input = hasChainRule(chainInputUserHook, "-i "+tailscaleInterface) ||
		hasChainRule(fmt.Sprintf("UBIOS_%s_LOCAL", prefix), "-i "+tailscaleInterface)
	output = hasChainRule(chainOutputUserHook, "-o "+tailscaleInterface) ||
		hasChainRule(fmt.Sprintf("UBIOS_LOCAL_%s", prefix), "-o "+tailscaleInterface)

	ipset = hasIPSetEntry(fmt.Sprintf("UBIOS4%s_subnets", prefix), tailscaleCGNAT)
	return
}

func (fm *FirewallManager) CheckWgS2sRulesPresent(ctx context.Context, ifaces []string) map[string]bool {
	result := make(map[string]bool, len(ifaces))
	for _, iface := range ifaces {
		forward := hasChainRule(chainForwardInUser, "-i "+iface)
		input := hasChainRule(chainInputUserHook, "-i "+iface)
		output := hasChainRule(chainOutputUserHook, "-o "+iface)
		result[iface] = forward && input && output
	}
	return result
}

func (fm *FirewallManager) discoverChainPrefix(zoneID string) string {
	if zoneID == "" {
		return ""
	}

	prefix := discoverChainPrefixFromMongo(zoneID)
	if prefix != "" {
		chain := fmt.Sprintf("UBIOS_%s_IN_USER", prefix)
		if hasChainRule(chain, "") {
			slog.Info("chain prefix discovered via MongoDB", "zoneId", zoneID, "prefix", prefix)
			return prefix
		}
		slog.Warn("discovered chain missing in iptables", "prefix", prefix, "chain", chain)
	}

	return ""
}

func discoverChainPrefixFromMongo(zoneID string) string {
	script := `db.getSiblingDB("ace").firewall_zone.find({default_zone:false}).sort({_id:1}).forEach(function(z){print(z.external_id.toString())})`
	out, err := exec.Command("mongo", "--port", mongoPort, "--quiet", "--eval", script).Output()
	if err != nil {
		slog.Debug("mongo chain prefix query failed", "err", err)
		return ""
	}

	for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		cleaned := stripUUIDWrapper(strings.TrimSpace(line))
		if cleaned == zoneID {
			return fmt.Sprintf("CUSTOM%d", i+1)
		}
	}
	return ""
}

func stripUUIDWrapper(s string) string {
	if strings.HasPrefix(s, `UUID("`) && strings.HasSuffix(s, `")`) {
		return s[6 : len(s)-2]
	}
	return s
}

func zoneIPSetName(chainPrefix string) string {
	return chainPrefix + "_subnets"
}

var (
	filterRulesCacheMu   sync.Mutex
	filterRulesCache     string
	filterRulesCacheTime time.Time
)

func cachedFilterRules() string {
	filterRulesCacheMu.Lock()
	defer filterRulesCacheMu.Unlock()
	if filterRulesCache != "" && time.Since(filterRulesCacheTime) < time.Second {
		return filterRulesCache
	}
	out, err := exec.Command("iptables-save", "-t", "filter").Output()
	if err != nil {
		return ""
	}
	filterRulesCache = string(out)
	filterRulesCacheTime = time.Now()
	return filterRulesCache
}

func hasChainRuleIn(rules, chain, match string) bool {
	if match == "" {
		return strings.Contains(rules, "\n:"+chain+" ") ||
			strings.HasPrefix(rules, ":"+chain+" ")
	}
	prefix := "-A " + chain + " "
	for _, line := range strings.Split(rules, "\n") {
		if strings.HasPrefix(line, prefix) && strings.Contains(line, match) {
			return true
		}
	}
	return false
}

func hasChainRule(chain, match string) bool {
	if rules := cachedFilterRules(); rules != "" {
		return hasChainRuleIn(rules, chain, match)
	}
	out, err := exec.Command("iptables", "-w", "2", "-S", chain).Output()
	if err != nil {
		return false
	}
	if match == "" {
		return true
	}
	return strings.Contains(string(out), match)
}

func hasIPSetEntry(setName, match string) bool {
	out, err := exec.Command("ipset", "list", setName).Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), match)
}
