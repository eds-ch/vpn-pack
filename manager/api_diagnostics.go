package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tailscale.com/tailcfg"
)

type diagnosticsResponse struct {
	IPForwarding  string             `json:"ipForwarding"`
	FwmarkPatched bool               `json:"fwmarkPatched"`
	FwmarkValue   string             `json:"fwmarkValue"`
	PreferredDERP int                `json:"preferredDERP"`
	DERPRegions   []derpRegion       `json:"derpRegions"`
	WgS2s         *wgS2sDiagnostics  `json:"wgS2s,omitempty"`
}

type wgS2sDiagnostics struct {
	WireguardModule bool              `json:"wireguardModule"`
	Tunnels         []wgS2sTunnelDiag `json:"tunnels"`
}

type wgS2sTunnelDiag struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	InterfaceName string `json:"interfaceName"`
	InterfaceUp   bool   `json:"interfaceUp"`
	RoutesOk      bool   `json:"routesOk"`
	ForwardINOk   bool   `json:"forwardINOk"`
	Connected     bool   `json:"connected"`
	Endpoint      string `json:"endpoint,omitempty"`
}

type derpRegion struct {
	RegionID   int     `json:"regionID"`
	RegionCode string  `json:"regionCode"`
	RegionName string  `json:"regionName"`
	LatencyMs  float64 `json:"latencyMs"`
	Preferred  bool    `json:"preferred"`
}

type netcheckResult struct {
	PreferredDERP int              `json:"PreferredDERP"`
	RegionLatency map[string]int64 `json:"RegionLatency"`
}

var (
	netcheckCache   *netcheckResult
	netcheckCacheMu sync.Mutex
	netcheckCacheAt time.Time
)

func tailscaleBinPath() string {
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "tailscale")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "tailscale"
}

func runNetcheck(ctx context.Context) *netcheckResult {
	netcheckCacheMu.Lock()
	defer netcheckCacheMu.Unlock()

	if netcheckCache != nil && time.Since(netcheckCacheAt) < netcheckCacheTTL {
		return netcheckCache
	}

	cmdCtx, cancel := context.WithTimeout(ctx, netcheckTimeout)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, tailscaleBinPath(), "netcheck", "--format=json").Output()
	if err != nil {
		return netcheckCache
	}

	raw := string(out)
	if idx := strings.Index(raw, "{"); idx > 0 {
		raw = raw[idx:]
	}

	var result netcheckResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return netcheckCache
	}

	netcheckCache = &result
	netcheckCacheAt = time.Now()
	return netcheckCache
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	ipFwd := "enabled"
	if err := s.lc.CheckIPForwarding(r.Context()); err != nil {
		ipFwd = err.Error()
	}

	nc := runNetcheck(r.Context())
	preferredDERP := 0
	var regionLatencyNs map[string]int64
	if nc != nil {
		preferredDERP = nc.PreferredDERP
		regionLatencyNs = nc.RegionLatency
	}

	derpMap, err := s.lc.CurrentDERPMap(r.Context())
	regions := buildDERPRegions(derpMap, err, regionLatencyNs, preferredDERP)

	resp := diagnosticsResponse{
		IPForwarding:  ipFwd,
		FwmarkPatched: true,
		FwmarkValue:   "0x800000",
		PreferredDERP: preferredDERP,
		DERPRegions:   regions,
	}
	if s.wgManager != nil {
		resp.WgS2s = s.gatherWgS2sDiagnostics()
	}
	writeJSON(w, http.StatusOK, resp)
}

func buildDERPRegions(derpMap *tailcfg.DERPMap, derpErr error, regionLatencyNs map[string]int64, preferredDERP int) []derpRegion {
	if derpErr != nil || derpMap == nil {
		return []derpRegion{}
	}

	rids := derpMap.RegionIDs()
	regions := make([]derpRegion, 0, len(rids))
	for _, rid := range rids {
		reg := derpMap.Regions[rid]
		if reg == nil {
			continue
		}

		var latMs float64
		if regionLatencyNs != nil {
			ns := regionLatencyNs[strconv.Itoa(rid)]
			if ns > 0 {
				latMs = float64(ns) / 1e6
			}
		}

		regions = append(regions, derpRegion{
			RegionID:   rid,
			RegionCode: reg.RegionCode,
			RegionName: reg.RegionName,
			LatencyMs:  latMs,
			Preferred:  rid == preferredDERP,
		})
	}
	sort.Slice(regions, func(i, j int) bool {
		if regions[i].LatencyMs == 0 {
			return false
		}
		if regions[j].LatencyMs == 0 {
			return true
		}
		return regions[i].LatencyMs < regions[j].LatencyMs
	})
	return regions
}

func (s *Server) handleBugReport(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Note string `json:"note"`
	}
	if r.Body != nil && r.ContentLength > 0 {
		if err := readJSON(w, r, &req); err != nil {
			return
		}
	}

	marker, err := s.lc.BugReport(r.Context(), req.Note)
	if err != nil {
		writeError(w, http.StatusInternalServerError, humanizeLocalAPIError(err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"marker": marker})
}

func (s *Server) gatherWgS2sDiagnostics() *wgS2sDiagnostics {
	_, modErr := os.Stat("/sys/module/wireguard")

	tunnels := s.wgManager.GetTunnels()
	statuses := s.wgManager.GetStatuses()

	statusMap := make(map[string]int, len(statuses))
	for i := range statuses {
		statusMap[statuses[i].ID] = i
	}

	var enabledIfaces []string
	for _, t := range tunnels {
		if t.Enabled {
			enabledIfaces = append(enabledIfaces, t.InterfaceName)
		}
	}
	fwPresent := s.fw.CheckWgS2sRulesPresent(enabledIfaces)

	diags := make([]wgS2sTunnelDiag, 0, len(tunnels))
	for _, t := range tunnels {
		if !t.Enabled {
			continue
		}

		d := wgS2sTunnelDiag{
			ID:            t.ID,
			Name:          t.Name,
			InterfaceName: t.InterfaceName,
		}

		_, ifErr := net.InterfaceByName(t.InterfaceName)
		d.InterfaceUp = ifErr == nil
		d.ForwardINOk = fwPresent[t.InterfaceName]
		d.RoutesOk = checkRoutesInstalled(t.InterfaceName, t.AllowedIPs)

		if idx, ok := statusMap[t.ID]; ok {
			d.Connected = statuses[idx].Connected
			d.Endpoint = statuses[idx].Endpoint
		}

		diags = append(diags, d)
	}

	return &wgS2sDiagnostics{
		WireguardModule: modErr == nil,
		Tunnels:         diags,
	}
}

func checkRoutesInstalled(iface string, expectedCIDRs []string) bool {
	if len(expectedCIDRs) == 0 {
		return true
	}

	out, err := exec.Command("ip", "-j", "route", "show", "dev", iface).Output()
	if err != nil {
		return false
	}

	var routes []struct {
		Dst string `json:"dst"`
	}
	if err := json.Unmarshal(out, &routes); err != nil {
		return false
	}

	installed := make(map[string]bool, len(routes))
	for _, r := range routes {
		installed[r.Dst] = true
	}

	for _, cidr := range expectedCIDRs {
		if !installed[cidr] {
			return false
		}
	}
	return true
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	entries := s.logBuf.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{"lines": entries})
}
