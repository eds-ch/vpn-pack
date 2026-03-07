package domain

import (
	"net/netip"
	"slices"
	"sync"
	"time"
)

type ZoneManifest struct {
	ZoneID      string   `json:"zoneId,omitempty"`
	ZoneName    string   `json:"zoneName,omitempty"`
	PolicyIDs   []string `json:"policyIds,omitempty"`
	ChainPrefix string   `json:"chainPrefix,omitempty"`
}

type WanPortEntry struct {
	PolicyID   string `json:"policyId"`
	PolicyName string `json:"policyName"`
	Port       int    `json:"port"`
}

type DNSPolicyEntry struct {
	PolicyID  string `json:"policyId"`
	Domain    string `json:"domain"`
	IPAddress string `json:"ipAddress"`
}

type WgS2sZoneInfo struct {
	ZoneID      string `json:"zoneId"`
	ZoneName    string `json:"zoneName"`
	TunnelCount int    `json:"tunnelCount"`
}

type Zone struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	NetworkIDs []string `json:"networkIds"`
}

type Policy struct {
	ID              string          `json:"id"`
	Enabled         bool            `json:"enabled"`
	Name            string          `json:"name"`
	Action          PolicyAction    `json:"action"`
	Source          PolicyEndpoint  `json:"source"`
	Destination     PolicyEndpoint  `json:"destination"`
	IPProtocolScope IPProtocolScope `json:"ipProtocolScope,omitempty"`
	LoggingEnabled  bool            `json:"loggingEnabled"`
}

type PolicyAction struct {
	Type               string `json:"type"`
	AllowReturnTraffic bool   `json:"allowReturnTraffic"`
}

type PolicyEndpoint struct {
	ZoneID        string         `json:"zoneId"`
	TrafficFilter *TrafficFilter `json:"trafficFilter,omitempty"`
}

type TrafficFilter struct {
	Type       string     `json:"type"`
	PortFilter PortFilter `json:"portFilter"`
}

type PortFilter struct {
	Type          string           `json:"type"`
	MatchOpposite bool             `json:"matchOpposite"`
	Items         []PortFilterItem `json:"items"`
}

type PortFilterItem struct {
	Type  string `json:"type"`
	Value int    `json:"value"`
}

type IPProtocolScope struct {
	IPVersion      string          `json:"ipVersion,omitempty"`
	ProtocolFilter *ProtocolFilter `json:"protocolFilter,omitempty"`
}

type ProtocolFilter struct {
	Type          string       `json:"type"`
	Protocol      ProtocolName `json:"protocol"`
	MatchOpposite bool         `json:"matchOpposite"`
}

type ProtocolName struct {
	Name string `json:"name"`
}

type SiteInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type AppInfo struct {
	ApplicationVersion string `json:"applicationVersion"`
}

type DNSPolicy struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	Domain    string `json:"domain"`
	IPAddress string `json:"ipAddress"`
	Enabled   bool   `json:"enabled"`
}

type SubnetConflict struct {
	CIDR          string `json:"cidr"`
	ConflictsWith string `json:"conflictsWith"`
	Interface     string `json:"interface,omitempty"`
	Severity      string `json:"severity"`
	Message       string `json:"message"`
}

type SSEMessage struct {
	Event string
	Data  []byte
}

type WatcherStatus string

const (
	StatusHealthy   WatcherStatus = "healthy"
	StatusDegraded  WatcherStatus = "degraded"
	StatusUnhealthy WatcherStatus = "unhealthy"
)

type WatcherHealth struct {
	Status         WatcherStatus `json:"status"`
	LastSuccess    *time.Time    `json:"lastSuccess,omitempty"`
	ReconnectCount int           `json:"reconnects"`
	LastError      string        `json:"error,omitempty"`
	DegradedReason string        `json:"degradedReason,omitempty"`
}

type HealthSnapshot struct {
	Status   WatcherStatus            `json:"status"`
	Watchers map[string]WatcherHealth `json:"watchers"`
}

type StateData struct {
	BackendState      string               `json:"backendState"`
	TailscaleIPs      []string             `json:"tailscaleIPs"`
	TailnetName       string               `json:"tailnetName"`
	AuthURL           string               `json:"authURL"`
	ControlURL        string               `json:"controlURL"`
	Version           string               `json:"version"`
	Self              *SelfNode            `json:"self,omitempty"`
	Health            []string             `json:"health,omitempty"`
	ExitNode          bool                 `json:"exitNode"`
	Routes            []RouteStatus        `json:"routes"`
	Peers             []PeerInfo           `json:"peers"`
	DERP              []DERPInfo           `json:"derp,omitempty"`
	FirewallHealth    *FirewallHealth      `json:"firewallHealth,omitempty"`
	DPIFingerprinting *bool                `json:"dpiFingerprinting,omitempty"`
	IntegrationStatus *IntegrationStatus   `json:"integrationStatus,omitempty"`
	WgS2sTunnels      []WgS2sStatus       `json:"wgS2sTunnels,omitempty"`

	SettingsFields
}

type SelfNode struct {
	HostName string `json:"hostName"`
	DNSName  string `json:"dnsName"`
	Online   bool   `json:"online"`
	TxBytes  int64  `json:"txBytes"`
	RxBytes  int64  `json:"rxBytes"`
}

