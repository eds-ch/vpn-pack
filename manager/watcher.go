package main

import (
	"context"
	"log/slog"
	"math"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/service"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/types/netmap"
)

func (s *Server) runWatcher(ctx context.Context) {
	go s.runStatusRefresh(ctx)

	for {
		if err := s.watchLoop(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("watcher disconnected, reconnecting", "err", err)
			s.health.RecordError("tailscale", err)
			s.setUnavailable()
			select {
			case <-ctx.Done():
				return
			case <-time.After(config.ReconnectDelay):
			}
		}
	}
}

func (s *Server) runStatusRefresh(ctx context.Context) {
	ticker := time.NewTicker(config.StatusRefresh)
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
	s.health.SetDegraded("firewall", "key_expired")
	s.integration.InvalidateCache()
	return s.integration.GetStatus(ctx)
}

func (s *Server) repairMissingPolicies(ctx context.Context, status *service.IntegrationStatus) *service.IntegrationStatus {
	if status == nil || status.ZBFEnabled == nil || !*status.ZBFEnabled || s.health.IsDegraded("firewall") {
		return status
	}
	ts := s.manifest.GetTailscaleZone()
	if ts.ZoneID == "" || len(ts.PolicyIDs) != 0 || s.fw == nil {
		return status
	}
	slog.Info("ZBF enabled but policies missing, retrying firewall setup")
	if result := s.fwOrch.SetupTailscaleFirewall(ctx); result.Err() != nil {
		slog.Warn("firewall setup retry failed, will not retry until restart", "err", result.Err())
		s.health.SetDegraded("firewall", "setup_failed")
	} else {
		s.openTailscaleWanPort(ctx)
	}
	return s.integration.GetStatus(ctx)
}

func (s *Server) applyRefreshState(ctx context.Context, enrichment *statusEnrichment, integrationStatus *service.IntegrationStatus) {
	s.state.Lock()
	defer s.state.Unlock()
	s.applyEnrichment(enrichment)
	s.state.Data().FirewallHealth = s.firewallHealthSnapshot(ctx)
	s.state.Data().IntegrationStatus = integrationStatus
	s.state.Data().AcceptDNS = s.manifest != nil && s.manifest.HasDNSPolicy(config.DNSMarkerTailscale)
	s.state.Data().UDPPort = service.ReadTailscaledPort()
	if s.wgManager != nil {
		tunnels := s.wgManager.GetStatuses()
		s.wgS2sSvc.EnrichForwardINOk(ctx, tunnels)
		s.state.Data().WgS2sTunnels = tunnels
	}
}

func (s *Server) watchLoop(ctx context.Context) error {
	mask := ipn.NotifyInitialState | ipn.NotifyInitialPrefs | ipn.NotifyInitialNetMap | ipn.NotifyInitialHealthState
	watcher, err := s.ts.WatchIPNBus(ctx, mask)
	if err != nil {
		return err
	}
	defer func() { _ = watcher.Close() }()
	s.health.RecordSuccess("tailscale")

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
	s.state.Lock()
	defer s.state.Unlock()

	if n.Version != "" {
		s.state.Data().Version = n.Version
	}
	if n.State != nil {
		s.state.Data().BackendState = n.State.String()
	}
	if n.BrowseToURL != nil {
		s.state.Data().AuthURL = *n.BrowseToURL
	}
	if n.LoginFinished != nil {
		s.state.Data().AuthURL = ""
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
		s.state.Data().DPIFingerprinting = syncDPIFingerprint(s.state.Data().ExitNode)
	}

	if n.Health != nil {
		warnings := make([]string, 0, len(n.Health.Warnings))
		for code := range n.Health.Warnings {
			warnings = append(warnings, string(code))
		}
		s.state.Data().Health = warnings
	}

	return fetchStatus
}

func (s *Server) applyNotifyPrefs(p ipn.PrefsView) {
	s.state.Data().ControlURL = p.ControlURL()

	ar := p.AdvertiseRoutes()
	routes := make([]netip.Prefix, ar.Len())
	for i := range ar.Len() {
		routes[i] = ar.At(i)
	}
	s.state.SetAdvertiseRoutes(routes)

	s.state.Data().Hostname = p.Hostname()
	s.state.Data().AcceptDNS = s.manifest != nil && s.manifest.HasDNSPolicy(config.DNSMarkerTailscale)
	s.state.Data().AcceptRoutes = p.RouteAll()
	s.state.Data().ShieldsUp = p.ShieldsUp()
	s.state.Data().RunSSH = p.RunSSH()
	s.state.Data().NoSNAT = p.NoSNAT()
	s.state.Data().RelayServerPort = p.RelayServerPort().Clone()
	s.state.Data().RelayServerEndpoints = service.FormatAddrPorts(p.RelayServerStaticEndpoints().AsSlice())

	tags := p.AdvertiseTags().AsSlice()
	if tags == nil {
		tags = []string{}
	}
	s.state.Data().AdvertiseTags = tags
	s.state.Data().UDPPort = service.ReadTailscaledPort()
}

func (s *Server) refreshExternalState(ctx context.Context, fetchStatus bool) {
	var enrichment *statusEnrichment
	if fetchStatus {
		enrichment = s.fetchStatusEnrichment(ctx)
	}
	integrationStatus := s.integration.GetStatus(ctx)

	s.state.Lock()
	s.applyEnrichment(enrichment)
	s.state.Data().FirewallHealth = s.firewallHealthSnapshot(ctx)
	s.state.Data().IntegrationStatus = integrationStatus
	s.state.Unlock()
}

func (s *Server) processNetMap(nm *netmap.NetworkMap) {
	selfNode := nm.SelfNode
	if selfNode.Valid() {
		addrs := selfNode.Addresses()
		ips := make([]string, addrs.Len())
		for i := range addrs.Len() {
			ips[i] = addrs.At(i).Addr().String()
		}
		s.state.Data().TailscaleIPs = ips

		s.state.Data().Self = &SelfNode{
			HostName: selfNode.Hostinfo().Hostname(),
			DNSName:  selfNode.Name(),
			Online:   s.state.Data().BackendState == "Running",
		}

		aips := selfNode.AllowedIPs()
		aipSlice := make([]netip.Prefix, aips.Len())
		for i := range aips.Len() {
			aipSlice[i] = aips.At(i)
		}
		s.state.SetAllowedIPs(aipSlice)

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
				s.state.Data().DERP = derp
			}
		}
	}

	s.state.Data().TailnetName = nm.Domain
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
	s.state.Data().Peers = e.peers
	if s.state.Data().Self != nil {
		s.state.Data().Self.TxBytes = e.totalTx
		s.state.Data().Self.RxBytes = e.totalRx
		s.state.Data().Self.Online = e.selfOnline
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
			chainPrefix = config.DefaultChainPrefix
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
	aips := s.state.AllowedIPs()
	allowed := make(map[string]bool, len(aips))
	for _, p := range aips {
		allowed[p.String()] = true
	}

	routes, isExit := service.BuildRouteStatuses(s.state.AdvertiseRoutes(), allowed)
	s.state.Data().ExitNode = isExit
	s.state.Data().Routes = routes
}

func (s *Server) broadcastState() {
	domain.BroadcastEvent(s.hub, "", s.state.Snapshot())
}

func (s *Server) setUnavailable() {
	s.state.Lock()
	s.state.Data().BackendState = "Unavailable"
	s.state.Unlock()
	s.broadcastState()
}
