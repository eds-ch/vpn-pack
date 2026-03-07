package main

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"unifi-tailscale/manager/config"
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

func (a integrationICAdapter) SetAPIKey(key string) { a.ic.SetAPIKey(key) }
func (a integrationICAdapter) HasAPIKey() bool      { return a.ic.HasAPIKey() }
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

func (a *firewallManifestAdapter) GetSiteID() string { return a.ms.GetSiteID() }
func (a *firewallManifestAdapter) HasSiteID() bool   { return a.ms.HasSiteID() }
func (a *firewallManifestAdapter) GetTailscaleChainPrefix() string {
	return a.ms.GetTailscaleChainPrefix()
}
func toZoneManifestData(z ZoneManifest) service.ZoneManifestData {
	return service.ZoneManifestData{
		ZoneID:      z.ZoneID,
		ZoneName:    z.ZoneName,
		PolicyIDs:   z.PolicyIDs,
		ChainPrefix: z.ChainPrefix,
	}
}

func (a *firewallManifestAdapter) GetTailscaleZone() service.ZoneManifestData {
	return toZoneManifestData(a.ms.GetTailscaleZone())
}
func (a *firewallManifestAdapter) SetTailscaleZone(zoneID, zoneName string, policyIDs []string, chainPrefix string) error {
	return a.ms.SetTailscaleZone(zoneID, zoneName, policyIDs, chainPrefix)
}
func (a *firewallManifestAdapter) GetWgS2sSnapshot() map[string]service.ZoneManifestData {
	raw := a.ms.GetWgS2sSnapshot()
	out := make(map[string]service.ZoneManifestData, len(raw))
	for k, v := range raw {
		out[k] = toZoneManifestData(v)
	}
	return out
}
func (a *firewallManifestAdapter) GetWgS2sZone(tunnelID string) (service.ZoneManifestData, bool) {
	zm, ok := a.ms.GetWgS2sZone(tunnelID)
	if !ok {
		return service.ZoneManifestData{}, false
	}
	return toZoneManifestData(zm), true
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

func (a *firewallOpsAdapter) DiscoverChainPrefix(ctx context.Context, zoneID string) string {
	return a.fw.DiscoverChainPrefix(ctx, zoneID)
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
	if err := a.fw.OpenWanPort(ctx, port, config.WanMarkerWgS2sPrefix+iface); err != nil {
		slog.Warn("wg-s2s WAN port open failed", "port", port, "err", err)
	} else {
		a.fw.RestoreRulesWithRetry(context.WithoutCancel(ctx), 3, 2*time.Second)
	}
}

func (a *wgS2sFirewallAdapter) CloseWanPort(ctx context.Context, port int, iface string) {
	if port <= 0 {
		return
	}
	if err := a.fw.CloseWanPort(ctx, port, config.WanMarkerWgS2sPrefix+iface); err != nil {
		slog.Warn("wg-s2s WAN port close failed", "port", port, "err", err)
	} else {
		a.fw.RestoreRulesWithRetry(context.WithoutCancel(ctx), 3, 2*time.Second)
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

// --- Notification adapters ---

type settingsNotifierAdapter struct {
	restart   func()
	state     *TailscaleState
	broadcast func()
}

func (a *settingsNotifierAdapter) OnRestartRequired() {
	a.restart()
}

func (a *settingsNotifierAdapter) OnDNSChanged(enabled bool) {
	a.state.Update(func(d *stateData) {
		d.AcceptDNS = enabled
	})
	a.broadcast()
}

type integrationNotifierAdapter struct {
	fw          FirewallService
	fwOrch      *service.FirewallOrchestrator
	health      *HealthTracker
	state       *TailscaleState
	broadcast   func()
	openWanPort func(context.Context)
}

func (a *integrationNotifierAdapter) OnBeforeKeyDelete(ctx context.Context) {
	if a.fw != nil {
		if err := a.fw.RemoveDNSForwarding(ctx); err != nil {
			slog.Warn("DNS forwarding cleanup failed during key removal", "err", err)
		}
	}
}

func (a *integrationNotifierAdapter) OnKeyConfigured(ctx context.Context, st *service.IntegrationStatus) {
	if a.fwOrch != nil && st.SiteID != "" {
		if result := a.fwOrch.SetupTailscaleFirewall(ctx); result.Err() != nil {
			slog.Warn("firewall setup after key save failed", "err", result.Err())
		}
		a.openWanPort(ctx)
	}
	a.health.ClearDegraded("firewall")
	a.health.RecordSuccess("firewall")
	a.state.Update(func(d *stateData) {
		d.IntegrationStatus = st
	})
	a.broadcast()
}

func (a *integrationNotifierAdapter) OnKeyDeleted() {
	a.state.Update(func(d *stateData) {
		d.IntegrationStatus = &service.IntegrationStatus{Configured: false}
	})
	a.broadcast()
}

func localSubnetProvider() []service.SubnetEntry {
	raw := parseLocalSubnets()
	out := make([]service.SubnetEntry, len(raw))
	for i, s := range raw {
		out[i] = service.SubnetEntry{CIDR: s.CIDR, Name: s.Name, Type: s.Type}
	}
	return out
}

func subnetValidatorProvider(allowedIPs []string, excludeIfaces ...string) ([]service.SubnetConflict, []service.SubnetConflict) {
	sys, err := service.CollectSystemSubnets(excludeIfaces...)
	if err != nil {
		slog.Warn("subnet collection failed, skipping validation", "err", err)
		return nil, nil
	}
	vr := service.ValidateAllowedIPs(allowedIPs, sys)
	return vr.Warnings, vr.Blocked
}

// fileKeyStore implements service.KeyStore via package-level file I/O functions.
type fileKeyStore struct{}

func (fileKeyStore) Save(key string) error { return service.SaveAPIKey(key) }
func (fileKeyStore) Delete() error         { return service.DeleteAPIKey() }
