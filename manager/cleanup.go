package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/service"
	"unifi-tailscale/manager/udapi"
)

// cleanupManagerActiveCheck reports whether vpn-pack-manager.service is
// currently active. Overridable in tests; defaults to systemctl probe.
// Cleanup must run only with the manager service stopped — otherwise the
// daemon and the cleanup binary can both issue concurrent GET-modify-PUT
// cycles against UDAPI ipsets and lose updates (UDAPI exposes no
// versioning, so the intra-process RMW lock cannot extend cross-process).
//
// Fail-closed: only an *exec.ExitError (systemctl ran, said "not active")
// is treated as not-active. Any other error (binary missing, dbus
// unreachable, permission denied) is treated as could-not-verify, and
// the guard returns true so cleanup refuses rather than racing blind.
var cleanupManagerActiveCheck = func() bool {
	err := exec.Command("systemctl", "is-active", "--quiet", "vpn-pack-manager.service").Run()
	if err == nil {
		return true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return false
	}
	slog.Warn("cleanup: cannot determine manager service state; assuming active for safety", "err", err)
	return true
}

// errCleanupRefused is returned by runCleanup when it refuses to run
// because the manager service is still active.
var errCleanupRefused = errors.New("vpn-pack-manager.service is active; stop it first (systemctl stop vpn-pack-manager) before running --cleanup")

func runCleanup() error {
	slog.Info("cleanup: removing UDAPI firewall rules and WG S2S interfaces")

	if cleanupManagerActiveCheck() {
		slog.Error("cleanup refused: manager service active", "err", errCleanupRefused)
		return errCleanupRefused
	}

	uc := udapi.NewClient(config.UDAPISocketPath)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	removeTailscaleUDAPIRules(ctx, uc)
	removeWgS2sUDAPIRules(ctx, uc)
	removeWgS2sInterfaces()
	removeSubnetsEntries(ctx, uc)
	removeExitNodeRules()

	removeIntegrationResources()

	if hasDPIFingerprint() {
		if err := setDPIFingerprint(true); err != nil {
			slog.Warn("cleanup: DPI fingerprint restore failed", "err", err)
		}
	}

	slog.Info("cleanup: done")
	return nil
}

func removeTailscaleUDAPIRules(ctx context.Context, uc *udapi.UDAPIClient) {
	marker := config.FirewallMarker
	if err := udapi.RemoveInterfaceRules(ctx, uc, config.TailscaleInterface, marker); err != nil {
		slog.Warn("cleanup: tailscale UDAPI rules removal failed", "err", err)
	} else {
		slog.Info("cleanup: tailscale UDAPI rules removed")
	}
}

func removeWgS2sUDAPIRules(ctx context.Context, uc *udapi.UDAPIClient) {
	ifaces, err := listWgS2sInterfaces()
	if err != nil {
		slog.Warn("cleanup: could not list wg-s2s interfaces", "err", err)
		return
	}
	for _, iface := range ifaces {
		marker := wgS2sMarkerPrefix + iface
		if err := udapi.RemoveInterfaceRules(ctx, uc, iface, marker); err != nil {
			slog.Warn("cleanup: wg-s2s UDAPI rules removal failed", "iface", iface, "err", err)
		} else {
			slog.Info("cleanup: wg-s2s UDAPI rules removed", "iface", iface)
		}
	}
}

func removeWgS2sInterfaces() {
	ifaces, err := listWgS2sInterfaces()
	if err != nil {
		return
	}
	for _, iface := range ifaces {
		if err := exec.Command("ip", "link", "delete", iface).Run(); err != nil {
			slog.Warn("cleanup: wg-s2s interface removal failed", "iface", iface, "err", err)
		} else {
			slog.Info("cleanup: wg-s2s interface removed", "iface", iface)
		}
	}
}

