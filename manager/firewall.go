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

	"golang.org/x/sync/singleflight"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/service"
	"unifi-tailscale/manager/udapi"
)

// Duplicated in service/firewall.go — different packages, can't share.
var errIntegrationNotConfigured = errors.New("integration API not configured")

type FirewallManager struct {
	udapi    *udapi.UDAPIClient
	ic       *IntegrationClient
	manifest ManifestStore
}

func (fm *FirewallManager) IntegrationReady() bool {
	return fm.ic != nil && fm.ic.HasAPIKey() && fm.manifest != nil && fm.manifest.HasSiteID()
}

func NewFirewallManager(socketPath string, ic *IntegrationClient, manifest ManifestStore) *FirewallManager {
	return &FirewallManager{
		udapi:    udapi.NewClient(socketPath),
		ic:       ic,
		manifest: manifest,
	}
}

func (fm *FirewallManager) SetupWgS2sFirewall(ctx context.Context, tunnelID, iface string, allowedIPs []string) error {
	chainPrefix := fm.manifest.GetWgS2sChainPrefix(tunnelID)

	if chainPrefix == config.DefaultChainPrefix {
		if zm, ok := fm.manifest.GetWgS2sZone(tunnelID); ok && zm.ZoneID != "" {
			if rediscovered := fm.DiscoverChainPrefix(zm.ZoneID); rediscovered != "" {
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

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if len(allowedIPs) > 0 {
		ipsetName := zoneIPSetName(chainPrefix)
		blocked := make(map[string]bool)
		if sys, err := service.CollectSystemSubnets(iface); err == nil {
			result := service.ValidateAllowedIPs(allowedIPs, sys)
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
	if chainPrefix == config.DefaultChainPrefix || len(cidrs) == 0 {
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
	if !fm.IntegrationReady() {
		return errIntegrationNotConfigured
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
	if !fm.IntegrationReady() {
		return errIntegrationNotConfigured
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
	if !fm.IntegrationReady() {
		return errIntegrationNotConfigured
	}

	if entry, ok := fm.manifest.GetDNSPolicy(config.DNSMarkerTailscale); ok {
		if entry.Domain == magicDNSSuffix {
			return nil
		}
		siteID := fm.manifest.GetSiteID()
		if err := fm.ic.DeleteDNSPolicy(ctx, siteID, entry.PolicyID); err != nil && !errors.Is(err, ErrNotFound) {
			slog.Warn("failed to delete old DNS forwarding policy", "domain", entry.Domain, "err", err)
		}
		if err := fm.manifest.RemoveDNSPolicy(config.DNSMarkerTailscale); err != nil {
			slog.Warn("failed to remove old DNS policy from manifest", "err", err)
		}
	}

	siteID := fm.manifest.GetSiteID()
	pol, err := fm.ic.EnsureDNSForwardDomain(ctx, siteID, magicDNSSuffix, config.TailscaleDNSResolverIP)
	if err != nil {
		return fmt.Errorf("create DNS forward domain: %w", err)
	}

	if err := fm.manifest.SetDNSPolicy(config.DNSMarkerTailscale, pol.ID, magicDNSSuffix, config.TailscaleDNSResolverIP); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	slog.Info("DNS forwarding policy created", "domain", magicDNSSuffix, "resolver", config.TailscaleDNSResolverIP, "policyId", pol.ID)
	return nil
}

func (fm *FirewallManager) RemoveDNSForwarding(ctx context.Context) error {
	entry, ok := fm.manifest.GetDNSPolicy(config.DNSMarkerTailscale)
	if !ok {
		return nil
	}

	if fm.IntegrationReady() {
		siteID := fm.manifest.GetSiteID()
		if err := fm.ic.DeleteDNSPolicy(ctx, siteID, entry.PolicyID); err != nil && !errors.Is(err, ErrNotFound) {
			slog.Warn("failed to delete DNS forwarding policy from API", "policyId", entry.PolicyID, "err", err)
		}
	}

	if err := fm.manifest.RemoveDNSPolicy(config.DNSMarkerTailscale); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	slog.Info("DNS forwarding policy removed", "domain", entry.Domain)
	return nil
}

func (fm *FirewallManager) RestoreRulesWithRetry(ctx context.Context, retries int, delay time.Duration) {
	retryLoop(ctx, retries, delay, fm.RestoreTailscaleRules)
}

func retryLoop(ctx context.Context, retries int, delay time.Duration, fn func(context.Context) error) {
	for i := range retries {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}
		}
		if err := fn(ctx); err != nil {
			slog.Warn("retry failed", "attempt", i+1, "err", err)
			continue
		}
		return
	}
}

func (fm *FirewallManager) RestoreTailscaleRules(ctx context.Context) error {
	if !fm.IntegrationReady() {
		return nil
	}

	chainPrefix := fm.manifest.GetTailscaleChainPrefix()

	marker := config.FirewallMarker

	ts := fm.manifest.GetTailscaleZone()
	if chainPrefix == config.DefaultChainPrefix && ts.ZoneID != "" {
		if rediscovered := fm.DiscoverChainPrefix(ts.ZoneID); rediscovered != "" {
			_ = udapi.RemoveInterfaceRules(fm.udapi, config.TailscaleInterface, marker)
			chainPrefix = rediscovered
			if err := fm.manifest.SetTailscaleZone(ts.ZoneID, ts.ZoneName, ts.PolicyIDs, rediscovered); err != nil {
				slog.Warn("manifest save failed", "err", err)
			}
			slog.Info("tailscale chain prefix re-discovered", "prefix", rediscovered)
		}
	}

	return fm.EnsureTailscaleRules(chainPrefix)
}

func (fm *FirewallManager) RemoveTailscaleInterfaceRules() error {
	return udapi.RemoveInterfaceRules(fm.udapi, config.TailscaleInterface, config.FirewallMarker)
}

func (fm *FirewallManager) EnsureTailscaleRules(chainPrefix string) error {
	if chainPrefix != config.DefaultChainPrefix {
		fwd := hasChainRule(config.ChainForwardInUser, "-i "+config.TailscaleInterface)
		inp := hasChainRule(config.ChainInputUserHook, "-i "+config.TailscaleInterface)
		out := hasChainRule(config.ChainOutputUserHook, "-o "+config.TailscaleInterface)
		ipsetOK := hasIPSetEntry(fmt.Sprintf("UBIOS4%s_subnets", chainPrefix), config.TailscaleCGNAT)
		if fwd && inp && out && ipsetOK {
			return nil
		}
	}

	marker := config.FirewallMarker
	if err := udapi.AddInterfaceRulesForZone(fm.udapi, config.TailscaleInterface, marker, chainPrefix); err != nil {
		return err
	}

	ipsetName := zoneIPSetName(chainPrefix)
	if err := udapi.EnsureZoneSubnet(fm.udapi, ipsetName, config.TailscaleCGNAT); err != nil {
		slog.Warn("zone ipset failed", "ipset", ipsetName, "err", err)
	}
	return nil
}

func (fm *FirewallManager) CheckTailscaleRulesPresent(ctx context.Context) (forward, input, output, ipset bool) {
	prefix := fm.manifest.GetTailscaleChainPrefix()
	forward = hasChainRule(config.ChainForwardInUser, "-i "+config.TailscaleInterface) ||
		hasChainRule(fmt.Sprintf("UBIOS_%s_IN", prefix), "-i "+config.TailscaleInterface)
	input = hasChainRule(config.ChainInputUserHook, "-i "+config.TailscaleInterface) ||
		hasChainRule(fmt.Sprintf("UBIOS_%s_LOCAL", prefix), "-i "+config.TailscaleInterface)
	output = hasChainRule(config.ChainOutputUserHook, "-o "+config.TailscaleInterface) ||
		hasChainRule(fmt.Sprintf("UBIOS_LOCAL_%s", prefix), "-o "+config.TailscaleInterface)

	ipset = hasIPSetEntry(fmt.Sprintf("UBIOS4%s_subnets", prefix), config.TailscaleCGNAT)
	return
}

func (fm *FirewallManager) CheckWgS2sRulesPresent(ctx context.Context, ifaces []string) map[string]bool {
	result := make(map[string]bool, len(ifaces))
	for _, iface := range ifaces {
		forward := hasChainRule(config.ChainForwardInUser, "-i "+iface)
		input := hasChainRule(config.ChainInputUserHook, "-i "+iface)
		output := hasChainRule(config.ChainOutputUserHook, "-o "+iface)
		result[iface] = forward && input && output
	}
	return result
}

func (fm *FirewallManager) DiscoverChainPrefix(zoneID string) string {
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
	out, err := exec.Command("mongo", "--port", config.MongoPort, "--quiet", "--eval", script).Output()
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
	filterRulesFlight    singleflight.Group
)

func cachedFilterRules() string {
	filterRulesCacheMu.Lock()
	if filterRulesCache != "" && time.Since(filterRulesCacheTime) < time.Second {
		c := filterRulesCache
		filterRulesCacheMu.Unlock()
		return c
	}
	filterRulesCacheMu.Unlock()

	v, _, _ := filterRulesFlight.Do("iptables-save", func() (any, error) {
		out, err := exec.Command("iptables-save", "-t", "filter").Output()
		if err != nil {
			return "", err
		}
		result := string(out)

		filterRulesCacheMu.Lock()
		filterRulesCache = result
		filterRulesCacheTime = time.Now()
		filterRulesCacheMu.Unlock()
		return result, nil
	})
	return v.(string)
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
