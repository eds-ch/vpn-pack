package main

import (
	"context"
	"log/slog"
	"math"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/netmap"
	"unifi-tailscale/manager/internal/wgs2s"
	"unifi-tailscale/manager/service"
)

type TailscaleState struct {
	mu              sync.Mutex
	data            stateData
	advertiseRoutes []netip.Prefix
	allowedIPs      []netip.Prefix
}

type stateData struct {
	BackendState      string              `json:"backendState"`
	TailscaleIPs      []string            `json:"tailscaleIPs"`
	TailnetName       string              `json:"tailnetName"`
	AuthURL           string              `json:"authURL"`
	ControlURL        string              `json:"controlURL"`
	Version           string              `json:"version"`
	Self              *SelfNode           `json:"self,omitempty"`
	Health            []string            `json:"health,omitempty"`
	ExitNode          bool                `json:"exitNode"`
	Routes            []service.RouteStatus `json:"routes"`
	Peers             []PeerInfo          `json:"peers"`
	DERP              []DERPInfo          `json:"derp,omitempty"`
	FirewallHealth    *FirewallHealth     `json:"firewallHealth,omitempty"`
	DPIFingerprinting *bool               `json:"dpiFingerprinting,omitempty"`
	IntegrationStatus *service.IntegrationStatus `json:"integrationStatus,omitempty"`
	WgS2sTunnels      []wgs2s.WgS2sStatus `json:"wgS2sTunnels,omitempty"`

	service.SettingsFields
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

func (ts *TailscaleState) snapshot() stateData {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.data
}

func (s *Server) runWatcher(ctx context.Context) {
	go s.runStatusRefresh(ctx)

	for {
		if err := s.watchLoop(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("watcher disconnected, reconnecting", "err", err)
			s.setUnavailable()
			select {
			case <-ctx.Done():
				return
			case <-time.After(reconnectDelay):
			}
		}
	}
}

func (s *Server) runStatusRefresh(ctx context.Context) {
	ticker := time.NewTicker(statusRefresh)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshTick(ctx)
		}
	}
}

func (s *Server) refreshTick(ctx context.Context) {
	enrichment := s.fetchStatusEnrichment(ctx)
	integrationStatus := s.integration.GetStatus(ctx)
	integrationStatus = s.handleAPIKeyExpiry(ctx, integrationStatus)
	integrationStatus = s.repairMissingPolicies(ctx, integrationStatus)
	s.applyRefreshState(ctx, enrichment, integrationStatus)
	s.broadcastState()
}

func (s *Server) handleAPIKeyExpiry(ctx context.Context, status *service.IntegrationStatus) *service.IntegrationStatus {
	if status == nil || status.Reason != "key_expired" || !s.ic.HasAPIKey() {
		return status
	}
	slog.Warn("periodic check: API key rejected, clearing")
	s.ic.SetAPIKey("")
	_ = service.DeleteAPIKey()
	_ = s.manifest.ResetIntegration()
	s.intRetry.markDegraded()
	s.integration.InvalidateCache()
	return s.integration.GetStatus(ctx)
}

func (s *Server) repairMissingPolicies(ctx context.Context, status *service.IntegrationStatus) *service.IntegrationStatus {
	if status == nil || status.ZBFEnabled == nil || !*status.ZBFEnabled || s.intRetry.isDegraded() {
		return status
	}
	ts := s.manifest.GetTailscaleZone()
	if ts.ZoneID == "" || len(ts.PolicyIDs) != 0 || s.fw == nil {
		return status
	}
	slog.Info("ZBF enabled but policies missing, retrying firewall setup")
	if result := s.fw.SetupTailscaleFirewall(ctx); result.Err() != nil {
		slog.Warn("firewall setup retry failed, will not retry until restart", "err", result.Err())
		s.intRetry.markDegraded()
	} else {
		s.openTailscaleWanPort(ctx)
	}
	return s.integration.GetStatus(ctx)
}

func (s *Server) applyRefreshState(ctx context.Context, enrichment *statusEnrichment, integrationStatus *service.IntegrationStatus) {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	s.applyEnrichment(enrichment)
	s.state.data.FirewallHealth = s.firewallHealthSnapshot(ctx)
	s.state.data.IntegrationStatus = integrationStatus
	s.state.data.AcceptDNS = s.manifest != nil && s.manifest.HasDNSPolicy(dnsMarkerTailscale)
	s.state.data.UDPPort = service.ReadTailscaledPort()
	if s.wgManager != nil {
		tunnels := s.wgManager.GetStatuses()
		s.wgS2sSvc.EnrichForwardINOk(ctx, tunnels)
		s.state.data.WgS2sTunnels = tunnels
	}
}