func removeSubnetsEntries(ctx context.Context, uc *udapi.UDAPIClient) {
	manifest, err := LoadManifest(config.ManifestPath)
	if err == nil && manifest.Tailscale.ChainPrefix != "" && manifest.Tailscale.ChainPrefix != config.DefaultChainPrefix {
		ipsetName := zoneIPSetName(manifest.Tailscale.ChainPrefix)
		if err := udapi.RemoveZoneSubnet(ctx, uc, ipsetName, config.TailscaleCGNAT); err != nil {
			slog.Warn("cleanup: zone ipset entry removal failed", "ipset", ipsetName, "err", err)
		} else {
			slog.Info("cleanup: zone ipset entry removed", "ipset", ipsetName, "cidr", config.TailscaleCGNAT)
		}
	}

	if err := udapi.RemoveVPNSubnet(ctx, uc, config.TailscaleCGNAT); err != nil {
		slog.Warn("cleanup: VPN_subnets entry removal failed", "err", err)
	} else {
		slog.Info("cleanup: VPN_subnets entry removed", "cidr", config.TailscaleCGNAT)
	}
}

func listWgS2sInterfaces() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("list interfaces: %w", err)
	}
	var result []string
	for _, iface := range ifaces {
		if strings.HasPrefix(iface.Name, "wg-s2s") {
			result = append(result, iface.Name)
		}
	}
	return result, nil
}

// buildIntegrationAPIHook and loadAPIKeyHook are overridden in tests.
var buildIntegrationAPIHook = buildIntegrationAPI
var loadAPIKeyHook = service.LoadAPIKey

// vpnPackResourceNamePrefix is the common Name prefix every zone and
// policy this manager creates carries (see service/firewall.go and
// client/integration.go). The discovery fallback uses it to identify
// our resources when the local manifest is missing or corrupt.
const vpnPackResourceNamePrefix = "VPN Pack: "

func removeIntegrationResources() {
	apiKey := loadAPIKeyHook()
	if apiKey == "" {
		slog.Warn("cleanup: no Integration API key, skipping zone/policy cleanup")
		return
	}

	ic := buildIntegrationAPIHook(apiKey)
	if !ic.HasAPIKey() {
		slog.Warn("cleanup: integration disabled (SPKI pin missing); skipping zone/policy cleanup")
		return
	}

	manifest, err := LoadManifest(config.ManifestPath)
	if err != nil {
		slog.Warn("cleanup: manifest unreadable; falling back to API discovery", "err", err)
		removeIntegrationResourcesByDiscovery(ic)
		return
	}

	siteID := manifest.SiteID
	if siteID == "" {
		slog.Warn("cleanup: no site ID in manifest; falling back to API discovery")
		removeIntegrationResourcesByDiscovery(ic)
		return
	}
	slog.Info("cleanup: removing Integration API zones and policies", "siteId", siteID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for marker, entry := range manifest.WanPorts {
		deleteResourceBestEffort("WAN port policy", marker, func() error {
			return ic.DeletePolicy(ctx, siteID, entry.PolicyID)
		})
	}

	for tunnelID, zm := range manifest.WgS2s {
		for _, policyID := range zm.PolicyIDs {
			deleteResourceBestEffort("wg-s2s policy", tunnelID+"/"+policyID, func() error {
				return ic.DeletePolicy(ctx, siteID, policyID)
			})
		}
	}

	for _, policyID := range manifest.Tailscale.PolicyIDs {
		deleteResourceBestEffort("tailscale policy", policyID, func() error {
			return ic.DeletePolicy(ctx, siteID, policyID)
		})
	}

	for marker, entry := range manifest.DNSPolicies {
		deleteResourceBestEffort("DNS forwarding policy", marker, func() error {
			return ic.DeletePolicy(ctx, siteID, entry.PolicyID)
		})
	}

	deletedZones := make(map[string]bool)
	for _, zm := range manifest.WgS2s {
		if zm.ZoneID == "" || deletedZones[zm.ZoneID] {
			continue
		}
		deletedZones[zm.ZoneID] = true
		deleteResourceBestEffort("wg-s2s zone", zm.ZoneID, func() error {
			return ic.DeleteZone(ctx, siteID, zm.ZoneID)
		})
	}

	if manifest.Tailscale.ZoneID != "" {
		deleteResourceBestEffort("tailscale zone", manifest.Tailscale.ZoneID, func() error {
			return ic.DeleteZone(ctx, siteID, manifest.Tailscale.ZoneID)
		})
	}

	slog.Info("cleanup: Integration API cleanup complete")
}

// removeIntegrationResourcesByDiscovery is the BUG-L17 fallback. With no
// manifest to consult, we ask the API which zones and policies exist and
// delete everything whose Name carries our "VPN Pack: " prefix. Policies
// must go before zones (FK constraint on the Integration side).
func removeIntegrationResourcesByDiscovery(ic IntegrationAPI) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	siteID, err := ic.DiscoverSiteID(ctx)
	if err != nil || siteID == "" {
		slog.Warn("cleanup: discovery fallback aborted (no siteID)", "err", err)
		return
	}
	slog.Info("cleanup: discovery fallback active", "siteId", siteID)

	policies, err := ic.ListPolicies(ctx, siteID)
	if err != nil {
		slog.Warn("cleanup: discovery fallback could not list policies", "err", err)
	} else {
		for _, p := range policies {
			if !strings.HasPrefix(p.Name, vpnPackResourceNamePrefix) {
				continue
			}
			if p.Metadata != nil && p.Metadata.Origin == domain.PolicyOriginDerived {
				continue
			}
			pid := p.ID
			deleteResourceBestEffort("policy (discovered)", p.Name, func() error {
				return ic.DeletePolicy(ctx, siteID, pid)
			})
		}
	}

	zones, err := ic.ListZones(ctx, siteID)
	if err != nil {
		slog.Warn("cleanup: discovery fallback could not list zones", "err", err)
		return
	}
	for _, z := range zones {
		if !strings.HasPrefix(z.Name, vpnPackResourceNamePrefix) {
			continue
		}
		zid := z.ID
		deleteResourceBestEffort("zone (discovered)", z.Name, func() error {
			return ic.DeleteZone(ctx, siteID, zid)
		})
	}

	slog.Info("cleanup: discovery fallback complete")
}

