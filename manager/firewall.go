package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/ops"
	"unifi-tailscale/manager/service"
	"unifi-tailscale/manager/udapi"
)

const wgS2sMarkerPrefix = "wg-s2s-manager:"

// Duplicated in service/firewall.go — different packages, can't share.
var errIntegrationNotConfigured = errors.New("integration API not configured")

type FirewallManager struct {
	udapi    *udapi.UDAPIClient
	ic       IntegrationAPI
	manifest ManifestStore
	bgWg     sync.WaitGroup

	filterMu     sync.Mutex
	filterCache  string
	filterTime   time.Time
	filterFlight singleflight.Group

	ipsetMu     sync.Mutex
	ipsetCache  string
	ipsetSet    string
	ipsetTime   time.Time
	ipsetFlight singleflight.Group

	mongoMu    sync.Mutex
	mongoCache map[string]string
	mongoTime  time.Time

	// chainProbe / ipsetProbe are overridable seams so unit tests can drive
	// the rule-presence checks without shelling out to iptables/ipset.
	chainProbe func(chain, match string) bool
	ipsetProbe func(setName, match string) bool

	// UDAPI write-side seams. Tests substitute fakes; production wires them
	// to the real udapi.* helpers in NewFirewallManager.
	addInterfaceRules    func(ctx context.Context, iface, marker, chainPrefix string) error
	removeInterfaceRules func(ctx context.Context, iface, marker string) error
	ensureZoneSubnets    func(ctx context.Context, setName string, cidrs []string) error
	removeZoneSubnet     func(ctx context.Context, setName, cidr string) error
}

func (fm *FirewallManager) IntegrationReady() bool {
	return fm.ic != nil && fm.ic.HasAPIKey() && fm.manifest != nil && fm.manifest.HasSiteID()
}

func NewFirewallManager(socketPath string, ic IntegrationAPI, manifest ManifestStore) *FirewallManager {
	fm := &FirewallManager{
		udapi:    udapi.NewClient(socketPath),
		ic:       ic,
		manifest: manifest,
	}
	fm.chainProbe = fm.hasChainRule
	fm.ipsetProbe = fm.hasIPSetEntry
	fm.addInterfaceRules = func(ctx context.Context, iface, marker, chainPrefix string) error {
		return udapi.AddInterfaceRulesForZone(ctx, fm.udapi, iface, marker, chainPrefix)
	}
	fm.removeInterfaceRules = func(ctx context.Context, iface, marker string) error {
		return udapi.RemoveInterfaceRules(ctx, fm.udapi, iface, marker)
	}
	fm.ensureZoneSubnets = func(ctx context.Context, setName string, cidrs []string) error {
		return udapi.EnsureZoneSubnets(ctx, fm.udapi, setName, cidrs)
	}
	fm.removeZoneSubnet = func(ctx context.Context, setName, cidr string) error {
		return udapi.RemoveZoneSubnet(ctx, fm.udapi, setName, cidr)
	}
	return fm
}

func (fm *FirewallManager) SetupWgS2sFirewall(ctx context.Context, tunnelID, iface string, allowedIPs []string) error {
	chainPrefix := fm.manifest.GetWgS2sChainPrefix(tunnelID)

	if chainPrefix == config.DefaultChainPrefix {
		if zm, ok := fm.manifest.GetWgS2sZone(tunnelID); ok && zm.ZoneID != "" {
			chainPrefix = fm.rediscoverAndSaveWgS2s(ctx, tunnelID, zm, chainPrefix)
		}
	}

	marker := wgS2sMarkerPrefix + iface
	ipsetName := zoneIPSetName(chainPrefix)

	filtered := fm.filterBlockedAllowedIPs(iface, allowedIPs)

	steps := []ops.Op{
		{
			Name: "install wg-s2s chain rules",
			Do:   func(ctx context.Context) error { return fm.addInterfaceRules(ctx, iface, marker, chainPrefix) },
			Undo: func(ctx context.Context) error { return fm.removeInterfaceRules(ctx, iface, marker) },
		},
	}
	if len(filtered) > 0 {
		steps = append(steps, ops.Op{
			Name: "fill wg-s2s ipset",
			Do:   func(ctx context.Context) error { return fm.ensureZoneSubnets(ctx, ipsetName, filtered) },
			Undo: func(ctx context.Context) error {
				for _, cidr := range filtered {
					_ = fm.removeZoneSubnet(ctx, ipsetName, cidr)
				}
				return nil
			},
		})
	}
	return ops.Run(ctx, steps)
}

