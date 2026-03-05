package main

import (
	"context"
	"io"
	"strings"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"

	"unifi-tailscale/manager/internal/wgs2s"
)

// mockSSEHub implements SSEHub for testing.
type mockSSEHub struct {
	subscribeFn      func() (chan sseMessage, func(), error)
	broadcastFn      func(data []byte)
	broadcastNamedFn func(event string, data []byte)
	currentStateFn   func() []byte
}

func (m *mockSSEHub) Subscribe() (chan sseMessage, func(), error) {
	if m.subscribeFn != nil {
		return m.subscribeFn()
	}
	ch := make(chan sseMessage, 1)
	return ch, func() { close(ch) }, nil
}
func (m *mockSSEHub) Broadcast(data []byte) {
	if m.broadcastFn != nil {
		m.broadcastFn(data)
	}
}
func (m *mockSSEHub) BroadcastNamed(event string, data []byte) {
	if m.broadcastNamedFn != nil {
		m.broadcastNamedFn(event, data)
	}
}
func (m *mockSSEHub) CurrentState() []byte {
	if m.currentStateFn != nil {
		return m.currentStateFn()
	}
	return nil
}

// mockManifestStore implements ManifestStore for testing.
type mockManifestStore struct {
	getSiteIDFn              func() string
	hasSiteIDFn              func() bool
	getTailscaleZoneFn       func() ZoneManifest
	getTailscaleChainPrefixFn func() string
	getWgS2sZoneFn           func(tunnelID string) (ZoneManifest, bool)
	getWgS2sZonesFn          func() []WgS2sZoneInfo
	getWgS2sChainPrefixFn    func(tunnelID string) string
	getWanPortPolicyIDFn     func(marker string) string
	getWanPortEntryFn        func(marker string) (WanPortEntry, bool)
	getWanPortsSnapshotFn    func() map[string]WanPortEntry
	getWgS2sSnapshotFn       func() map[string]ZoneManifest
	getSystemZoneIDsFn       func() (string, string)
	hasDNSPolicyFn           func(marker string) bool
	getDNSPolicyFn           func(marker string) (DNSPolicyEntry, bool)

	setSiteIDFn          func(siteID string) error
	setTailscaleZoneFn   func(zoneID, zoneName string, policyIDs []string, chainPrefix string) error
	setWgS2sZoneFn       func(tunnelID string, zm ZoneManifest) error
	removeWgS2sTunnelFn  func(tunnelID string) error
	setWanPortFn         func(marker, policyID, policyName string, port int) error
	removeWanPortFn      func(marker string) error
	setSystemZoneIDsFn   func(externalID, gatewayID string) error
	setDNSPolicyFn       func(marker, policyID, domain, ipAddress string) error
	removeDNSPolicyFn    func(marker string) error
	resetIntegrationFn   func() error
}

