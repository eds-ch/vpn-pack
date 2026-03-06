package domain

import (
	"context"
	"io"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
)

type SSEHub interface {
	Subscribe() (chan SSEMessage, func(), error)
	Broadcast(data []byte)
	BroadcastNamed(event string, data []byte)
	CurrentState() []byte
}

type ManifestStore interface {
	GetSiteID() string
	HasSiteID() bool
	GetTailscaleZone() ZoneManifest
	GetTailscaleChainPrefix() string
	GetWgS2sZone(tunnelID string) (ZoneManifest, bool)
	GetWgS2sZones() []WgS2sZoneInfo
	GetWgS2sChainPrefix(tunnelID string) string
	GetWanPortPolicyID(marker string) string
	GetWanPortEntry(marker string) (WanPortEntry, bool)
	GetWanPortsSnapshot() map[string]WanPortEntry
	GetWgS2sSnapshot() map[string]ZoneManifest
	GetSystemZoneIDs() (string, string)
	HasDNSPolicy(marker string) bool
	GetDNSPolicy(marker string) (DNSPolicyEntry, bool)

	SetSiteID(siteID string) error
	SetTailscaleZone(zoneID, zoneName string, policyIDs []string, chainPrefix string) error
	SetWgS2sZone(tunnelID string, zm ZoneManifest) error
	RemoveWgS2sTunnel(tunnelID string) error
	SetWanPort(marker, policyID, policyName string, port int) error
	RemoveWanPort(marker string) error
	SetSystemZoneIDs(externalID, gatewayID string) error
	SetDNSPolicy(marker, policyID, domain, ipAddress string) error
	RemoveDNSPolicy(marker string) error
	ResetIntegration() error
}

type IntegrationAPI interface {
	SetAPIKey(key string)
	HasAPIKey() bool
	Validate(ctx context.Context) (*AppInfo, error)
	DiscoverSiteID(ctx context.Context) (string, error)
	CreateZone(ctx context.Context, siteID, name string) (*Zone, error)
	EnsureZone(ctx context.Context, siteID, name string) (*Zone, error)
	EnsurePolicies(ctx context.Context, siteID, name, zoneID string) ([]string, error)
	ListPolicies(ctx context.Context, siteID string) ([]Policy, error)
	DeletePolicy(ctx context.Context, siteID, policyID string) error
	DeleteZone(ctx context.Context, siteID, zoneID string) error
	FindInternalZoneID(ctx context.Context, siteID string) (string, error)
	ListZones(ctx context.Context, siteID string) ([]Zone, error)
	FindSystemZoneIDs(ctx context.Context, siteID string) (string, string, error)
	EnsureWanPortPolicy(ctx context.Context, siteID string, port int, name, extID, gwID string) (string, error)
	EnsureDNSForwardDomain(ctx context.Context, siteID, domain, resolverIP string) (*DNSPolicy, error)
	DeleteDNSPolicy(ctx context.Context, siteID, policyID string) error
	ListDNSPolicies(ctx context.Context, siteID string) ([]DNSPolicy, error)
}

type IPNWatcher interface {
	Next() (ipn.Notify, error)
	Close() error
}

type TailscaleControl interface {
	Status(ctx context.Context) (*ipnstate.Status, error)
	StatusWithoutPeers(ctx context.Context) (*ipnstate.Status, error)
	EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error)
	GetPrefs(ctx context.Context) (*ipn.Prefs, error)
	StartLoginInteractive(ctx context.Context) error
	Start(ctx context.Context, opts ipn.Options) error
	Logout(ctx context.Context) error
	BugReport(ctx context.Context, note string) (string, error)
	CheckIPForwarding(ctx context.Context) error
	CurrentDERPMap(ctx context.Context) (*tailcfg.DERPMap, error)
	WatchIPNBus(ctx context.Context, mask ipn.NotifyWatchOpt) (IPNWatcher, error)
	TailDaemonLogs(ctx context.Context) (io.Reader, error)
}

type FirewallService interface {
	SetupWgS2sFirewall(ctx context.Context, tunnelID, iface string, allowedIPs []string) error
	RemoveWgS2sFirewall(ctx context.Context, tunnelID, iface string, allowedIPs []string)
	RemoveWgS2sIPSetEntries(ctx context.Context, tunnelID string, cidrs []string)
	OpenWanPort(ctx context.Context, port int, marker string) error
	CloseWanPort(ctx context.Context, port int, marker string) error
	EnsureDNSForwarding(ctx context.Context, magicDNSSuffix string) error
	RemoveDNSForwarding(ctx context.Context) error
	RestoreTailscaleRules(ctx context.Context) error
	RestoreRulesWithRetry(ctx context.Context, retries int, delay time.Duration)
	WaitBackground()
	CheckTailscaleRulesPresent(ctx context.Context) (forward, input, output, ipset bool)
	CheckWgS2sRulesPresent(ctx context.Context, ifaces []string) map[string]bool
	DiscoverChainPrefix(zoneID string) string
	EnsureTailscaleRules(chainPrefix string) error
	RemoveTailscaleInterfaceRules() error
	IntegrationReady() bool
}

type WgS2sControl interface {
	CreateTunnel(cfg TunnelConfig, privateKey string) (*TunnelConfig, error)
	DeleteTunnel(id string) error
	EnableTunnel(id string) error
	DisableTunnel(id string) error
	UpdateTunnel(id string, updates TunnelConfig) (*TunnelConfig, error)
	RestoreAll() error
	GetTunnels() []TunnelConfig
	GetStatuses() []WgS2sStatus
	GetPublicKey(id string) (string, error)
	Close()
}