func (s *Server) watchLoop(ctx context.Context) error {
	mask := ipn.NotifyInitialState | ipn.NotifyInitialPrefs | ipn.NotifyInitialNetMap | ipn.NotifyInitialHealthState
	watcher, err := s.ts.WatchIPNBus(ctx, mask)
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Close() }()

	for {
		n, err := watcher.Next()
		if err != nil {
			return err
		}
		s.processNotify(ctx, &n)
	}
}

func (s *Server) processNotify(ctx context.Context, n *ipn.Notify) {
	fetchStatus := s.updateStateFromNotify(n)
	s.refreshExternalState(ctx, fetchStatus)
	s.broadcastState()
}

func (s *Server) updateStateFromNotify(n *ipn.Notify) bool {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()

	if n.Version != "" {
		s.state.data.Version = n.Version
	}
	if n.State != nil {
		s.state.data.BackendState = n.State.String()
	}
	if n.BrowseToURL != nil {
		s.state.data.AuthURL = *n.BrowseToURL
	}
	if n.LoginFinished != nil {
		s.state.data.AuthURL = ""
	}

	if n.Prefs != nil && n.Prefs.Valid() {
		s.applyNotifyPrefs(*n.Prefs)
	}

	var fetchStatus bool
	if n.NetMap != nil {
		s.processNetMap(n.NetMap)
		fetchStatus = true
	}

	if n.Prefs != nil || n.NetMap != nil {
		s.recomputeRoutes()
		s.state.data.DPIFingerprinting = syncDPIFingerprint(s.state.data.ExitNode)
	}

	if n.Health != nil {
		warnings := make([]string, 0, len(n.Health.Warnings))
		for code := range n.Health.Warnings {
			warnings = append(warnings, string(code))
		}
		s.state.data.Health = warnings
	}

	return fetchStatus
}

func (s *Server) applyNotifyPrefs(p ipn.PrefsView) {
	s.state.data.ControlURL = p.ControlURL()

	ar := p.AdvertiseRoutes()
	s.state.advertiseRoutes = make([]netip.Prefix, ar.Len())
	for i := range ar.Len() {
		s.state.advertiseRoutes[i] = ar.At(i)
	}

	s.state.data.Hostname = p.Hostname()
	s.state.data.AcceptDNS = s.manifest != nil && s.manifest.HasDNSPolicy(dnsMarkerTailscale)
	s.state.data.AcceptRoutes = p.RouteAll()
	s.state.data.ShieldsUp = p.ShieldsUp()
	s.state.data.RunSSH = p.RunSSH()
	s.state.data.NoSNAT = p.NoSNAT()
	s.state.data.RelayServerPort = p.RelayServerPort().Clone()
	s.state.data.RelayServerEndpoints = service.FormatAddrPorts(p.RelayServerStaticEndpoints().AsSlice())

	tags := p.AdvertiseTags().AsSlice()
	if tags == nil {
		tags = []string{}
	}
	s.state.data.AdvertiseTags = tags
	s.state.data.UDPPort = service.ReadTailscaledPort()
}

func (s *Server) refreshExternalState(ctx context.Context, fetchStatus bool) {
	var enrichment *statusEnrichment
	if fetchStatus {
		enrichment = s.fetchStatusEnrichment(ctx)
	}
	integrationStatus := s.integration.GetStatus(ctx)

	s.state.mu.Lock()
	s.applyEnrichment(enrichment)
	s.state.data.FirewallHealth = s.firewallHealthSnapshot(ctx)
	s.state.data.IntegrationStatus = integrationStatus
	s.state.mu.Unlock()
}

func (s *Server) processNetMap(nm *netmap.NetworkMap) {
	selfNode := nm.SelfNode
	if selfNode.Valid() {
		addrs := selfNode.Addresses()
		ips := make([]string, addrs.Len())
		for i := range addrs.Len() {
			ips[i] = addrs.At(i).Addr().String()
		}
		s.state.data.TailscaleIPs = ips

		s.state.data.Self = &SelfNode{
			HostName: selfNode.Hostinfo().Hostname(),
			DNSName:  selfNode.Name(),
			Online:   s.state.data.BackendState == "Running",
		}

		aips := selfNode.AllowedIPs()
		s.state.allowedIPs = make([]netip.Prefix, aips.Len())
		for i := range aips.Len() {
			s.state.allowedIPs[i] = aips.At(i)
		}

		ni := selfNode.Hostinfo().NetInfo()
		if ni.Valid() && nm.DERPMap != nil {
			preferred := ni.PreferredDERP()
			latMap := ni.DERPLatency()
			if latMap.Len() > 0 {
				regionLat := make(map[int]float64)
				for key, val := range latMap.AsMap() {
					idStr, _, _ := strings.Cut(key, "-")
					rid, err := strconv.Atoi(idStr)
					if err != nil || val <= 0 {
						continue
					}
					if cur, ok := regionLat[rid]; !ok || val < cur {
						regionLat[rid] = val
					}
				}
				derp := make([]DERPInfo, 0, len(regionLat))
				for rid, lat := range regionLat {
					reg := nm.DERPMap.Regions[rid]
					if reg == nil {
						continue
					}
					derp = append(derp, DERPInfo{
						RegionID:   rid,
						RegionCode: reg.RegionCode,
						RegionName: reg.RegionName,
						LatencyMs:  math.Round(lat*10000) / 10,
						Preferred:  rid == preferred,
					})
				}
				sort.Slice(derp, func(i, j int) bool {
					return derp[i].LatencyMs < derp[j].LatencyMs
				})
				s.state.data.DERP = derp
			}
		}
	}

	s.state.data.TailnetName = nm.Domain
}