func (m *mockManifestStore) GetSiteID() string {
	if m.getSiteIDFn != nil {
		return m.getSiteIDFn()
	}
	return ""
}
func (m *mockManifestStore) HasSiteID() bool {
	if m.hasSiteIDFn != nil {
		return m.hasSiteIDFn()
	}
	return false
}
func (m *mockManifestStore) GetTailscaleZone() ZoneManifest {
	if m.getTailscaleZoneFn != nil {
		return m.getTailscaleZoneFn()
	}
	return ZoneManifest{}
}
func (m *mockManifestStore) GetTailscaleChainPrefix() string {
	if m.getTailscaleChainPrefixFn != nil {
		return m.getTailscaleChainPrefixFn()
	}
	return "VPN"
}
func (m *mockManifestStore) GetWgS2sZone(tunnelID string) (ZoneManifest, bool) {
	if m.getWgS2sZoneFn != nil {
		return m.getWgS2sZoneFn(tunnelID)
	}
	return ZoneManifest{}, false
}
func (m *mockManifestStore) GetWgS2sZones() []WgS2sZoneInfo {
	if m.getWgS2sZonesFn != nil {
		return m.getWgS2sZonesFn()
	}
	return nil
}
func (m *mockManifestStore) GetWgS2sChainPrefix(tunnelID string) string {
	if m.getWgS2sChainPrefixFn != nil {
		return m.getWgS2sChainPrefixFn(tunnelID)
	}
	return ""
}
func (m *mockManifestStore) GetWanPortPolicyID(marker string) string {
	if m.getWanPortPolicyIDFn != nil {
		return m.getWanPortPolicyIDFn(marker)
	}
	return ""
}
func (m *mockManifestStore) GetWanPortEntry(marker string) (WanPortEntry, bool) {
	if m.getWanPortEntryFn != nil {
		return m.getWanPortEntryFn(marker)
	}
	return WanPortEntry{}, false
}
func (m *mockManifestStore) GetWanPortsSnapshot() map[string]WanPortEntry {
	if m.getWanPortsSnapshotFn != nil {
		return m.getWanPortsSnapshotFn()
	}
	return nil
}
func (m *mockManifestStore) GetWgS2sSnapshot() map[string]ZoneManifest {
	if m.getWgS2sSnapshotFn != nil {
		return m.getWgS2sSnapshotFn()
	}
	return nil
}
func (m *mockManifestStore) GetSystemZoneIDs() (string, string) {
	if m.getSystemZoneIDsFn != nil {
		return m.getSystemZoneIDsFn()
	}
	return "", ""
}
func (m *mockManifestStore) HasDNSPolicy(marker string) bool {
	if m.hasDNSPolicyFn != nil {
		return m.hasDNSPolicyFn(marker)
	}
	return false
}
func (m *mockManifestStore) GetDNSPolicy(marker string) (DNSPolicyEntry, bool) {
	if m.getDNSPolicyFn != nil {
		return m.getDNSPolicyFn(marker)
	}
	return DNSPolicyEntry{}, false
}
func (m *mockManifestStore) SetSiteID(siteID string) error {
	if m.setSiteIDFn != nil {
		return m.setSiteIDFn(siteID)
	}
	return nil
}
func (m *mockManifestStore) SetTailscaleZone(zoneID, zoneName string, policyIDs []string, chainPrefix string) error {
	if m.setTailscaleZoneFn != nil {
		return m.setTailscaleZoneFn(zoneID, zoneName, policyIDs, chainPrefix)
	}
	return nil
}
func (m *mockManifestStore) SetWgS2sZone(tunnelID string, zm ZoneManifest) error {
	if m.setWgS2sZoneFn != nil {
		return m.setWgS2sZoneFn(tunnelID, zm)
	}
	return nil
}
func (m *mockManifestStore) RemoveWgS2sTunnel(tunnelID string) error {
	if m.removeWgS2sTunnelFn != nil {
		return m.removeWgS2sTunnelFn(tunnelID)
	}
	return nil
}
func (m *mockManifestStore) SetWanPort(marker, policyID, policyName string, port int) error {
	if m.setWanPortFn != nil {
		return m.setWanPortFn(marker, policyID, policyName, port)
	}
	return nil
}
func (m *mockManifestStore) RemoveWanPort(marker string) error {
	if m.removeWanPortFn != nil {
		return m.removeWanPortFn(marker)
	}
	return nil
}
func (m *mockManifestStore) SetSystemZoneIDs(externalID, gatewayID string) error {
	if m.setSystemZoneIDsFn != nil {
		return m.setSystemZoneIDsFn(externalID, gatewayID)
	}
	return nil
}
func (m *mockManifestStore) SetDNSPolicy(marker, policyID, domain, ipAddress string) error {
	if m.setDNSPolicyFn != nil {
		return m.setDNSPolicyFn(marker, policyID, domain, ipAddress)
	}
	return nil
}
func (m *mockManifestStore) RemoveDNSPolicy(marker string) error {
	if m.removeDNSPolicyFn != nil {
		return m.removeDNSPolicyFn(marker)
	}
	return nil
}
func (m *mockManifestStore) ResetIntegration() error {
	if m.resetIntegrationFn != nil {
		return m.resetIntegrationFn()
	}
	return nil
}