func (fm *FirewallManager) filterBlockedAllowedIPs(iface string, allowedIPs []string) []string {
	if len(allowedIPs) == 0 {
		return nil
	}
	blocked := make(map[string]bool)
	if sys, err := service.CollectSystemSubnets(iface); err == nil {
		result := service.ValidateAllowedIPs(allowedIPs, sys)
		for _, b := range result.Blocked {
			slog.Warn("skipping conflicting ipset entry", "cidr", b.CIDR, "conflictsWith", b.ConflictsWith, "iface", b.Interface)
			blocked[b.CIDR] = true
		}
	}
	filtered := make([]string, 0, len(allowedIPs))
	for _, cidr := range allowedIPs {
		if !blocked[cidr] {
			filtered = append(filtered, cidr)
		}
	}
	return filtered
}

func (fm *FirewallManager) rediscoverAndSaveWgS2s(ctx context.Context, tunnelID string, zm ZoneManifest, current string) string {
	rediscovered := fm.DiscoverChainPrefix(ctx, zm.ZoneID)
	if rediscovered == "" {
		return current
	}
	if err := fm.manifest.SetWgS2sZone(tunnelID, ZoneManifest{ZoneID: zm.ZoneID, ZoneName: zm.ZoneName, PolicyIDs: zm.PolicyIDs, ChainPrefix: rediscovered}); err != nil {
		slog.Warn("manifest save failed", "err", err)
	}
	return rediscovered
}

