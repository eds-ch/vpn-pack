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

func runCleanup() {
	slog.Info("cleanup: removing UDAPI firewall rules and WG S2S interfaces")

	uc := udapi.NewClient(udapiSocketPath)

	removeTailscaleUDAPIRules(uc)
	removeWgS2sUDAPIRules(uc)
	removeWgS2sInterfaces()
	removeVPNSubnetsEntry(uc)

	removeIntegrationResources()

	if hasDPIFingerprint() {
		if err := setDPIFingerprint(true); err != nil {
			slog.Warn("cleanup: DPI fingerprint restore failed", "err", err)
		}
	}

	slog.Info("cleanup: done")
}

func removeTailscaleUDAPIRules(uc *udapi.UDAPIClient) {
	marker := firewallMarker
	if err := udapi.RemoveInterfaceRules(uc, "tailscale0", marker); err != nil {
		slog.Warn("cleanup: tailscale UDAPI rules removal failed", "err", err)
	} else {
		slog.Info("cleanup: tailscale UDAPI rules removed")
	}
}

func removeWgS2sUDAPIRules(uc *udapi.UDAPIClient) {
	ifaces, err := listWgS2sInterfaces()
	if err != nil {
		slog.Warn("cleanup: could not list wg-s2s interfaces", "err", err)
		return
	}
	for _, iface := range ifaces {
		marker := "wg-s2s-manager:" + iface
		if err := udapi.RemoveInterfaceRules(uc, iface, marker); err != nil {
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

func removeVPNSubnetsEntry(uc *udapi.UDAPIClient) {
	if err := udapi.RemoveVPNSubnet(uc, tailscaleCGNAT); err != nil {
		slog.Warn("cleanup: VPN_subnets entry removal failed", "err", err)
	} else {
		slog.Info("cleanup: VPN_subnets entry removed", "cidr", tailscaleCGNAT)
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

func removeIntegrationResources() {
	apiKey := loadAPIKey()
	if apiKey == "" {
		slog.Warn("cleanup: no Integration API key, skipping zone/policy cleanup")
		return
	}

	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		slog.Warn("cleanup: cannot load manifest, skipping zone/policy cleanup", "err", err)
		return
	}

	siteID := manifest.SiteID
	if siteID == "" {
		slog.Warn("cleanup: no site ID in manifest, skipping zone/policy cleanup")
		return
	}

	ic := NewIntegrationClient(apiKey)
	slog.Info("cleanup: removing Integration API zones and policies", "siteId", siteID)

	for marker, entry := range manifest.WanPorts {
		deleteResourceBestEffort("WAN port policy", marker, func() error {
			return ic.DeletePolicy(siteID, entry.PolicyID)
		})
	}

	for tunnelID, zm := range manifest.WgS2s {
		for _, policyID := range zm.PolicyIDs {
			deleteResourceBestEffort("wg-s2s policy", tunnelID+"/"+policyID, func() error {
				return ic.DeletePolicy(siteID, policyID)
			})
		}
	}

	for _, policyID := range manifest.Tailscale.PolicyIDs {
		deleteResourceBestEffort("tailscale policy", policyID, func() error {
			return ic.DeletePolicy(siteID, policyID)
		})
	}

	deletedZones := make(map[string]bool)
	for _, zm := range manifest.WgS2s {
		if zm.ZoneID == "" || deletedZones[zm.ZoneID] {
			continue
		}
		deletedZones[zm.ZoneID] = true
		deleteResourceBestEffort("wg-s2s zone", zm.ZoneID, func() error {
			return ic.DeleteZone(siteID, zm.ZoneID)
		})
	}

	if manifest.Tailscale.ZoneID != "" {
		deleteResourceBestEffort("tailscale zone", manifest.Tailscale.ZoneID, func() error {
			return ic.DeleteZone(siteID, manifest.Tailscale.ZoneID)
		})
	}

	slog.Info("cleanup: Integration API cleanup complete")
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