// mockIntegrationAPI implements IntegrationAPI for testing.
type mockIntegrationAPI struct {
	setAPIKeyFn            func(key string)
	hasAPIKeyFn            func() bool
	validateFn             func(ctx context.Context) (*AppInfo, error)
	discoverSiteIDFn       func(ctx context.Context) (string, error)
	createZoneFn           func(ctx context.Context, siteID, name string) (*Zone, error)
	ensureZoneFn           func(ctx context.Context, siteID, name string) (*Zone, error)
	ensurePoliciesFn       func(ctx context.Context, siteID, name, zoneID string) ([]string, error)
	listPoliciesFn         func(ctx context.Context, siteID string) ([]Policy, error)
	deletePolicyFn         func(ctx context.Context, siteID, policyID string) error
	deleteZoneFn           func(ctx context.Context, siteID, zoneID string) error
	findInternalZoneIDFn   func(ctx context.Context, siteID string) (string, error)
	listZonesFn            func(ctx context.Context, siteID string) ([]Zone, error)
	findSystemZoneIDsFn    func(ctx context.Context, siteID string) (string, string, error)
	ensureWanPortPolicyFn  func(ctx context.Context, siteID string, port int, name, extID, gwID string) (string, error)
	ensureDNSForwardDomainFn func(ctx context.Context, siteID, domain, resolverIP string) (*DNSPolicy, error)
	deleteDNSPolicyFn      func(ctx context.Context, siteID, policyID string) error
	listDNSPoliciesFn      func(ctx context.Context, siteID string) ([]DNSPolicy, error)
}

func (m *mockIntegrationAPI) SetAPIKey(key string) {
	if m.setAPIKeyFn != nil {
		m.setAPIKeyFn(key)
	}
}
func (m *mockIntegrationAPI) HasAPIKey() bool {
	if m.hasAPIKeyFn != nil {
		return m.hasAPIKeyFn()
	}
	return false
}
func (m *mockIntegrationAPI) Validate(ctx context.Context) (*AppInfo, error) {
	if m.validateFn != nil {
		return m.validateFn(ctx)
	}
	return &AppInfo{}, nil
}
func (m *mockIntegrationAPI) DiscoverSiteID(ctx context.Context) (string, error) {
	if m.discoverSiteIDFn != nil {
		return m.discoverSiteIDFn(ctx)
	}
	return "", nil
}
func (m *mockIntegrationAPI) CreateZone(ctx context.Context, siteID, name string) (*Zone, error) {
	if m.createZoneFn != nil {
		return m.createZoneFn(ctx, siteID, name)
	}
	return &Zone{}, nil
}
func (m *mockIntegrationAPI) EnsureZone(ctx context.Context, siteID, name string) (*Zone, error) {
	if m.ensureZoneFn != nil {
		return m.ensureZoneFn(ctx, siteID, name)
	}
	return &Zone{}, nil
}
func (m *mockIntegrationAPI) EnsurePolicies(ctx context.Context, siteID, name, zoneID string) ([]string, error) {
	if m.ensurePoliciesFn != nil {
		return m.ensurePoliciesFn(ctx, siteID, name, zoneID)
	}
	return nil, nil
}
func (m *mockIntegrationAPI) ListPolicies(ctx context.Context, siteID string) ([]Policy, error) {
	if m.listPoliciesFn != nil {
		return m.listPoliciesFn(ctx, siteID)
	}
	return nil, nil
}
func (m *mockIntegrationAPI) DeletePolicy(ctx context.Context, siteID, policyID string) error {
	if m.deletePolicyFn != nil {
		return m.deletePolicyFn(ctx, siteID, policyID)
	}
	return nil
}
func (m *mockIntegrationAPI) DeleteZone(ctx context.Context, siteID, zoneID string) error {
	if m.deleteZoneFn != nil {
		return m.deleteZoneFn(ctx, siteID, zoneID)
	}
	return nil
}
func (m *mockIntegrationAPI) FindInternalZoneID(ctx context.Context, siteID string) (string, error) {
	if m.findInternalZoneIDFn != nil {
		return m.findInternalZoneIDFn(ctx, siteID)
	}
	return "", nil
}
func (m *mockIntegrationAPI) ListZones(ctx context.Context, siteID string) ([]Zone, error) {
	if m.listZonesFn != nil {
		return m.listZonesFn(ctx, siteID)
	}
	return nil, nil
}
func (m *mockIntegrationAPI) FindSystemZoneIDs(ctx context.Context, siteID string) (string, string, error) {
	if m.findSystemZoneIDsFn != nil {
		return m.findSystemZoneIDsFn(ctx, siteID)
	}
	return "", "", nil
}
func (m *mockIntegrationAPI) EnsureWanPortPolicy(ctx context.Context, siteID string, port int, name, extID, gwID string) (string, error) {
	if m.ensureWanPortPolicyFn != nil {
		return m.ensureWanPortPolicyFn(ctx, siteID, port, name, extID, gwID)
	}
	return "", nil
}
func (m *mockIntegrationAPI) EnsureDNSForwardDomain(ctx context.Context, siteID, domain, resolverIP string) (*DNSPolicy, error) {
	if m.ensureDNSForwardDomainFn != nil {
		return m.ensureDNSForwardDomainFn(ctx, siteID, domain, resolverIP)
	}
	return &DNSPolicy{}, nil
}
func (m *mockIntegrationAPI) DeleteDNSPolicy(ctx context.Context, siteID, policyID string) error {
	if m.deleteDNSPolicyFn != nil {
		return m.deleteDNSPolicyFn(ctx, siteID, policyID)
	}
	return nil
}
func (m *mockIntegrationAPI) ListDNSPolicies(ctx context.Context, siteID string) ([]DNSPolicy, error) {
	if m.listDNSPoliciesFn != nil {
		return m.listDNSPoliciesFn(ctx, siteID)
	}
	return nil, nil
}

