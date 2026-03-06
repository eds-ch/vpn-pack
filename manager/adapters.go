package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"unifi-tailscale/manager/service"
)

type settingsManifestAdapter struct {
	ms ManifestStore
}

func (a settingsManifestAdapter) HasDNSPolicy(marker string) bool {
	return a.ms.HasDNSPolicy(marker)
}

func (a settingsManifestAdapter) WanPort(marker string) (int, bool) {
	entry, ok := a.ms.GetWanPortEntry(marker)
	return entry.Port, ok
}

type integrationICAdapter struct {
	ic IntegrationAPI
}

func (a integrationICAdapter) SetAPIKey(key string)    { a.ic.SetAPIKey(key) }
func (a integrationICAdapter) HasAPIKey() bool         { return a.ic.HasAPIKey() }
func (a integrationICAdapter) DiscoverSiteID(ctx context.Context) (string, error) {
	return a.ic.DiscoverSiteID(ctx)
}
func (a integrationICAdapter) FindSystemZoneIDs(ctx context.Context, siteID string) (string, string, error) {
	return a.ic.FindSystemZoneIDs(ctx, siteID)
}
func (a integrationICAdapter) Validate(ctx context.Context) (string, error) {
	info, err := a.ic.Validate(ctx)
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			return "", service.ErrUnauthorized
		}
		return "", err
	}
	return info.ApplicationVersion, nil
}

// --- Adapters for FirewallOrchestrator ---

type firewallIntegrationAdapter struct {
	ic IntegrationAPI
}

