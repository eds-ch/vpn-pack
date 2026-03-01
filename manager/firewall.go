package main

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strings"

	"unifi-tailscale/manager/udapi"
)

var errIntegrationNotConfigured = errors.New("integration API not configured")

type FirewallManager struct {
	udapi    *udapi.UDAPIClient
	ic       *IntegrationClient
	manifest *Manifest
}

func (fm *FirewallManager) requireIntegration() error {
	if fm.ic == nil || !fm.ic.HasAPIKey() || fm.manifest == nil || !fm.manifest.HasSiteID() {
		return errIntegrationNotConfigured
	}
	return nil
}

func NewFirewallManager(socketPath string, ic *IntegrationClient, manifest *Manifest) *FirewallManager {
	return &FirewallManager{
		udapi:    udapi.NewClient(socketPath),
		ic:       ic,
		manifest: manifest,
	}
}

func (fm *FirewallManager) SetupTailscaleFirewall() error {
	if err := fm.requireIntegration(); err != nil {
		slog.Info("skipping tailscale firewall setup: integration not configured")
		return nil
	}

	chainPrefix := defaultChainPrefix

	if fm.ic != nil && fm.ic.HasAPIKey() && fm.manifest != nil && fm.manifest.HasSiteID() {
		siteID := fm.manifest.GetSiteID()
		zone, err := fm.ic.EnsureZone(siteID, "VPN Pack: Tailscale")
		if err != nil {
			slog.Warn("Integration API zone setup failed, using UDAPI only", "err", err)
		} else {
			slog.Info("integration zone ready", "zoneId", zone.ID, "name", zone.Name)

			policyIDs, err := fm.ic.EnsurePolicies(siteID, "Tailscale", zone.ID)
			if err != nil {
				slog.Warn("integration policy setup had errors", "err", err)
			} else {
				slog.Info("integration policies ready", "count", len(policyIDs))
			}

			discovered := fm.discoverChainPrefix(zone.ID)
			if discovered != "" {
				chainPrefix = discovered
			}

			fm.manifest.SetTailscaleZone(zone.ID, zone.Name, policyIDs, chainPrefix)
			if err := fm.manifest.Save(); err != nil {
				slog.Warn("manifest save failed", "err", err)
			}
		}
	}

	marker := firewallMarker

	oldPrefix := fm.manifest.GetTailscaleChainPrefix()
	if chainPrefix != oldPrefix {
		_ = udapi.RemoveInterfaceRules(fm.udapi, tailscaleInterface, marker)
	}

	if err := fm.ensureTailscaleRules(chainPrefix); err != nil {
		return err
	}

	slog.Info("tailscale firewall setup complete", "chainPrefix", chainPrefix)
	return nil
}

func (fm *FirewallManager) SetupWgS2sZone(tunnelID, zoneID, zoneName string) error {
	if err := fm.requireIntegration(); err != nil {
		return err
	}
	siteID := fm.manifest.GetSiteID()

	if zoneID != "" && zoneID != "new" {
		for _, zm := range fm.manifest.GetWgS2sSnapshot() {
			if zm.ZoneID == zoneID {
				fm.manifest.SetWgS2sZone(tunnelID, zm)
				if err := fm.manifest.Save(); err != nil {
					return fmt.Errorf("save manifest: %w", err)
				}
				return nil
			}
		}
		return fmt.Errorf("zone %s not found in manifest", zoneID)
	}

	if zoneName == "" {
		zoneName = "WireGuard S2S"
	}
	zoneDisplayName := "VPN Pack: " + zoneName

	zone, err := fm.ic.EnsureZone(siteID, zoneDisplayName)
	if err != nil {
		return fmt.Errorf("ensure zone %q: %w", zoneDisplayName, err)
	}
	slog.Info("wg-s2s integration zone ready", "zoneId", zone.ID, "name", zone.Name)

	policyIDs, err := fm.ic.EnsurePolicies(siteID, zoneName, zone.ID)
	if err != nil {
		slog.Warn("wg-s2s integration policy setup had errors", "err", err)
	} else {
		slog.Info("wg-s2s integration policies ready", "count", len(policyIDs))
	}

	chainPrefix := fm.discoverChainPrefix(zone.ID)
	if chainPrefix == "" {
		chainPrefix = defaultChainPrefix
	}

	fm.manifest.SetWgS2sZone(tunnelID, ZoneManifest{ZoneID: zone.ID, ZoneName: zone.Name, PolicyIDs: policyIDs, ChainPrefix: chainPrefix})
	if err := fm.manifest.Save(); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}
	return nil
}