// mockTailscaleControl implements TailscaleControl for testing.
type mockTailscaleControl struct {
	statusFn               func(ctx context.Context) (*ipnstate.Status, error)
	statusWithoutPeersFn   func(ctx context.Context) (*ipnstate.Status, error)
	editPrefsFn            func(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error)
	getPrefsFn             func(ctx context.Context) (*ipn.Prefs, error)
	startLoginInteractiveFn func(ctx context.Context) error
	startFn                func(ctx context.Context, opts ipn.Options) error
	logoutFn               func(ctx context.Context) error
	bugReportFn            func(ctx context.Context, note string) (string, error)
	checkIPForwardingFn    func(ctx context.Context) error
	currentDERPMapFn       func(ctx context.Context) (*tailcfg.DERPMap, error)
	watchIPNBusFn          func(ctx context.Context, mask ipn.NotifyWatchOpt) (IPNWatcher, error)
	tailDaemonLogsFn       func(ctx context.Context) (io.Reader, error)
}

func (m *mockTailscaleControl) Status(ctx context.Context) (*ipnstate.Status, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx)
	}
	return &ipnstate.Status{BackendState: "Running"}, nil
}
func (m *mockTailscaleControl) StatusWithoutPeers(ctx context.Context) (*ipnstate.Status, error) {
	if m.statusWithoutPeersFn != nil {
		return m.statusWithoutPeersFn(ctx)
	}
	return &ipnstate.Status{BackendState: "Running"}, nil
}
func (m *mockTailscaleControl) EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	if m.editPrefsFn != nil {
		return m.editPrefsFn(ctx, mp)
	}
	return &ipn.Prefs{}, nil
}
func (m *mockTailscaleControl) GetPrefs(ctx context.Context) (*ipn.Prefs, error) {
	if m.getPrefsFn != nil {
		return m.getPrefsFn(ctx)
	}
	return &ipn.Prefs{}, nil
}
func (m *mockTailscaleControl) StartLoginInteractive(ctx context.Context) error {
	if m.startLoginInteractiveFn != nil {
		return m.startLoginInteractiveFn(ctx)
	}
	return nil
}
func (m *mockTailscaleControl) Start(ctx context.Context, opts ipn.Options) error {
	if m.startFn != nil {
		return m.startFn(ctx, opts)
	}
	return nil
}
func (m *mockTailscaleControl) Logout(ctx context.Context) error {
	if m.logoutFn != nil {
		return m.logoutFn(ctx)
	}
	return nil
}
func (m *mockTailscaleControl) BugReport(ctx context.Context, note string) (string, error) {
	if m.bugReportFn != nil {
		return m.bugReportFn(ctx, note)
	}
	return "BUG-test", nil
}
func (m *mockTailscaleControl) CheckIPForwarding(ctx context.Context) error {
	if m.checkIPForwardingFn != nil {
		return m.checkIPForwardingFn(ctx)
	}
	return nil
}
func (m *mockTailscaleControl) CurrentDERPMap(ctx context.Context) (*tailcfg.DERPMap, error) {
	if m.currentDERPMapFn != nil {
		return m.currentDERPMapFn(ctx)
	}
	return &tailcfg.DERPMap{}, nil
}
func (m *mockTailscaleControl) WatchIPNBus(ctx context.Context, mask ipn.NotifyWatchOpt) (IPNWatcher, error) {
	if m.watchIPNBusFn != nil {
		return m.watchIPNBusFn(ctx, mask)
	}
	return &mockIPNWatcher{}, nil
}
func (m *mockTailscaleControl) TailDaemonLogs(ctx context.Context) (io.Reader, error) {
	if m.tailDaemonLogsFn != nil {
		return m.tailDaemonLogsFn(ctx)
	}
	return strings.NewReader(""), nil
}

