package main

import (
	"context"
	"encoding/json"
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
)

type TailscaleState struct {
	mu              sync.Mutex
	data            stateData
	advertiseRoutes []netip.Prefix
	allowedIPs      []netip.Prefix
}

type stateData struct {
	BackendState   string              `json:"backendState"`
	TailscaleIPs   []string            `json:"tailscaleIPs"`
	TailnetName    string              `json:"tailnetName"`
	AuthURL        string              `json:"authURL"`
	ControlURL     string              `json:"controlURL"`
	Version        string              `json:"version"`
	Self           *SelfNode           `json:"self,omitempty"`
	Health         []string            `json:"health,omitempty"`
	ExitNode       bool                `json:"exitNode"`
	Routes         []RouteStatus       `json:"routes"`
	Peers          []PeerInfo          `json:"peers"`
	DERP              []DERPInfo           `json:"derp,omitempty"`
	FirewallHealth    *FirewallHealth     `json:"firewallHealth,omitempty"`
	DPIFingerprinting *bool               `json:"dpiFingerprinting,omitempty"`
	IntegrationStatus *IntegrationStatus  `json:"integrationStatus,omitempty"`
	WgS2sTunnels      []wgs2s.WgS2sStatus `json:"wgS2sTunnels,omitempty"`

	// Settings (from Tailscale prefs, pushed via SSE for live sync)
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

type RouteStatus struct {
	CIDR     string `json:"cidr"`
	Approved bool   `json:"approved"`
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
	ZoneActive     bool `json:"zoneActive"`
	WatcherRunning bool `json:"watcherRunning"`
	UDAPIReachable bool `json:"udapiReachable"`
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
			enrichment := s.fetchStatusEnrichment()
			integrationStatus := s.fetchIntegrationStatus()

			if integrationStatus != nil && integrationStatus.Reason == "key_expired" && s.ic.HasAPIKey() {
				slog.Warn("periodic check: API key rejected, clearing")
				s.ic.SetAPIKey("")
				_ = deleteAPIKey()
				s.manifest.ResetIntegration()
				_ = s.manifest.Save()
				s.integrationDegraded.Store(true)
				integrationStatus = s.fetchIntegrationStatus()
			}

			if integrationStatus != nil && integrationStatus.ZBFEnabled != nil && *integrationStatus.ZBFEnabled && !s.integrationDegraded.Load() {
				ts := s.manifest.GetTailscaleZone()
				if ts.ZoneID != "" && len(ts.PolicyIDs) == 0 && s.fw != nil {
					slog.Info("ZBF enabled but policies missing, retrying firewall setup")
					if err := s.fw.SetupTailscaleFirewall(); err != nil {
						slog.Warn("firewall setup retry failed, will not retry until restart", "err", err)
						s.integrationDegraded.Store(true)
					} else {
						if port := readTailscaledPort(); port > 0 {
							if err := s.fw.OpenWanPort(port, "tailscale-wg"); err != nil {
								slog.Warn("WAN port open failed", "port", port, "err", err)
							}
						}
					}
					integrationStatus = s.fetchIntegrationStatus()
				}
			}

			s.state.mu.Lock()
			s.applyEnrichment(enrichment)
			s.state.data.FirewallHealth = s.firewallHealthSnapshot()
			s.state.data.DPIFingerprinting = syncDPIFingerprint(s.state.data.ExitNode)
			s.state.data.IntegrationStatus = integrationStatus
			s.state.data.UDPPort = readTailscaledPort()
			if s.wgManager != nil {
				tunnels := s.wgManager.GetStatuses()
				if s.fw != nil {
					var ifaces []string
					for _, t := range tunnels {
						if t.Enabled {
							ifaces = append(ifaces, t.InterfaceName)
						}
					}
					fwPresent := s.fw.CheckWgS2sRulesPresent(ifaces)
					for i := range tunnels {
						tunnels[i].ForwardINOk = fwPresent[tunnels[i].InterfaceName]
					}
				}
				s.state.data.WgS2sTunnels = tunnels
			}
			s.state.mu.Unlock()

			s.broadcastState()
		}
	}
}