func removeExitNodeRules() {
	for _, fam := range []string{"-4", "-6"} {
		out, err := exec.Command("ip", fam, "rule", "show").Output()
		if err != nil {
			slog.Warn("cleanup: ip rule show failed", "family", fam, "err", err)
			continue
		}
		lookupStr := fmt.Sprintf("lookup %d", service.ExitRouteTable)
		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.Contains(line, lookupStr) {
				continue
			}
			idx := strings.IndexByte(line, ':')
			if idx <= 0 {
				continue
			}
			prio, err := strconv.Atoi(strings.TrimSpace(line[:idx]))
			if err != nil || prio < service.ExitRuleBasePrio || prio > service.ExitRuleMaxPrio {
				continue
			}
			if delErr := exec.Command("ip", fam, "rule", "del", "prio", strconv.Itoa(prio)).Run(); delErr != nil {
				slog.Warn("cleanup: exit node ip rule removal failed", "family", fam, "prio", prio, "err", delErr)
			} else {
				slog.Info("cleanup: exit node ip rule removed", "family", fam, "prio", prio)
			}
		}
	}

	masqRules := []struct {
		cmd string
		src string
	}{
		{"iptables", config.TailscaleCGNAT},
		{"ip6tables", "fd7a:115c:a1e0::/48"},
	}
	for _, r := range masqRules {
		if err := exec.Command(r.cmd, "-t", "nat", "-D", "POSTROUTING",
			"-o", config.TailscaleInterface, "!", "-s", r.src,
			"-j", "MASQUERADE", "-m", "comment", "--comment", service.ExitMasqComment).Run(); err == nil {
			slog.Info("cleanup: exit node masquerade rule removed", "cmd", r.cmd)
		}
	}
}

func deleteResourceBestEffort(kind, id string, fn func() error) {
	if err := fn(); err != nil {
		if errors.Is(err, ErrNotFound) {
			slog.Info("cleanup: already removed", "kind", kind, "id", id)
		} else {
			slog.Warn("cleanup: removal failed", "kind", kind, "id", id, "err", err)
		}
	} else {
		slog.Info("cleanup: removed", "kind", kind, "id", id)
	}
}
