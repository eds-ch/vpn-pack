package service

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"unifi-tailscale/manager/internal/wgs2s"

	"tailscale.com/tailcfg"
)

type DiagnosticsTailscale interface {
	CheckIPForwarding(ctx context.Context) error
	CurrentDERPMap(ctx context.Context) (*tailcfg.DERPMap, error)
	BugReport(ctx context.Context, note string) (string, error)
}

type DiagnosticsFirewall interface {
	CheckWgS2sRulesPresent(ctx context.Context, ifaces []string) map[string]bool
}

type DiagnosticsWgS2s interface {
	GetTunnels() []wgs2s.TunnelConfig
	GetStatuses() []wgs2s.WgS2sStatus
}

type DiagnosticsResponse struct {
	IPForwarding  string            `json:"ipForwarding"`
	FwmarkPatched bool              `json:"fwmarkPatched"`
	FwmarkValue   string            `json:"fwmarkValue"`
	PreferredDERP int               `json:"preferredDERP"`
	DERPRegions   []DERPRegionInfo  `json:"derpRegions"`
	WgS2s         *WgS2sDiagnostics `json:"wgS2s,omitempty"`
}

type DERPRegionInfo struct {
	RegionID   int     `json:"regionID"`
	RegionCode string  `json:"regionCode"`
	RegionName string  `json:"regionName"`
	LatencyMs  float64 `json:"latencyMs"`
	Preferred  bool    `json:"preferred"`
}

type WgS2sDiagnostics struct {
	WireguardModule bool              `json:"wireguardModule"`
	Tunnels         []WgS2sTunnelDiag `json:"tunnels"`
}

type WgS2sTunnelDiag struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	InterfaceName string `json:"interfaceName"`
	InterfaceUp   bool   `json:"interfaceUp"`
	RoutesOk      bool   `json:"routesOk"`
	ForwardINOk   bool   `json:"forwardINOk"`
	Connected     bool   `json:"connected"`
	Endpoint      string `json:"endpoint,omitempty"`
}

type NetcheckResult struct {
	PreferredDERP int              `json:"PreferredDERP"`
	RegionLatency map[string]int64 `json:"RegionLatency"`
}

type DiagnosticsService struct {
	ts DiagnosticsTailscale
	fw DiagnosticsFirewall
	wg DiagnosticsWgS2s

	netcheckMu      sync.Mutex
	netcheckCache   *NetcheckResult
	netcheckCacheAt time.Time
}

func NewDiagnosticsService(ts DiagnosticsTailscale, fw DiagnosticsFirewall, wg DiagnosticsWgS2s) *DiagnosticsService {
	return &DiagnosticsService{ts: ts, fw: fw, wg: wg}
}

func (svc *DiagnosticsService) SetWgS2s(wg DiagnosticsWgS2s) {
	svc.wg = wg
}

func (svc *DiagnosticsService) GetDiagnostics(ctx context.Context) (*DiagnosticsResponse, error) {
	var (
		ipFwd   string
		nc      *NetcheckResult
		derpMap *tailcfg.DERPMap
		derpErr error
		wgDiag  *WgS2sDiagnostics
	)

	var wg sync.WaitGroup

	wg.Add(3)
	go func() {
		defer wg.Done()
		if err := svc.ts.CheckIPForwarding(ctx); err != nil {
			ipFwd = err.Error()
		} else {
			ipFwd = "enabled"
		}
	}()
	go func() {
		defer wg.Done()
		nc = svc.runNetcheck(ctx)
	}()
	go func() {
		defer wg.Done()
		derpMap, derpErr = svc.ts.CurrentDERPMap(ctx)
	}()

	if svc.wg != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			wgDiag = svc.gatherWgS2sDiagnostics(ctx)
		}()
	}

	wg.Wait()

	preferredDERP := 0
	var regionLatencyNs map[string]int64
	if nc != nil {
		preferredDERP = nc.PreferredDERP
		regionLatencyNs = nc.RegionLatency
	}
	regions := BuildDERPRegions(derpMap, derpErr, regionLatencyNs, preferredDERP)

	return &DiagnosticsResponse{
		IPForwarding:  ipFwd,
		FwmarkPatched: true,
		FwmarkValue:   "0x800000",
		PreferredDERP: preferredDERP,
		DERPRegions:   regions,
		WgS2s:         wgDiag,
	}, nil
}

func (svc *DiagnosticsService) BugReport(ctx context.Context, note string) (string, error) {
	marker, err := svc.ts.BugReport(ctx, note)
	if err != nil {
		return "", upstreamError(humanizeLocalAPIError(err), err)
	}
	return marker, nil
}

// --- Private helpers ---

var tailscaleBin = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(exe), "tailscale")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "tailscale"
})

func (svc *DiagnosticsService) runNetcheck(ctx context.Context) *NetcheckResult {
	svc.netcheckMu.Lock()
	defer svc.netcheckMu.Unlock()

	if svc.netcheckCache != nil && time.Since(svc.netcheckCacheAt) < netcheckCacheTTL {
		return svc.netcheckCache
	}

	cmdCtx, cancel := context.WithTimeout(ctx, netcheckTimeout)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, tailscaleBin(), "netcheck", "--format=json").Output()
	if err != nil {
		return svc.netcheckCache
	}

	raw := string(out)
	if idx := strings.Index(raw, "{"); idx > 0 {
		raw = raw[idx:]
	}

	var result NetcheckResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return svc.netcheckCache
	}

	svc.netcheckCache = &result
	svc.netcheckCacheAt = time.Now()
	return svc.netcheckCache
}

func (svc *DiagnosticsService) gatherWgS2sDiagnostics(ctx context.Context) *WgS2sDiagnostics {
	_, modErr := os.Stat("/sys/module/wireguard")

	tunnels := svc.wg.GetTunnels()
	statuses := svc.wg.GetStatuses()

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

	var fwPresent map[string]bool
	if svc.fw != nil {
		fwPresent = svc.fw.CheckWgS2sRulesPresent(ctx, enabledIfaces)
	}

	diags := make([]WgS2sTunnelDiag, 0, len(tunnels))
	for _, t := range tunnels {
		if !t.Enabled {
			continue
		}

		d := WgS2sTunnelDiag{
			ID:            t.ID,
			Name:          t.Name,
			InterfaceName: t.InterfaceName,
		}

		_, ifErr := net.InterfaceByName(t.InterfaceName)
		d.InterfaceUp = ifErr == nil
		d.ForwardINOk = fwPresent[t.InterfaceName]
		d.RoutesOk = CheckRoutesInstalled(t.InterfaceName, t.AllowedIPs)

		if idx, ok := statusMap[t.ID]; ok {
			d.Connected = statuses[idx].Connected
			d.Endpoint = statuses[idx].Endpoint
		}

		diags = append(diags, d)
	}

	return &WgS2sDiagnostics{
		WireguardModule: modErr == nil,
		Tunnels:         diags,
	}
}

// --- Exported pure functions ---

func BuildDERPRegions(derpMap *tailcfg.DERPMap, derpErr error, regionLatencyNs map[string]int64, preferredDERP int) []DERPRegionInfo {
	if derpErr != nil || derpMap == nil {
		return []DERPRegionInfo{}
	}

	rids := derpMap.RegionIDs()
	regions := make([]DERPRegionInfo, 0, len(rids))
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

		regions = append(regions, DERPRegionInfo{
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

func CheckRoutesInstalled(iface string, expectedCIDRs []string) bool {
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

// --- Local constants ---

const (
	netcheckCacheTTL = 60 * time.Second
	netcheckTimeout  = 10 * time.Second
)