func (s *Server) watchLoop(ctx context.Context) error {
	mask := ipn.NotifyInitialState | ipn.NotifyInitialPrefs | ipn.NotifyInitialNetMap | ipn.NotifyInitialHealthState
	watcher, err := s.lc.WatchIPNBus(ctx, mask)
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Close() }()

	for {
		n, err := watcher.Next()
		if err != nil {
			return err
		}
		s.processNotify(&n)
	}
}

func (s *Server) processNotify(n *ipn.Notify) {
	var fetchStatus bool

	s.state.mu.Lock()

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
		s.state.data.ControlURL = n.Prefs.ControlURL()
		ar := n.Prefs.AdvertiseRoutes()
		s.state.advertiseRoutes = make([]netip.Prefix, ar.Len())
		for i := range ar.Len() {
			s.state.advertiseRoutes[i] = ar.At(i)
		}

		s.state.data.Hostname = n.Prefs.Hostname()
		s.state.data.AcceptDNS = n.Prefs.CorpDNS()
		s.state.data.AcceptRoutes = n.Prefs.RouteAll()
		s.state.data.ShieldsUp = n.Prefs.ShieldsUp()
		s.state.data.RunSSH = n.Prefs.RunSSH()
		s.state.data.NoSNAT = n.Prefs.NoSNAT()
		s.state.data.RelayServerPort = n.Prefs.RelayServerPort().Clone()
		s.state.data.RelayServerEndpoints = formatAddrPorts(n.Prefs.RelayServerStaticEndpoints().AsSlice())
		tags := n.Prefs.AdvertiseTags().AsSlice()
		if tags == nil {
			tags = []string{}
		}
		s.state.data.AdvertiseTags = tags
		s.state.data.UDPPort = readTailscaledPort()
	}

	if n.NetMap != nil {
		s.processNetMap(n.NetMap)
		fetchStatus = true
	}

	s.recomputeRoutes()
	s.state.data.DPIFingerprinting = syncDPIFingerprint(s.state.data.ExitNode)

	if n.Health != nil {
		warnings := make([]string, 0, len(n.Health.Warnings))
		for code := range n.Health.Warnings {
			warnings = append(warnings, string(code))
		}
		s.state.data.Health = warnings
	}

	s.state.mu.Unlock()

	var enrichment *statusEnrichment
	if fetchStatus {
		enrichment = s.fetchStatusEnrichment()
	}

	integrationStatus := s.fetchIntegrationStatus()
	s.state.mu.Lock()
	s.applyEnrichment(enrichment)
	s.state.data.FirewallHealth = s.firewallHealthSnapshot()
	s.state.data.IntegrationStatus = integrationStatus
	s.state.mu.Unlock()

	s.broadcastState()
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

func (s *Server) fetchStatusEnrichment() *statusEnrichment {
	st, err := s.lc.Status(context.Background())
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

func (s *Server) firewallHealthSnapshot() *FirewallHealth {
	if !s.deviceInfo.HasUDAPISocket {
		return nil
	}

	var forward bool
	if s.fw != nil {
		forward, _, _, _ = s.fw.CheckTailscaleRulesPresent()
	}

	udapiReachable := isUDAPIReachable()

	return &FirewallHealth{
		ZoneActive:     forward,
		WatcherRunning: s.watcherRunning.Load(),
		UDAPIReachable: udapiReachable,
	}
}

func (s *Server) recomputeRoutes() {
	allowed := make(map[string]bool, len(s.state.allowedIPs))
	for _, p := range s.state.allowedIPs {
		allowed[p.String()] = true
	}

	var routes []RouteStatus
	isExit := false
	for _, p := range s.state.advertiseRoutes {
		str := p.String()
		if str == "0.0.0.0/0" || str == "::/0" {
			isExit = true
			continue
		}
		routes = append(routes, RouteStatus{
			CIDR:     str,
			Approved: allowed[str],
		})
	}
	if routes == nil {
		routes = []RouteStatus{}
	}
	s.state.data.ExitNode = isExit
	s.state.data.Routes = routes
}

func (s *Server) broadcastState() {
	snap := s.state.snapshot()
	data, err := json.Marshal(snap)
	if err != nil {
		slog.Error("failed to marshal state", "err", err)
		return
	}
	s.hub.Broadcast(data)
}

func (s *Server) setUnavailable() {
	s.state.mu.Lock()
	s.state.data.BackendState = "Unavailable"
	s.state.mu.Unlock()
	s.broadcastState()
}
