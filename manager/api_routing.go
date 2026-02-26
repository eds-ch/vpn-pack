package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"tailscale.com/ipn"
	"unifi-tailscale/manager/internal/wgs2s"
)

type setRoutesRequest struct {
	Routes   []string `json:"routes"`
	ExitNode bool     `json:"exitNode"`
}

type setRoutesResponse struct {
	OK       bool   `json:"ok"`
	Message  string `json:"message"`
	AdminURL string `json:"adminURL"`
	Warning  string `json:"warning,omitempty"`
}

type routesResponse struct {
	Routes   []RouteStatus `json:"routes"`
	ExitNode bool          `json:"exitNode"`
}

type subnetEntry struct {
	CIDR string `json:"cidr"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type subnetsResponse struct {
	Subnets []subnetEntry `json:"subnets"`
}

type authKeyRequest struct {
	AuthKey string `json:"authKey"`
}

type firewallStatusResponse struct {
	IntegrationAPI bool              `json:"integrationAPI"`
	ChainPrefix    string            `json:"chainPrefix"`
	WatcherRunning bool              `json:"watcherRunning"`
	LastRestore    *time.Time        `json:"lastRestore"`
	RulesPresent   map[string]bool   `json:"rulesPresent"`
	UDAPIReachable bool              `json:"udapiReachable"`
}

func (s *Server) handleGetRoutes(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.lc.GetPrefs(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, humanizeLocalAPIError(err))
		return
	}

	isExit := false
	allowed := make(map[string]bool)
	st, err := s.lc.Status(r.Context())
	if err == nil && st.Self != nil && st.Self.AllowedIPs != nil {
		for i := range st.Self.AllowedIPs.Len() {
			allowed[st.Self.AllowedIPs.At(i).String()] = true
		}
	}

	var routes []RouteStatus
	for _, p := range prefs.AdvertiseRoutes {
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

	writeJSON(w, http.StatusOK, routesResponse{
		Routes:   routes,
		ExitNode: isExit,
	})
}

func (s *Server) handleSetRoutes(w http.ResponseWriter, r *http.Request) {
	var req setRoutesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	prefixes := make([]netip.Prefix, 0, len(req.Routes))
	for _, cidr := range req.Routes {
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid CIDR: %s", cidr))
			return
		}
		prefixes = append(prefixes, p.Masked())
	}

	var warning string
	if req.ExitNode {
		s.refreshVPNClients()
		s.vpnClientsMu.Lock()
		activeClients := s.deviceInfo.ActiveVPNClients
		s.vpnClientsMu.Unlock()
		if len(activeClients) > 0 {
			ifaces := strings.Join(activeClients, ", ")
			warning = fmt.Sprintf(
				"Advertising is safe, but don't route this device's own traffic through a remote exit node — "+
					"Tailscale ip rules have higher priority and would override %s routing.", ifaces)
		}
		prefixes = append(prefixes,
			netip.MustParsePrefix("0.0.0.0/0"),
			netip.MustParsePrefix("::/0"))
	}

	_, err := s.lc.EditPrefs(r.Context(), &ipn.MaskedPrefs{
		Prefs:              ipn.Prefs{AdvertiseRoutes: prefixes},
		AdvertiseRoutesSet: true,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, humanizeLocalAPIError(err))
		return
	}

	writeJSON(w, http.StatusOK, setRoutesResponse{
		OK:       true,
		Message:  "Routes applied locally. Approve in Tailscale admin console.",
		AdminURL: "https://login.tailscale.com/admin/machines",
		Warning:  warning,
	})
}

func (s *Server) handleAuthKey(w http.ResponseWriter, r *http.Request) {
	if !s.integrationReady() {
		writeError(w, http.StatusPreconditionFailed, "Integration API key required before activating Tailscale")
		return
	}

	var req authKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.AuthKey == "" {
		writeError(w, http.StatusBadRequest, "auth key is required")
		return
	}
	if !strings.HasPrefix(req.AuthKey, "tskey-") {
		writeError(w, http.StatusBadRequest, "auth key must start with 'tskey-' prefix")
		return
	}

	if err := s.disableCorpDNS(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, humanizeLocalAPIError(err))
		return
	}
	err := s.lc.Start(r.Context(), ipn.Options{AuthKey: req.AuthKey})
	if err != nil {
		writeError(w, http.StatusInternalServerError, humanizeLocalAPIError(err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleGetSubnets(w http.ResponseWriter, r *http.Request) {
	parsed := wgs2s.ParseLocalSubnets()
	subnets := make([]subnetEntry, len(parsed))
	for i, sub := range parsed {
		subnets[i] = subnetEntry{CIDR: sub.CIDR, Name: sub.Name, Type: sub.Type}
	}
	writeJSON(w, http.StatusOK, subnetsResponse{Subnets: subnets})
}

func (s *Server) handleFirewallStatus(w http.ResponseWriter, r *http.Request) {
	forward, input, output, ipset := false, false, false, false
	if s.fw != nil {
		forward, input, output, ipset = s.fw.CheckTailscaleRulesPresent()
	}

	udapiReachable := isUDAPIReachable()

	var lastRestore *time.Time
	if p := s.lastRestore.Load(); p != nil {
		t := *p
		lastRestore = &t
	}

	chainPrefix := "VPN"
	if s.manifest != nil {
		chainPrefix = s.manifest.GetTailscaleChainPrefix()
	}

	writeJSON(w, http.StatusOK, firewallStatusResponse{
		IntegrationAPI: s.ic != nil && s.ic.HasAPIKey(),
		ChainPrefix:    chainPrefix,
		WatcherRunning: s.watcherRunning.Load(),
		LastRestore:    lastRestore,
		RulesPresent: map[string]bool{
			"forward": forward,
			"input":   input,
			"output":  output,
			"ipset":   ipset,
		},
		UDAPIReachable: udapiReachable,
	})
}
