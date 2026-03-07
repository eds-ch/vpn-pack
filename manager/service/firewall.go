package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"unifi-tailscale/manager/config"
)

// --- Interfaces ---

type FirewallIntegration interface {
	HasAPIKey() bool
	EnsureZone(ctx context.Context, siteID, name string) (ZoneInfo, error)
	EnsurePolicies(ctx context.Context, siteID, name, zoneID string) ([]string, error)
	DeletePolicy(ctx context.Context, siteID, policyID string) error
	DeleteZone(ctx context.Context, siteID, zoneID string) error
}

type FirewallManifest interface {
	GetSiteID() string
	HasSiteID() bool
	GetTailscaleZone() ZoneManifestData
	GetTailscaleChainPrefix() string
	SetTailscaleZone(zoneID, zoneName string, policyIDs []string, chainPrefix string) error
	GetWgS2sSnapshot() map[string]ZoneManifestData
	GetWgS2sZone(tunnelID string) (ZoneManifestData, bool)
	SetWgS2sZone(tunnelID string, zs ZoneManifestData) error
	RemoveWgS2sTunnel(tunnelID string) error
}

type FirewallOps interface {
	DiscoverChainPrefix(zoneID string) string
	EnsureTailscaleRules(chainPrefix string) error
	RemoveTailscaleInterfaceRules() error
}

// --- Types ---

type ZoneManifestData struct {
	ZoneID      string
	ZoneName    string
	PolicyIDs   []string
	ChainPrefix string
}

type SetupResult struct {
	ZoneCreated   bool
	ZoneID        string
	ZoneName      string
	PoliciesReady bool
	PolicyIDs     []string
	UDAPIApplied  bool
	ChainPrefix   string
	Errors        []string
}

func (r *SetupResult) resetZone() {
	r.ZoneCreated = false
	r.ZoneID = ""
	r.ZoneName = ""
}

func (r *SetupResult) resetPolicies() {
	r.PoliciesReady = false
	r.PolicyIDs = nil
}

func (r *SetupResult) OK() bool {
	return r.ZoneCreated && r.PoliciesReady && r.UDAPIApplied && len(r.Errors) == 0
}

func (r *SetupResult) Degraded() bool {
	return r.ZoneCreated && r.PoliciesReady && !r.UDAPIApplied
}

func (r *SetupResult) Err() error {
	if len(r.Errors) == 0 {
		return nil
	}
	return fmt.Errorf("firewall setup: %s", strings.Join(r.Errors, "; "))
}

func (r *SetupResult) addError(step string, err error) {
	r.Errors = append(r.Errors, fmt.Sprintf("%s: %s", step, err))
}

// --- FirewallOrchestrator ---

// Duplicated in manager/firewall.go — different packages, can't share.
var errIntegrationNotConfigured = errors.New("integration API not configured")

type FirewallOrchestrator struct {
	ic       FirewallIntegration
	manifest FirewallManifest
	ops      FirewallOps
}

func NewFirewallOrchestrator(ic FirewallIntegration, manifest FirewallManifest, ops FirewallOps) *FirewallOrchestrator {
	return &FirewallOrchestrator{ic: ic, manifest: manifest, ops: ops}
}

func (o *FirewallOrchestrator) requireIntegration() error {
	if o.ic == nil || !o.ic.HasAPIKey() || o.manifest == nil || !o.manifest.HasSiteID() {
		return errIntegrationNotConfigured
	}
	return nil
}

func (o *FirewallOrchestrator) SetupTailscaleFirewall(ctx context.Context) *SetupResult {
	result := &SetupResult{ChainPrefix: config.DefaultChainPrefix}

	if err := o.requireIntegration(); err != nil {
		slog.Info("skipping tailscale firewall setup: integration not configured")
		return result
	}

	siteID := o.manifest.GetSiteID()

	zone, err := o.ic.EnsureZone(ctx, siteID, "VPN Pack: Tailscale")
	if err != nil {
		result.addError("zone", err)
		return result
	}
	result.ZoneCreated = true
	result.ZoneID = zone.ZoneID
	result.ZoneName = zone.ZoneName
	slog.Info("integration zone ready", "zoneId", zone.ZoneID, "name", zone.ZoneName)

	if ctx.Err() != nil {
		result.addError("context", ctx.Err())
		o.rollbackZone(ctx, siteID, zone.ZoneID, "context cancelled before policy setup")
		result.resetZone()
		return result
	}

	policyIDs, err := o.ic.EnsurePolicies(ctx, siteID, "Tailscale", zone.ZoneID)
	if err != nil {
		result.addError("policies", err)
		o.rollbackZone(ctx, siteID, zone.ZoneID, "tailscale policy setup failed")
		result.resetZone()
		return result
	}
	result.PoliciesReady = true
	result.PolicyIDs = policyIDs
	slog.Info("integration policies ready", "count", len(policyIDs))

	discovered := o.ops.DiscoverChainPrefix(zone.ZoneID)
	if discovered != "" {
		result.ChainPrefix = discovered
	}

	oldPrefix := o.manifest.GetTailscaleChainPrefix()

	if err := o.manifest.SetTailscaleZone(zone.ZoneID, zone.ZoneName, policyIDs, result.ChainPrefix); err != nil {
		result.addError("manifest", err)
		o.rollbackZone(ctx, siteID, zone.ZoneID, "tailscale manifest save failed", policyIDs...)
		result.resetZone()
		result.resetPolicies()
		return result
	}

	if ctx.Err() != nil {
		result.addError("context", ctx.Err())
		return result
	}

	if result.ChainPrefix != oldPrefix {
		if err := o.ops.RemoveTailscaleInterfaceRules(); err != nil {
			slog.Warn("failed to remove old tailscale rules during chain prefix migration",
				"oldPrefix", oldPrefix, "newPrefix", result.ChainPrefix, "err", err)
		}
	}

	if err := o.ops.EnsureTailscaleRules(result.ChainPrefix); err != nil {
		result.addError("udapi", err)
		return result
	}
	result.UDAPIApplied = true

	slog.Info("tailscale firewall setup complete", "chainPrefix", result.ChainPrefix)
	return result
}