func (a *firewallIntegrationAdapter) HasAPIKey() bool { return a.ic.HasAPIKey() }
func (a *firewallIntegrationAdapter) EnsureZone(ctx context.Context, siteID, name string) (service.ZoneInfo, error) {
	z, err := a.ic.EnsureZone(ctx, siteID, name)
	if err != nil {
		return service.ZoneInfo{}, err
	}
	return service.ZoneInfo{ZoneID: z.ID, ZoneName: z.Name}, nil
}
func (a *firewallIntegrationAdapter) EnsurePolicies(ctx context.Context, siteID, name, zoneID string) ([]string, error) {
	return a.ic.EnsurePolicies(ctx, siteID, name, zoneID)
}
func (a *firewallIntegrationAdapter) DeletePolicy(ctx context.Context, siteID, policyID string) error {
	err := a.ic.DeletePolicy(ctx, siteID, policyID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
func (a *firewallIntegrationAdapter) DeleteZone(ctx context.Context, siteID, zoneID string) error {
	err := a.ic.DeleteZone(ctx, siteID, zoneID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}

type firewallManifestAdapter struct {
	ms ManifestStore
}

func (a *firewallManifestAdapter) GetSiteID() string  { return a.ms.GetSiteID() }
func (a *firewallManifestAdapter) HasSiteID() bool     { return a.ms.HasSiteID() }
func (a *firewallManifestAdapter) GetTailscaleChainPrefix() string {
	return a.ms.GetTailscaleChainPrefix()
}
func (a *firewallManifestAdapter) GetTailscaleZone() service.ZoneManifestData {
	z := a.ms.GetTailscaleZone()
	return service.ZoneManifestData{ZoneID: z.ZoneID, ZoneName: z.ZoneName, PolicyIDs: z.PolicyIDs, ChainPrefix: z.ChainPrefix}
}
func (a *firewallManifestAdapter) SetTailscaleZone(zoneID, zoneName string, policyIDs []string, chainPrefix string) error {
	return a.ms.SetTailscaleZone(zoneID, zoneName, policyIDs, chainPrefix)
}
func (a *firewallManifestAdapter) GetWgS2sSnapshot() map[string]service.ZoneManifestData {
	raw := a.ms.GetWgS2sSnapshot()
	out := make(map[string]service.ZoneManifestData, len(raw))
	for k, v := range raw {
		out[k] = service.ZoneManifestData{ZoneID: v.ZoneID, ZoneName: v.ZoneName, PolicyIDs: v.PolicyIDs, ChainPrefix: v.ChainPrefix}
	}
	return out
}
func (a *firewallManifestAdapter) GetWgS2sZone(tunnelID string) (service.ZoneManifestData, bool) {
	zm, ok := a.ms.GetWgS2sZone(tunnelID)
	if !ok {
		return service.ZoneManifestData{}, false
	}
	return service.ZoneManifestData{ZoneID: zm.ZoneID, ZoneName: zm.ZoneName, PolicyIDs: zm.PolicyIDs, ChainPrefix: zm.ChainPrefix}, true
}
func (a *firewallManifestAdapter) SetWgS2sZone(tunnelID string, zs service.ZoneManifestData) error {
	return a.ms.SetWgS2sZone(tunnelID, ZoneManifest{ZoneID: zs.ZoneID, ZoneName: zs.ZoneName, PolicyIDs: zs.PolicyIDs, ChainPrefix: zs.ChainPrefix})
}
func (a *firewallManifestAdapter) RemoveWgS2sTunnel(tunnelID string) error {
	return a.ms.RemoveWgS2sTunnel(tunnelID)
}

type firewallOpsAdapter struct {
	fw FirewallService
}

func (a *firewallOpsAdapter) DiscoverChainPrefix(zoneID string) string {
	return a.fw.DiscoverChainPrefix(zoneID)
}
func (a *firewallOpsAdapter) EnsureTailscaleRules(chainPrefix string) error {
	return a.fw.EnsureTailscaleRules(chainPrefix)
}
func (a *firewallOpsAdapter) RemoveTailscaleInterfaceRules() error {
	return a.fw.RemoveTailscaleInterfaceRules()
}

// --- Adapter for WgS2sService ---

type wgS2sFirewallAdapter struct {
	fw   FirewallService
	orch *service.FirewallOrchestrator
}

func (a *wgS2sFirewallAdapter) SetupZone(ctx context.Context, tunnelID, zoneID, zoneName string) *service.ZoneSetupResult {
	r := a.orch.SetupWgS2sZone(ctx, tunnelID, zoneID, zoneName)
	if r == nil {
		return nil
	}
	return &service.ZoneSetupResult{
		ZoneCreated:   r.ZoneCreated,
		PoliciesReady: r.PoliciesReady,
		UDAPIApplied:  r.UDAPIApplied,
		Errors:        r.Errors,
	}
}

func (a *wgS2sFirewallAdapter) SetupFirewall(ctx context.Context, tunnelID, iface string, allowedIPs []string) error {
	return a.fw.SetupWgS2sFirewall(ctx, tunnelID, iface, allowedIPs)
}

func (a *wgS2sFirewallAdapter) RemoveFirewall(ctx context.Context, tunnelID, iface string, allowedIPs []string) {
	a.fw.RemoveWgS2sFirewall(ctx, tunnelID, iface, allowedIPs)
}

func (a *wgS2sFirewallAdapter) RemoveIPSetEntries(ctx context.Context, tunnelID string, cidrs []string) {
	a.fw.RemoveWgS2sIPSetEntries(ctx, tunnelID, cidrs)
}

func (a *wgS2sFirewallAdapter) TeardownZone(ctx context.Context, tunnelID string) {
	a.orch.TeardownWgS2sZone(ctx, tunnelID)
}

func (a *wgS2sFirewallAdapter) OpenWanPort(ctx context.Context, port int, iface string) {
	if err := a.fw.OpenWanPort(ctx, port, wanMarkerWgS2sPrefix+iface); err != nil {
		slog.Warn("wg-s2s WAN port open failed", "port", port, "err", err)
	} else {
		go a.fw.RestoreRulesWithRetry(context.WithoutCancel(ctx), 3, 2*time.Second)
	}
}

func (a *wgS2sFirewallAdapter) CloseWanPort(ctx context.Context, port int, iface string) {
	if port <= 0 {
		return
	}
	if err := a.fw.CloseWanPort(ctx, port, wanMarkerWgS2sPrefix+iface); err != nil {
		slog.Warn("wg-s2s WAN port close failed", "port", port, "err", err)
	} else {
		go a.fw.RestoreRulesWithRetry(context.WithoutCancel(ctx), 3, 2*time.Second)
	}
}

func (a *wgS2sFirewallAdapter) CheckRulesPresent(ctx context.Context, ifaces []string) map[string]bool {
	return a.fw.CheckWgS2sRulesPresent(ctx, ifaces)
}

func (a *wgS2sFirewallAdapter) IntegrationReady() bool {
	return a.fw.IntegrationReady()
}

type wgS2sManifestAdapter struct {
	ms ManifestStore
}

func (a *wgS2sManifestAdapter) GetZone(tunnelID string) (service.ZoneInfo, bool) {
	zm, ok := a.ms.GetWgS2sZone(tunnelID)
	if !ok {
		return service.ZoneInfo{}, false
	}
	return service.ZoneInfo{ZoneID: zm.ZoneID, ZoneName: zm.ZoneName}, true
}

func (a *wgS2sManifestAdapter) GetZones() []service.WgS2sZoneEntry {
	zones := a.ms.GetWgS2sZones()
	if zones == nil {
		return nil
	}
	out := make([]service.WgS2sZoneEntry, len(zones))
	for i, z := range zones {
		out[i] = service.WgS2sZoneEntry{ZoneID: z.ZoneID, ZoneName: z.ZoneName, TunnelCount: z.TunnelCount}
	}
	return out
}

type wgS2sLogAdapter struct {
	buf *LogBuffer
}

func (a *wgS2sLogAdapter) LogWarn(msg string) {
	a.buf.Add(newLogEntry("warn", msg, "wgs2s"))
}

func subnetValidatorProvider(allowedIPs []string, excludeIfaces ...string) ([]service.SubnetConflict, []service.SubnetConflict) {
	sys, err := CollectSystemSubnets(excludeIfaces...)
	if err != nil {
		slog.Warn("subnet collection failed, skipping validation", "err", err)
		return nil, nil
	}
	vr := ValidateAllowedIPs(allowedIPs, sys)
	warnings := make([]service.SubnetConflict, len(vr.Warnings))
	for i, w := range vr.Warnings {
		warnings[i] = service.SubnetConflict{CIDR: w.CIDR, ConflictsWith: w.ConflictsWith, Interface: w.Interface, Severity: w.Severity, Message: w.Message}
	}
	blocks := make([]service.SubnetConflict, len(vr.Blocked))
	for i, b := range vr.Blocked {
		blocks[i] = service.SubnetConflict{CIDR: b.CIDR, ConflictsWith: b.ConflictsWith, Interface: b.Interface, Severity: b.Severity, Message: b.Message}
	}
	return warnings, blocks
}