func (fm *FirewallManager) RemoveWgS2sFirewall(ctx context.Context, tunnelID, iface string, allowedIPs []string) {
	marker := wgS2sMarkerPrefix + iface
	if err := udapi.RemoveInterfaceRules(ctx, fm.udapi, iface, marker); err != nil {
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
		if err := udapi.RemoveZoneSubnet(ctx, fm.udapi, ipsetName, cidr); err != nil {
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
	fm.bgWg.Add(1)
	go func() {
		defer fm.bgWg.Done()
		retryLoop(ctx, retries, delay, fm.RestoreTailscaleRules)
	}()
}

func (fm *FirewallManager) WaitBackground() {
	fm.bgWg.Wait()
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
		if rediscovered := fm.DiscoverChainPrefix(ctx, ts.ZoneID); rediscovered != "" {
			_ = udapi.RemoveInterfaceRules(ctx, fm.udapi, config.TailscaleInterface, marker)
			chainPrefix = rediscovered
			if err := fm.manifest.SetTailscaleZone(ts.ZoneID, ts.ZoneName, ts.PolicyIDs, rediscovered); err != nil {
				slog.Warn("manifest save failed", "err", err)
			}
			slog.Info("tailscale chain prefix re-discovered", "prefix", rediscovered)
		}
	}

	return fm.EnsureTailscaleRules(ctx, chainPrefix)
}

func (fm *FirewallManager) RemoveTailscaleInterfaceRules(ctx context.Context) error {
	return udapi.RemoveInterfaceRules(ctx, fm.udapi, config.TailscaleInterface, config.FirewallMarker)
}

func (fm *FirewallManager) EnsureTailscaleRules(ctx context.Context, chainPrefix string) error {
	if chainPrefix != config.DefaultChainPrefix {
		fwd := fm.hasChainRule(config.ChainForwardInUser, "-i "+config.TailscaleInterface)
		inp := fm.hasChainRule(config.ChainInputUserHook, "-i "+config.TailscaleInterface)
		out := fm.hasChainRule(config.ChainOutputUserHook, "-o "+config.TailscaleInterface)
		ipsetOK := fm.hasIPSetEntry(fmt.Sprintf("UBIOS4%s_subnets", chainPrefix), config.TailscaleCGNAT)
		if fwd && inp && out && ipsetOK {
			return nil
		}
	}

	marker := config.FirewallMarker
	if err := udapi.AddInterfaceRulesForZone(ctx, fm.udapi, config.TailscaleInterface, marker, chainPrefix); err != nil {
		return err
	}

	ipsetName := zoneIPSetName(chainPrefix)
	if err := udapi.EnsureZoneSubnet(ctx, fm.udapi, ipsetName, config.TailscaleCGNAT); err != nil {
		return fmt.Errorf("zone ipset %s: %w", ipsetName, err)
	}
	return nil
}

func (fm *FirewallManager) CheckTailscaleRulesPresent(ctx context.Context) (forward, input, output, ipset bool) {
	prefix := fm.manifest.GetTailscaleChainPrefix()
	forward = fm.hasChainRule(config.ChainForwardInUser, "-i "+config.TailscaleInterface) ||
		fm.hasChainRule(fmt.Sprintf("UBIOS_%s_IN", prefix), "-i "+config.TailscaleInterface)
	input = fm.hasChainRule(config.ChainInputUserHook, "-i "+config.TailscaleInterface) ||
		fm.hasChainRule(fmt.Sprintf("UBIOS_%s_LOCAL", prefix), "-i "+config.TailscaleInterface)
	output = fm.hasChainRule(config.ChainOutputUserHook, "-o "+config.TailscaleInterface) ||
		fm.hasChainRule(fmt.Sprintf("UBIOS_LOCAL_%s", prefix), "-o "+config.TailscaleInterface)

	ipset = fm.hasIPSetEntry(fmt.Sprintf("UBIOS4%s_subnets", prefix), config.TailscaleCGNAT)
	return
}

func (fm *FirewallManager) CheckWgS2sRulesPresent(ctx context.Context, specs []domain.WgS2sCheckSpec) map[string]bool {
	result := make(map[string]bool, len(specs))
	for _, spec := range specs {
		forward := fm.chainProbe(config.ChainForwardInUser, "-i "+spec.InterfaceName)
		input := fm.chainProbe(config.ChainInputUserHook, "-i "+spec.InterfaceName)
		output := fm.chainProbe(config.ChainOutputUserHook, "-o "+spec.InterfaceName)

		ipsetOK := true
		if spec.ChainPrefix != "" && len(spec.Subnets) > 0 {
			setName := fmt.Sprintf("UBIOS4%s_subnets", spec.ChainPrefix)
			for _, cidr := range spec.Subnets {
				if !fm.ipsetProbe(setName, cidr) {
					ipsetOK = false
					break
				}
			}
		}
		result[spec.InterfaceName] = forward && input && output && ipsetOK
	}
	return result
}

func (fm *FirewallManager) DiscoverChainPrefix(ctx context.Context, zoneID string) string {
	if zoneID == "" {
		return ""
	}

	prefix := fm.discoverChainPrefixFromMongo(ctx, zoneID)
	if prefix != "" {
		chain := fmt.Sprintf("UBIOS_%s_IN_USER", prefix)
		if fm.hasChainRule(chain, "") {
			slog.Info("chain prefix discovered via MongoDB", "zoneId", zoneID, "prefix", prefix)
			return prefix
		}
		slog.Warn("discovered chain missing in iptables", "prefix", prefix, "chain", chain)
	}

	return ""
}

func (fm *FirewallManager) discoverChainPrefixFromMongo(ctx context.Context, zoneID string) string {
	fm.mongoMu.Lock()
	if fm.mongoCache != nil && time.Since(fm.mongoTime) < 30*time.Second {
		if prefix, ok := fm.mongoCache[zoneID]; ok {
			fm.mongoMu.Unlock()
			return prefix
		}
	}
	fm.mongoMu.Unlock()

	script := `db.getSiblingDB("ace").firewall_zone.find({default_zone:false}).sort({_id:1}).forEach(function(z){print(z.external_id.toString())})`
	out, err := exec.CommandContext(ctx, "mongo", "--port", config.MongoPort, "--quiet", "--eval", script).Output()
	if err != nil {
		slog.Debug("mongo chain prefix query failed", "err", err)
		return ""
	}

	cache := make(map[string]string)
	for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		cleaned := stripUUIDWrapper(strings.TrimSpace(line))
		cache[cleaned] = fmt.Sprintf("CUSTOM%d", i+1)
	}

	fm.mongoMu.Lock()
	fm.mongoCache = cache
	fm.mongoTime = time.Now()
	fm.mongoMu.Unlock()

	return cache[zoneID]
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

// auditTsForwardOrderResult describes the FORWARD-chain order between
// tailscaled's `-j ts-forward` hook and UniFi's `-j UBIOS_FORWARD_JUMP`.
// SEC-C15: patch 005 sets the right order once at AddHooks time, but only
// the firewall-watcher can catch a later regression (manual flush, restore
// from save). Misplaced => ts-forward appears BEFORE UBIOS_FORWARD_JUMP,
// which means Tailscale's fallback ACCEPT runs before UniFi zone policies.
type auditTsForwardOrderResult struct {
	HasUBIOS     bool
	HasTSForward bool
	TSForwardPos int // 1-based, 0 if missing
	UBIOSPos     int // 1-based, 0 if missing
}

func (r auditTsForwardOrderResult) Misplaced() bool {
	return r.HasUBIOS && r.HasTSForward && r.TSForwardPos < r.UBIOSPos
}

// auditTsForwardOrder walks the FORWARD chain rules in iptables-save
// output. The input is the raw output of `iptables-save -t filter`.
func auditTsForwardOrder(rulesText string) auditTsForwardOrderResult {
	var res auditTsForwardOrderResult
	pos := 0
	prefix := "-A FORWARD "
	for _, line := range strings.Split(rulesText, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		pos++
		if strings.Contains(line, "-j ts-forward") && !res.HasTSForward {
			res.HasTSForward = true
			res.TSForwardPos = pos
		}
		if strings.Contains(line, "-j UBIOS_FORWARD_JUMP") && !res.HasUBIOS {
			res.HasUBIOS = true
			res.UBIOSPos = pos
		}
	}
	return res
}

// AuditAndFixTsForwardOrder verifies the FORWARD-chain ordering and, when
// ts-forward is misplaced, removes the rule and re-inserts it immediately
// after UBIOS_FORWARD_JUMP. Idempotent: no-op when ordering is correct.
func (fm *FirewallManager) AuditAndFixTsForwardOrder(ctx context.Context) error {
	rules := fm.cachedFilterRules()
	if rules == "" {
		return nil
	}
	res := auditTsForwardOrder(rules)
	if !res.Misplaced() {
		return nil
	}
	slog.Warn("ts-forward chain order regressed; restoring after UBIOS_FORWARD_JUMP",
		"tsForwardPos", res.TSForwardPos, "ubiosPos", res.UBIOSPos)
	if err := exec.CommandContext(ctx, "iptables", "-w", "2", "-t", "filter", "-D", "FORWARD", "-j", "ts-forward").Run(); err != nil {
		return fmt.Errorf("delete misplaced ts-forward: %w", err)
	}
	// After deletion the rule above UBIOS shifts up by one; the new slot
	// directly after UBIOS_FORWARD_JUMP is at position UBIOSPos (1-based).
	insertAt := res.UBIOSPos
	if err := exec.CommandContext(ctx, "iptables", "-w", "2", "-t", "filter", "-I", "FORWARD", strconv.Itoa(insertAt), "-j", "ts-forward").Run(); err != nil {
		return fmt.Errorf("reinsert ts-forward at %d: %w", insertAt, err)
	}
	fm.invalidateFilterCache()
	return nil
}

func (fm *FirewallManager) invalidateFilterCache() {
	fm.filterMu.Lock()
	fm.filterCache = ""
	fm.filterMu.Unlock()
}

func (fm *FirewallManager) cachedFilterRules() string {
	fm.filterMu.Lock()
	if fm.filterCache != "" && time.Since(fm.filterTime) < time.Second {
		c := fm.filterCache
		fm.filterMu.Unlock()
		return c
	}
	fm.filterMu.Unlock()

	v, _, _ := fm.filterFlight.Do("iptables-save", func() (any, error) {
		out, err := exec.Command("iptables-save", "-t", "filter").Output()
		if err != nil {
			return "", err
		}
		result := string(out)

		fm.filterMu.Lock()
		fm.filterCache = result
		fm.filterTime = time.Now()
		fm.filterMu.Unlock()
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

func (fm *FirewallManager) hasChainRule(chain, match string) bool {
	if rules := fm.cachedFilterRules(); rules != "" {
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

func (fm *FirewallManager) hasIPSetEntry(setName, match string) bool {
	fm.ipsetMu.Lock()
	if fm.ipsetSet == setName && fm.ipsetCache != "" && time.Since(fm.ipsetTime) < time.Second {
		c := fm.ipsetCache
		fm.ipsetMu.Unlock()
		return strings.Contains(c, match)
	}
	fm.ipsetMu.Unlock()

	v, _, _ := fm.ipsetFlight.Do("ipset-"+setName, func() (any, error) {
		out, err := exec.Command("ipset", "list", setName).Output()
		if err != nil {
			return "", err
		}
		result := string(out)

		fm.ipsetMu.Lock()
		fm.ipsetCache = result
		fm.ipsetSet = setName
		fm.ipsetTime = time.Now()
		fm.ipsetMu.Unlock()
		return result, nil
	})
	return strings.Contains(v.(string), match)
}