type PeerInfo struct {
	HostName    string    `json:"hostName"`
	DNSName     string    `json:"dnsName"`
	TailscaleIP string    `json:"tailscaleIP"`
	OS          string    `json:"os"`
	Online      bool      `json:"online"`
	LastSeen    time.Time `json:"lastSeen"`
	CurAddr     string    `json:"curAddr"`
	Relay       string    `json:"relay"`
	PeerRelay   string    `json:"peerRelay"`
	RxBytes     int64     `json:"rxBytes"`
	TxBytes     int64     `json:"txBytes"`
	Active      bool      `json:"active"`
}

type DERPInfo struct {
	RegionID   int     `json:"regionID"`
	RegionCode string  `json:"regionCode"`
	RegionName string  `json:"regionName"`
	LatencyMs  float64 `json:"latencyMs"`
	Preferred  bool    `json:"preferred"`
}

type FirewallHealth struct {
	ZoneActive     bool   `json:"zoneActive"`
	WatcherRunning bool   `json:"watcherRunning"`
	UDAPIReachable bool   `json:"udapiReachable"`
	ChainPrefix    string `json:"chainPrefix"`
	ZoneName       string `json:"zoneName,omitempty"`
}

type TailscaleState struct {
	mu              sync.Mutex
	data            StateData
	advertiseRoutes []netip.Prefix
	allowedIPs      []netip.Prefix
}

func (ts *TailscaleState) Snapshot() StateData {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	s := ts.data
	s.TailscaleIPs = slices.Clone(s.TailscaleIPs)
	s.Health = slices.Clone(s.Health)
	s.Routes = slices.Clone(s.Routes)
	s.Peers = slices.Clone(s.Peers)
	s.DERP = slices.Clone(s.DERP)
	s.WgS2sTunnels = slices.Clone(s.WgS2sTunnels)
	s.AdvertiseTags = slices.Clone(s.AdvertiseTags)
	return s
}

func (ts *TailscaleState) Update(fn func(*StateData)) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	fn(&ts.data)
}

func (ts *TailscaleState) SetBackendState(s string) {
	ts.mu.Lock()
	ts.data.BackendState = s
	ts.mu.Unlock()
}

func (ts *TailscaleState) SetIntegrationStatus(st *IntegrationStatus) {
	ts.mu.Lock()
	ts.data.IntegrationStatus = st
	ts.mu.Unlock()
}

func (ts *TailscaleState) SetAcceptDNS(v bool) {
	ts.mu.Lock()
	ts.data.AcceptDNS = v
	ts.mu.Unlock()
}

// AdvertiseRoutes and AllowedIPs are called within Update() closures (watcher.go).
// Clone on read to prevent callers from mutating internal state.
func (ts *TailscaleState) AdvertiseRoutes() []netip.Prefix { return slices.Clone(ts.advertiseRoutes) }
func (ts *TailscaleState) SetAdvertiseRoutes(r []netip.Prefix) { ts.advertiseRoutes = r }
func (ts *TailscaleState) AllowedIPs() []netip.Prefix        { return slices.Clone(ts.allowedIPs) }
func (ts *TailscaleState) SetAllowedIPs(ips []netip.Prefix)  { ts.allowedIPs = ips }

func NewTailscaleState() *TailscaleState {
	return &TailscaleState{
		data: StateData{BackendState: "Unavailable"},
	}
}

type DeviceInfo struct {
	Hostname         string   `json:"hostname"`
	Model            string   `json:"model"`
	ModelShort       string   `json:"modelShort"`
	Firmware         string   `json:"firmware"`
	UniFiVersion     string   `json:"unifiVersion"`
	PackageVersion   string   `json:"packageVersion"`
	TailscaleVersion string   `json:"tailscaleVersion"`
	HasTUN           bool     `json:"hasTUN"`
	HasUDAPISocket   bool     `json:"hasUDAPISocket"`
	PersistentFree   int64    `json:"persistentFree"`
	ActiveVPNClients []string `json:"activeVPNClients"`
	Uptime           int64    `json:"uptime"`
}

type UpdateInfo struct {
	Available      bool   `json:"available"`
	Version        string `json:"version"`
	CurrentVersion string `json:"currentVersion"`
	ChangelogURL   string `json:"changelogURL"`
}

type RouteStatus struct {
	CIDR     string `json:"cidr"`
	Approved bool   `json:"approved"`
}

type IntegrationStatus struct {
	Configured bool   `json:"configured"`
	Valid      bool   `json:"valid"`
	SiteID     string `json:"siteId,omitempty"`
	AppVersion string `json:"appVersion,omitempty"`
	Error      string `json:"error,omitempty"`
	Reason     string `json:"reason,omitempty"`
	ZBFEnabled *bool  `json:"zbfEnabled,omitempty"`
}

type SettingsFields struct {
	Hostname             string   `json:"hostname"`
	AcceptDNS            bool     `json:"acceptDNS"`
	AcceptRoutes         bool     `json:"acceptRoutes"`
	ShieldsUp            bool     `json:"shieldsUp"`
	RunSSH               bool     `json:"runSSH"`
	NoSNAT               bool     `json:"noSNAT"`
	UDPPort              int      `json:"udpPort"`
	RelayServerPort      *uint16  `json:"relayServerPort"`
	RelayServerEndpoints string   `json:"relayServerEndpoints"`
	AdvertiseTags        []string `json:"advertiseTags"`
}
