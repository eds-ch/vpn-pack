package main

import (
	"errors"
	"fmt"
	"log/slog"
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
	if fm.ic == nil || !fm.ic.HasAPIKey() || fm.manifest == nil || fm.manifest.SiteID == "" {
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

	if fm.ic != nil && fm.ic.HasAPIKey() && fm.manifest != nil && fm.manifest.SiteID != "" {
		siteID := fm.manifest.SiteID
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

			fm.manifest.SetTailscaleZone(zone.ID, policyIDs, chainPrefix)
			if err := fm.manifest.Save(); err != nil {
				slog.Warn("manifest save failed", "err", err)
			}
		}
	}

	marker := firewallMarker

	oldPrefix := fm.manifest.GetTailscaleChainPrefix()
	if chainPrefix != oldPrefix {
		_ = udapi.RemoveInterfaceRules(fm.udapi, "tailscale0", marker)
	}

	if chainPrefix != defaultChainPrefix {
		fwd := hasChainRule(chainForwardInUser, "-i tailscale0")
		inp := hasChainRule(chainInputUserHook, "-i tailscale0")
		out := hasChainRule(chainOutputUserHook, "-o tailscale0")
		if fwd && inp && out {
			slog.Info("tailscale firewall setup complete", "chainPrefix", chainPrefix, "source", "ubios")
			return nil
		}
	}

	if err := udapi.AddInterfaceRulesForZone(fm.udapi, "tailscale0", marker, chainPrefix); err != nil {
		slog.Warn("UDAPI tailscale rules failed", "err", err, "prefix", chainPrefix)
		return err
	}

	if chainPrefix == defaultChainPrefix {
		slog.Info("ensuring VPN_subnets ipset entry", "cidr", tailscaleCGNAT)
		if err := udapi.EnsureVPNSubnet(fm.udapi, tailscaleCGNAT); err != nil {
			slog.Warn("VPN_subnets ipset failed", "err", err)
		}
	}

	slog.Info("tailscale firewall setup complete", "chainPrefix", chainPrefix)
	return nil
}

func (fm *FirewallManager) SetupWgS2sZone(tunnelID, zoneID, zoneName string) error {
	if err := fm.requireIntegration(); err != nil {
		return err
	}
	siteID := fm.manifest.SiteID

	if zoneID != "" && zoneID != "new" {
		for _, zm := range fm.manifest.WgS2s {
			if zm.ZoneID == zoneID {
				fm.manifest.SetWgS2sZone(tunnelID, zm.ZoneID, zm.ZoneName, zm.PolicyIDs, zm.ChainPrefix)
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

	fm.manifest.SetWgS2sZone(tunnelID, zone.ID, zone.Name, policyIDs, chainPrefix)
	if err := fm.manifest.Save(); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}
	return nil
}

func (fm *FirewallManager) SetupWgS2sFirewall(tunnelID, iface string) error {
	chainPrefix := fm.manifest.GetWgS2sChainPrefix(tunnelID)

	if chainPrefix == defaultChainPrefix {
		if zm, ok := fm.manifest.WgS2s[tunnelID]; ok && zm.ZoneID != "" {
			if rediscovered := fm.discoverChainPrefix(zm.ZoneID); rediscovered != "" {
				chainPrefix = rediscovered
				fm.manifest.SetWgS2sZone(tunnelID, zm.ZoneID, zm.ZoneName, zm.PolicyIDs, chainPrefix)
				if err := fm.manifest.Save(); err != nil {
					slog.Warn("manifest save failed", "err", err)
				}
			}
		}
	}

	marker := "wg-s2s-manager:" + iface
	return udapi.AddInterfaceRulesForZone(fm.udapi, iface, marker, chainPrefix)
}

func (fm *FirewallManager) RemoveWgS2sFirewall(iface string) {
	marker := "wg-s2s-manager:" + iface
	if err := udapi.RemoveInterfaceRules(fm.udapi, iface, marker); err != nil {
		slog.Warn("wg-s2s firewall rule removal failed", "iface", iface, "err", err)
	}
}

func (fm *FirewallManager) OpenWanPort(port int, marker string) error {
	if err := fm.requireIntegration(); err != nil {
		return err
	}

	if existing := fm.manifest.GetWanPortPolicyID(marker); existing != "" {
		return nil
	}

	siteID := fm.manifest.SiteID
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

	siteID := fm.manifest.SiteID
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
	if fm.manifest.ExternalZoneID != "" && fm.manifest.GatewayZoneID != "" {
		return fm.manifest.ExternalZoneID, fm.manifest.GatewayZoneID, nil
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

	if chainPrefix == defaultChainPrefix && fm.manifest.Tailscale.ZoneID != "" {
		if rediscovered := fm.discoverChainPrefix(fm.manifest.Tailscale.ZoneID); rediscovered != "" {
			_ = udapi.RemoveInterfaceRules(fm.udapi, "tailscale0", marker)
			chainPrefix = rediscovered
			fm.manifest.SetTailscaleZone(fm.manifest.Tailscale.ZoneID, fm.manifest.Tailscale.PolicyIDs, rediscovered)
			if err := fm.manifest.Save(); err != nil {
				slog.Warn("manifest save failed", "err", err)
			}
			slog.Info("tailscale chain prefix re-discovered", "prefix", rediscovered)
		}
	}

	if chainPrefix != defaultChainPrefix {
		fwd := hasChainRule(chainForwardInUser, "-i tailscale0")
		inp := hasChainRule(chainInputUserHook, "-i tailscale0")
		out := hasChainRule(chainOutputUserHook, "-o tailscale0")
		if fwd && inp && out {
			return nil
		}
	}

	if err := udapi.AddInterfaceRulesForZone(fm.udapi, "tailscale0", marker, chainPrefix); err != nil {
		return err
	}

	if chainPrefix == defaultChainPrefix {
		if err := udapi.EnsureVPNSubnet(fm.udapi, tailscaleCGNAT); err != nil {
			slog.Warn("VPN_subnets ipset failed", "err", err)
		}
	}

	return nil
}

func (fm *FirewallManager) CheckTailscaleRulesPresent() (forward, input, output, ipset bool) {
	prefix := fm.manifest.GetTailscaleChainPrefix()
	forward = hasChainRule(chainForwardInUser, "-i tailscale0") ||
		hasChainRule(fmt.Sprintf("UBIOS_%s_IN", prefix), "-i tailscale0")
	input = hasChainRule(chainInputUserHook, "-i tailscale0") ||
		hasChainRule(fmt.Sprintf("UBIOS_%s_LOCAL", prefix), "-i tailscale0")
	output = hasChainRule(chainOutputUserHook, "-o tailscale0") ||
		hasChainRule(fmt.Sprintf("UBIOS_LOCAL_%s", prefix), "-o tailscale0")

	if prefix == defaultChainPrefix {
		ipset = hasIPSetEntry("UBIOS4VPN_subnets", tailscaleCGNAT)
	} else {
		ipset = true
	}
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