func (o *FirewallOrchestrator) rollbackZone(ctx context.Context, siteID, zoneID, reason string, policyIDs ...string) {
	rctx := context.WithoutCancel(ctx)
	for _, pid := range policyIDs {
		if err := o.ic.DeletePolicy(rctx, siteID, pid); err != nil {
			slog.Warn("rollback: failed to delete policy", "policyId", pid, "reason", reason, "err", err)
		}
	}
	if err := o.ic.DeleteZone(rctx, siteID, zoneID); err != nil {
		slog.Warn("rollback: failed to delete zone", "zoneId", zoneID, "reason", reason, "err", err)
	} else {
		slog.Info("rollback: zone deleted", "zoneId", zoneID, "reason", reason)
	}
}

func (o *FirewallOrchestrator) SetupWgS2sZone(ctx context.Context, tunnelID, zoneID, zoneName string) *SetupResult {
	result := &SetupResult{ChainPrefix: config.DefaultChainPrefix}

	if err := o.requireIntegration(); err != nil {
		result.addError("integration", err)
		return result
	}
	siteID := o.manifest.GetSiteID()

	if zoneID != "" {
		for _, zm := range o.manifest.GetWgS2sSnapshot() {
			if zm.ZoneID == zoneID {
				if err := o.manifest.SetWgS2sZone(tunnelID, zm); err != nil {
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

	zone, err := o.ic.EnsureZone(ctx, siteID, zoneDisplayName)
	if err != nil {
		result.addError("zone", fmt.Errorf("ensure zone %q: %w", zoneDisplayName, err))
		return result
	}
	result.ZoneCreated = true
	result.ZoneID = zone.ZoneID
	result.ZoneName = zone.ZoneName
	slog.Info("wg-s2s integration zone ready", "zoneId", zone.ZoneID, "name", zone.ZoneName)

	policyIDs, err := o.ic.EnsurePolicies(ctx, siteID, zoneName, zone.ZoneID)
	if err != nil {
		result.addError("policies", err)
		o.rollbackZone(ctx, siteID, zone.ZoneID, "wg-s2s policy setup failed")
		result.resetZone()
		return result
	}
	result.PoliciesReady = true
	result.PolicyIDs = policyIDs
	slog.Info("wg-s2s integration policies ready", "count", len(policyIDs))

	chainPrefix := o.ops.DiscoverChainPrefix(zone.ZoneID)
	if chainPrefix == "" {
		chainPrefix = config.DefaultChainPrefix
	}
	result.ChainPrefix = chainPrefix

	if err := o.manifest.SetWgS2sZone(tunnelID, ZoneManifestData{ZoneID: zone.ZoneID, ZoneName: zone.ZoneName, PolicyIDs: policyIDs, ChainPrefix: chainPrefix}); err != nil {
		result.addError("manifest", fmt.Errorf("save manifest: %w", err))
		o.rollbackZone(ctx, siteID, zone.ZoneID, "wg-s2s manifest save failed", policyIDs...)
		result.resetZone()
		result.resetPolicies()
		return result
	}
	return result
}

func (o *FirewallOrchestrator) TeardownWgS2sZone(ctx context.Context, tunnelID string) {
	zm, ok := o.manifest.GetWgS2sZone(tunnelID)
	if !ok {
		return
	}

	// Remove from manifest BEFORE checking if other tunnels share the zone.
	// This ensures GetWgS2sSnapshot below won't include the tunnel being torn down.
	if err := o.manifest.RemoveWgS2sTunnel(tunnelID); err != nil {
		slog.Warn("teardown: manifest remove failed", "tunnelId", tunnelID, "err", err)
	}

	if zm.ZoneID == "" {
		return
	}

	for _, other := range o.manifest.GetWgS2sSnapshot() {
		if other.ZoneID == zm.ZoneID {
			return
		}
	}

	if err := o.requireIntegration(); err != nil {
		return
	}
	siteID := o.manifest.GetSiteID()

	for _, policyID := range zm.PolicyIDs {
		if err := o.ic.DeletePolicy(ctx, siteID, policyID); err != nil {
			slog.Warn("teardown: policy delete failed", "policyId", policyID, "err", err)
		}
	}

	if err := o.ic.DeleteZone(ctx, siteID, zm.ZoneID); err != nil {
		slog.Warn("teardown: zone delete failed", "zoneId", zm.ZoneID, "err", err)
	} else {
		slog.Info("teardown: wg-s2s zone deleted", "zoneId", zm.ZoneID, "zoneName", zm.ZoneName)
	}
}