type mockIPNWatcher struct {
	nextFn  func() (ipn.Notify, error)
	closeFn func() error
}

func (m *mockIPNWatcher) Next() (ipn.Notify, error) {
	if m.nextFn != nil {
		return m.nextFn()
	}
	return ipn.Notify{}, nil
}
func (m *mockIPNWatcher) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

// mockFirewallService implements FirewallService for testing.
type mockFirewallService struct {
	setupTailscaleFirewallFn    func(ctx context.Context) *FirewallSetupResult
	setupWgS2sZoneFn            func(ctx context.Context, tunnelID, zoneID, zoneName string) *FirewallSetupResult
	setupWgS2sFirewallFn        func(ctx context.Context, tunnelID, iface string, allowedIPs []string) error
	removeWgS2sFirewallFn       func(ctx context.Context, tunnelID, iface string, allowedIPs []string)
	removeWgS2sIPSetEntriesFn   func(ctx context.Context, tunnelID string, cidrs []string)
	openWanPortFn               func(ctx context.Context, port int, marker string) error
	closeWanPortFn              func(ctx context.Context, port int, marker string) error
	ensureDNSForwardingFn       func(ctx context.Context, magicDNSSuffix string) error
	removeDNSForwardingFn       func(ctx context.Context) error
	restoreTailscaleRulesFn     func(ctx context.Context) error
	checkTailscaleRulesPresentFn func(ctx context.Context) (bool, bool, bool, bool)
	checkWgS2sRulesPresentFn    func(ctx context.Context, ifaces []string) map[string]bool
	integrationReadyFn          func() bool
}

func (m *mockFirewallService) SetupTailscaleFirewall(ctx context.Context) *FirewallSetupResult {
	if m.setupTailscaleFirewallFn != nil {
		return m.setupTailscaleFirewallFn(ctx)
	}
	return &FirewallSetupResult{}
}
func (m *mockFirewallService) SetupWgS2sZone(ctx context.Context, tunnelID, zoneID, zoneName string) *FirewallSetupResult {
	if m.setupWgS2sZoneFn != nil {
		return m.setupWgS2sZoneFn(ctx, tunnelID, zoneID, zoneName)
	}
	return &FirewallSetupResult{}
}
func (m *mockFirewallService) SetupWgS2sFirewall(ctx context.Context, tunnelID, iface string, allowedIPs []string) error {
	if m.setupWgS2sFirewallFn != nil {
		return m.setupWgS2sFirewallFn(ctx, tunnelID, iface, allowedIPs)
	}
	return nil
}
func (m *mockFirewallService) RemoveWgS2sFirewall(ctx context.Context, tunnelID, iface string, allowedIPs []string) {
	if m.removeWgS2sFirewallFn != nil {
		m.removeWgS2sFirewallFn(ctx, tunnelID, iface, allowedIPs)
	}
}
func (m *mockFirewallService) RemoveWgS2sIPSetEntries(ctx context.Context, tunnelID string, cidrs []string) {
	if m.removeWgS2sIPSetEntriesFn != nil {
		m.removeWgS2sIPSetEntriesFn(ctx, tunnelID, cidrs)
	}
}
func (m *mockFirewallService) OpenWanPort(ctx context.Context, port int, marker string) error {
	if m.openWanPortFn != nil {
		return m.openWanPortFn(ctx, port, marker)
	}
	return nil
}
func (m *mockFirewallService) CloseWanPort(ctx context.Context, port int, marker string) error {
	if m.closeWanPortFn != nil {
		return m.closeWanPortFn(ctx, port, marker)
	}
	return nil
}
func (m *mockFirewallService) EnsureDNSForwarding(ctx context.Context, magicDNSSuffix string) error {
	if m.ensureDNSForwardingFn != nil {
		return m.ensureDNSForwardingFn(ctx, magicDNSSuffix)
	}
	return nil
}
func (m *mockFirewallService) RemoveDNSForwarding(ctx context.Context) error {
	if m.removeDNSForwardingFn != nil {
		return m.removeDNSForwardingFn(ctx)
	}
	return nil
}
func (m *mockFirewallService) RestoreTailscaleRules(ctx context.Context) error {
	if m.restoreTailscaleRulesFn != nil {
		return m.restoreTailscaleRulesFn(ctx)
	}
	return nil
}
func (m *mockFirewallService) CheckTailscaleRulesPresent(ctx context.Context) (bool, bool, bool, bool) {
	if m.checkTailscaleRulesPresentFn != nil {
		return m.checkTailscaleRulesPresentFn(ctx)
	}
	return true, true, true, true
}
func (m *mockFirewallService) CheckWgS2sRulesPresent(ctx context.Context, ifaces []string) map[string]bool {
	if m.checkWgS2sRulesPresentFn != nil {
		return m.checkWgS2sRulesPresentFn(ctx, ifaces)
	}
	return map[string]bool{}
}
func (m *mockFirewallService) IntegrationReady() bool {
	if m.integrationReadyFn != nil {
		return m.integrationReadyFn()
	}
	return false
}