func (fm *FirewallManager) SetupWgS2sFirewall(tunnelID, iface string, allowedIPs []string) error {
	chainPrefix := fm.manifest.GetWgS2sChainPrefix(tunnelID)

	if chainPrefix == defaultChainPrefix {
		if zm, ok := fm.manifest.GetWgS2sZone(tunnelID); ok && zm.ZoneID != "" {
			if rediscovered := fm.discoverChainPrefix(zm.ZoneID); rediscovered != "" {
				chainPrefix = rediscovered
				fm.manifest.SetWgS2sZone(tunnelID, ZoneManifest{ZoneID: zm.ZoneID, ZoneName: zm.ZoneName, PolicyIDs: zm.PolicyIDs, ChainPrefix: chainPrefix})
				if err := fm.manifest.Save(); err != nil {
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
		sys, sysErr := CollectSystemSubnets(iface)
		for _, cidr := range allowedIPs {
			if sysErr == nil {
				if _, candidateNet, err := net.ParseCIDR(cidr); err == nil {
					blocked := false
					for _, ifSub := range sys.Interfaces {
						if _, ifNet, err := net.ParseCIDR(ifSub.CIDR); err == nil && subnetsOverlap(candidateNet, ifNet) {
							slog.Warn("skipping conflicting ipset entry", "cidr", cidr, "conflictsWith", ifSub.CIDR, "iface", ifSub.Interface)
							blocked = true
							break
						}
					}
					if blocked {
						continue
					}
				}
			}
			if err := udapi.EnsureZoneSubnet(fm.udapi, ipsetName, cidr); err != nil {
				slog.Warn("wg-s2s zone ipset failed", "ipset", ipsetName, "cidr", cidr, "err", err)
			}
		}
	}

	return nil
}

func (fm *FirewallManager) RemoveWgS2sFirewall(tunnelID, iface string, allowedIPs []string) {
	marker := "wg-s2s-manager:" + iface
	if err := udapi.RemoveInterfaceRules(fm.udapi, iface, marker); err != nil {
		slog.Warn("wg-s2s firewall rule removal failed", "iface", iface, "err", err)
	}
	fm.RemoveWgS2sIPSetEntries(tunnelID, allowedIPs)
}

func (fm *FirewallManager) RemoveWgS2sIPSetEntries(tunnelID string, cidrs []string) {
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

func (fm *FirewallManager) OpenWanPort(port int, marker string) error {
	if err := fm.requireIntegration(); err != nil {
		return err
	}

	if existing := fm.manifest.GetWanPortPolicyID(marker); existing != "" {
		return nil
	}

	siteID := fm.manifest.GetSiteID()
	extID, gwID, err := fm.resolveSystemZones(siteID)
	if err != nil {
		return fmt.Errorf("resolve system zones: %w", err)
	}

	name := wanPortPolicyName(port, marker)
	policyID, err := fm.ic.EnsureWanPortPolicy(siteID, port, name, extID, gwID)
	if err != nil {
		return fmt.Errorf("ensure WAN port policy: %w", err)
	}

	fm.manifest.SetWanPort(marker, policyID, name, port)
	if err := fm.manifest.Save(); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	slog.Info("WAN port policy created", "port", port, "marker", marker, "policyId", policyID)
	return nil
}

func (fm *FirewallManager) CloseWanPort(port int, marker string) error {
	if err := fm.requireIntegration(); err != nil {
		return err
	}

	policyID := fm.manifest.GetWanPortPolicyID(marker)
	if policyID == "" {
		return nil
	}

	siteID := fm.manifest.GetSiteID()
	if err := fm.ic.DeletePolicy(siteID, policyID); err != nil {
		if errors.Is(err, ErrNotFound) {
			slog.Info("WAN port policy already gone from API", "marker", marker, "policyId", policyID)
		} else {
			return fmt.Errorf("delete WAN port policy: %w", err)
		}
	}

	fm.manifest.RemoveWanPort(marker)
	if err := fm.manifest.Save(); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	slog.Info("WAN port policy deleted", "port", port, "marker", marker)
	return nil
}

func (fm *FirewallManager) resolveSystemZones(siteID string) (string, string, error) {
	if extID, gwID := fm.manifest.GetSystemZoneIDs(); extID != "" && gwID != "" {
		return extID, gwID, nil
	}

	extID, gwID, err := fm.ic.findSystemZoneIDs(siteID)
	if err != nil {
		return "", "", fmt.Errorf("find system zones: %w", err)
	}

	fm.manifest.SetSystemZoneIDs(extID, gwID)
	if err := fm.manifest.Save(); err != nil {
		return "", "", fmt.Errorf("save manifest: %w", err)
	}

	return extID, gwID, nil
}

func (fm *FirewallManager) RestoreTailscaleRules() error {
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
			fm.manifest.SetTailscaleZone(ts.ZoneID, ts.ZoneName, ts.PolicyIDs, rediscovered)
			if err := fm.manifest.Save(); err != nil {
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

func (fm *FirewallManager) CheckTailscaleRulesPresent() (forward, input, output, ipset bool) {
	prefix := fm.manifest.GetTailscaleChainPrefix()
	forward = hasChainRule(chainForwardInUser, "-i " + tailscaleInterface) ||
		hasChainRule(fmt.Sprintf("UBIOS_%s_IN", prefix), "-i " + tailscaleInterface)
	input = hasChainRule(chainInputUserHook, "-i " + tailscaleInterface) ||
		hasChainRule(fmt.Sprintf("UBIOS_%s_LOCAL", prefix), "-i " + tailscaleInterface)
	output = hasChainRule(chainOutputUserHook, "-o " + tailscaleInterface) ||
		hasChainRule(fmt.Sprintf("UBIOS_LOCAL_%s", prefix), "-o " + tailscaleInterface)

	ipset = hasIPSetEntry(fmt.Sprintf("UBIOS4%s_subnets", prefix), tailscaleCGNAT)
	return
}

func (fm *FirewallManager) CheckWgS2sRulesPresent(ifaces []string) map[string]bool {
	result := make(map[string]bool, len(ifaces))
	for _, iface := range ifaces {
		result[iface] = hasChainRule(chainForwardInUser, "-i "+iface)
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
	out, err := exec.Command("mongo", "--port", "27117", "--quiet", "--eval", script).Output()
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

func hasChainRule(chain, match string) bool {
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