type statusEnrichment struct {
	peers      []PeerInfo
	totalTx    int64
	totalRx    int64
	selfOnline bool
}

func (s *Server) fetchStatusEnrichment(ctx context.Context) *statusEnrichment {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	st, err := s.ts.Status(ctx)
	if err != nil {
		slog.Warn("lc.Status failed", "err", err)
		return nil
	}

	peers := extractPeers(st)
	var totalTx, totalRx int64
	for _, p := range st.Peer {
		totalTx += p.TxBytes
		totalRx += p.RxBytes
	}

	selfOnline := false
	if st.Self != nil {
		selfOnline = st.Self.Online
	}

	return &statusEnrichment{
		peers:      peers,
		totalTx:    totalTx,
		totalRx:    totalRx,
		selfOnline: selfOnline,
	}
}

func (s *Server) applyEnrichment(e *statusEnrichment) {
	if e == nil {
		return
	}
	s.state.data.Peers = e.peers
	if s.state.data.Self != nil {
		s.state.data.Self.TxBytes = e.totalTx
		s.state.data.Self.RxBytes = e.totalRx
		s.state.data.Self.Online = e.selfOnline
	}
}

func extractPeers(st *ipnstate.Status) []PeerInfo {
	if st == nil || st.Peer == nil {
		return []PeerInfo{}
	}
	peers := make([]PeerInfo, 0, len(st.Peer))
	for _, p := range st.Peer {
		if p.ShareeNode {
			continue
		}
		ip := ""
		if len(p.TailscaleIPs) > 0 {
			ip = p.TailscaleIPs[0].String()
		}
		peers = append(peers, PeerInfo{
			HostName:    p.HostName,
			DNSName:     p.DNSName,
			TailscaleIP: ip,
			OS:          p.OS,
			Online:      p.Online,
			LastSeen:    p.LastSeen,
			CurAddr:     p.CurAddr,
			Relay:       p.Relay,
			PeerRelay:   p.PeerRelay,
			RxBytes:     p.RxBytes,
			TxBytes:     p.TxBytes,
			Active:      p.Active,
		})
	}
	return peers
}

func (s *Server) firewallHealthSnapshot(ctx context.Context) *FirewallHealth {
	if !s.deviceInfo.HasUDAPISocket {
		return nil
	}

	var forward bool
	if s.fw != nil {
		forward, _, _, _ = s.fw.CheckTailscaleRulesPresent(ctx)
	}

	udapiReachable := isUDAPIReachable()

	var chainPrefix, zoneName string
	if s.manifest != nil {
		ts := s.manifest.GetTailscaleZone()
		chainPrefix = ts.ChainPrefix
		if chainPrefix == "" {
			chainPrefix = defaultChainPrefix
		}
		zoneName = ts.ZoneName
	}

	return &FirewallHealth{
		ZoneActive:     forward,
		WatcherRunning: s.watcherRunning.Load(),
		UDAPIReachable: udapiReachable,
		ChainPrefix:    chainPrefix,
		ZoneName:       zoneName,
	}
}

func (s *Server) recomputeRoutes() {
	allowed := make(map[string]bool, len(s.state.allowedIPs))
	for _, p := range s.state.allowedIPs {
		allowed[p.String()] = true
	}

	routes, isExit := service.BuildRouteStatuses(s.state.advertiseRoutes, allowed)
	s.state.data.ExitNode = isExit
	s.state.data.Routes = routes
}

func (s *Server) broadcastState() {
	BroadcastEvent(s.hub, "", s.state.snapshot())
}

func (s *Server) setUnavailable() {
	s.state.mu.Lock()
	s.state.data.BackendState = "Unavailable"
	s.state.mu.Unlock()
	s.broadcastState()
}