// mockWgS2sControl implements WgS2sControl for testing.
type mockWgS2sControl struct {
	createTunnelFn func(cfg wgs2s.TunnelConfig, privateKey string) (*wgs2s.TunnelConfig, error)
	deleteTunnelFn func(id string) error
	enableTunnelFn func(id string) error
	disableTunnelFn func(id string) error
	updateTunnelFn func(id string, updates wgs2s.TunnelConfig) (*wgs2s.TunnelConfig, error)
	restoreAllFn   func() error
	getTunnelsFn   func() []wgs2s.TunnelConfig
	getStatusesFn  func() []wgs2s.WgS2sStatus
	getPublicKeyFn func(id string) (string, error)
	closeFn        func()
}

func (m *mockWgS2sControl) CreateTunnel(cfg wgs2s.TunnelConfig, privateKey string) (*wgs2s.TunnelConfig, error) {
	if m.createTunnelFn != nil {
		return m.createTunnelFn(cfg, privateKey)
	}
	return &cfg, nil
}
func (m *mockWgS2sControl) DeleteTunnel(id string) error {
	if m.deleteTunnelFn != nil {
		return m.deleteTunnelFn(id)
	}
	return nil
}
func (m *mockWgS2sControl) EnableTunnel(id string) error {
	if m.enableTunnelFn != nil {
		return m.enableTunnelFn(id)
	}
	return nil
}
func (m *mockWgS2sControl) DisableTunnel(id string) error {
	if m.disableTunnelFn != nil {
		return m.disableTunnelFn(id)
	}
	return nil
}
func (m *mockWgS2sControl) UpdateTunnel(id string, updates wgs2s.TunnelConfig) (*wgs2s.TunnelConfig, error) {
	if m.updateTunnelFn != nil {
		return m.updateTunnelFn(id, updates)
	}
	return &updates, nil
}
func (m *mockWgS2sControl) RestoreAll() error {
	if m.restoreAllFn != nil {
		return m.restoreAllFn()
	}
	return nil
}
func (m *mockWgS2sControl) GetTunnels() []wgs2s.TunnelConfig {
	if m.getTunnelsFn != nil {
		return m.getTunnelsFn()
	}
	return nil
}
func (m *mockWgS2sControl) GetStatuses() []wgs2s.WgS2sStatus {
	if m.getStatusesFn != nil {
		return m.getStatusesFn()
	}
	return nil
}
func (m *mockWgS2sControl) GetPublicKey(id string) (string, error) {
	if m.getPublicKeyFn != nil {
		return m.getPublicKeyFn(id)
	}
	return "", nil
}
func (m *mockWgS2sControl) Close() {
	if m.closeFn != nil {
		m.closeFn()
	}
}
