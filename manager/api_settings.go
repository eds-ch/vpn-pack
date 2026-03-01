package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"

	"tailscale.com/ipn"
)

type settingsFields struct {
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

type settingsResponse struct {
	settingsFields
	ControlURL string `json:"controlURL"`
}

type settingsRequest struct {
	Hostname             *string    `json:"hostname,omitempty"`
	AcceptDNS            *bool      `json:"acceptDNS,omitempty"`
	AcceptRoutes         *bool      `json:"acceptRoutes,omitempty"`
	ShieldsUp            *bool      `json:"shieldsUp,omitempty"`
	RunSSH               *bool      `json:"runSSH,omitempty"`
	ControlURL           *string    `json:"controlURL,omitempty"`
	NoSNAT               *bool      `json:"noSNAT,omitempty"`
	UDPPort              *int       `json:"udpPort,omitempty"`
	RelayServerPort      *int       `json:"relayServerPort"`
	RelayServerEndpoints *string    `json:"relayServerEndpoints,omitempty"`
	AdvertiseTags        *[]string  `json:"advertiseTags,omitempty"`
}

func toSettingsResponse(prefs *ipn.Prefs) settingsResponse {
	return settingsResponse{
		settingsFields: settingsFields{
			Hostname:             prefs.Hostname,
			AcceptDNS:            prefs.CorpDNS,
			AcceptRoutes:         prefs.RouteAll,
			ShieldsUp:            prefs.ShieldsUp,
			RunSSH:               prefs.RunSSH,
			NoSNAT:               prefs.NoSNAT,
			UDPPort:              readTailscaledPort(),
			RelayServerPort:      prefs.RelayServerPort,
			RelayServerEndpoints: formatAddrPorts(prefs.RelayServerStaticEndpoints),
			AdvertiseTags:        prefs.AdvertiseTags,
		},
		ControlURL: prefs.ControlURL,
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.lc.GetPrefs(r.Context())
	if err != nil {
		writeError(w, http.StatusBadGateway, humanizeLocalAPIError(err))
		return
	}

	writeJSON(w, http.StatusOK, toSettingsResponse(prefs))
}

func (s *Server) handleSetSettings(w http.ResponseWriter, r *http.Request) {
	var req settingsRequest
	if err := readJSON(w, r, &req); err != nil {
		return
	}

	relayEndpoints, err := s.validateSettingsRequest(r.Context(), &req)
	if err != nil {
		writeAPIError(w, err)
		return
	}

	var oldRelayPort *uint16
	if req.RelayServerPort != nil && s.deviceInfo.HasUDAPISocket {
		prefs, err := s.lc.GetPrefs(r.Context())
		if err == nil {
			oldRelayPort = prefs.RelayServerPort
		}
	}

	var oldControlURL string
	if req.ControlURL != nil {
		if prefs, err := s.lc.GetPrefs(r.Context()); err == nil {
			oldControlURL = prefs.ControlURL
		}
	}

	mp := buildMaskedPrefs(&req, relayEndpoints)

	updated, err := s.lc.EditPrefs(r.Context(), mp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, humanizeLocalAPIError(err))
		return
	}

	var needsRestart bool
	if req.ControlURL != nil && *req.ControlURL != oldControlURL {
		needsRestart = true
	}

	portRestart, err := s.applyUDPPortChange(req.UDPPort)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	if portRestart {
		needsRestart = true
	}

	s.updateRelayPortRules(req.RelayServerPort, oldRelayPort)
	s.updateTailscaleWgPortRules(req.UDPPort)

	if needsRestart {
		go func() {
			if out, err := exec.Command("systemctl", "restart", "tailscaled").CombinedOutput(); err != nil {
				slog.Warn("tailscaled restart failed", "err", err, "output", string(out))
			} else {
				slog.Info("tailscaled restarted for settings change")
			}
		}()
	}

	writeJSON(w, http.StatusOK, toSettingsResponse(updated))
}

func (s *Server) validateSettingsRequest(ctx context.Context, req *settingsRequest) ([]netip.AddrPort, error) {
	if req.AcceptDNS != nil && *req.AcceptDNS {
		return nil, &apiError{http.StatusBadRequest,
			"AcceptDNS (MagicDNS) cannot be enabled on UniFi gateways: " +
				"it overwrites /etc/resolv.conf, breaking local DNS and UniFi DNS redirect rules"}
	}

	if req.ControlURL != nil {
		if err := validateControlURL(*req.ControlURL); err != nil {
			return nil, &apiError{http.StatusBadRequest, err.Error()}
		}
		prefs, err := s.lc.GetPrefs(ctx)
		if err != nil {
			return nil, &apiError{http.StatusBadGateway, humanizeLocalAPIError(err)}
		}
		if *req.ControlURL != prefs.ControlURL {
			st, err := s.lc.Status(ctx)
			if err != nil {
				return nil, &apiError{http.StatusBadGateway, humanizeLocalAPIError(err)}
			}
			if st.BackendState == "Running" {
				return nil, &apiError{http.StatusBadRequest, "Must log out before changing control server URL"}
			}
		}
	}

	if req.UDPPort != nil {
		port := *req.UDPPort
		if port < 1 || port > 65535 {
			return nil, &apiError{http.StatusBadRequest, "UDP port must be between 1 and 65535"}
		}
	}

	if req.RelayServerPort != nil {
		port := *req.RelayServerPort
		if port < -1 || port > 65535 {
			return nil, &apiError{http.StatusBadRequest, "Relay server port must be between 0 and 65535, or -1 to disable"}
		}
	}

	if req.AdvertiseTags != nil {
		for _, tag := range *req.AdvertiseTags {
			if err := validateTag(tag); err != nil {
				return nil, &apiError{http.StatusBadRequest, fmt.Sprintf("Invalid tag %q: %v", tag, err)}
			}
		}
	}

	var relayEndpoints []netip.AddrPort
	if req.RelayServerEndpoints != nil && *req.RelayServerEndpoints != "" {
		var err error
		relayEndpoints, err = parseAddrPorts(*req.RelayServerEndpoints)
		if err != nil {
			return nil, &apiError{http.StatusBadRequest, fmt.Sprintf("Invalid relay server endpoints: %v", err)}
		}
	}

	return relayEndpoints, nil
}

func validateTag(tag string) error {
	name, ok := strings.CutPrefix(tag, "tag:")
	if !ok {
		return fmt.Errorf("must start with 'tag:'")
	}
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if !isAlpha(name[0]) {
		return fmt.Errorf("name must start with a letter")
	}
	for _, b := range []byte(name) {
		if !isAlpha(b) && !isNum(b) && b != '-' {
			return fmt.Errorf("name can only contain letters, numbers, or dashes")
		}
	}
	return nil
}

func isAlpha(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isNum(b byte) bool   { return b >= '0' && b <= '9' }

func buildMaskedPrefs(req *settingsRequest, relayEndpoints []netip.AddrPort) *ipn.MaskedPrefs {
	mp := &ipn.MaskedPrefs{}
	if req.Hostname != nil {
		mp.Hostname = *req.Hostname
		mp.HostnameSet = true
	}
	if req.AcceptDNS != nil {
		mp.CorpDNS = *req.AcceptDNS
		mp.CorpDNSSet = true
	}
	if req.AcceptRoutes != nil {
		mp.RouteAll = *req.AcceptRoutes
		mp.RouteAllSet = true
	}
	if req.ShieldsUp != nil {
		mp.ShieldsUp = *req.ShieldsUp
		mp.ShieldsUpSet = true
	}
	if req.RunSSH != nil {
		mp.RunSSH = *req.RunSSH
		mp.RunSSHSet = true
	}
	if req.ControlURL != nil {
		mp.ControlURL = *req.ControlURL
		mp.ControlURLSet = true
	}
	if req.NoSNAT != nil {
		mp.NoSNAT = *req.NoSNAT
		mp.NoSNATSet = true
	}
	if req.RelayServerPort != nil {
		port := *req.RelayServerPort
		if port < 0 {
			mp.RelayServerPort = nil
		} else {
			p := uint16(port)
			mp.RelayServerPort = &p
		}
		mp.RelayServerPortSet = true
	}
	if req.RelayServerEndpoints != nil {
		mp.RelayServerStaticEndpoints = relayEndpoints
		mp.RelayServerStaticEndpointsSet = true
	}
	if req.AdvertiseTags != nil {
		mp.AdvertiseTags = *req.AdvertiseTags
		mp.AdvertiseTagsSet = true
	}
	return mp
}

func (s *Server) applyUDPPortChange(newPort *int) (bool, error) {
	if newPort == nil {
		return false, nil
	}
	currentPort := readTailscaledPort()
	if *newPort == currentPort {
		return false, nil
	}
	if err := writeTailscaledPort(*newPort); err != nil {
		slog.Warn("failed to write tailscaled port", "err", err)
		return false, &apiError{http.StatusInternalServerError, "Failed to update UDP port configuration"}
	}
	return true, nil
}

func (s *Server) updateRelayPortRules(newRelayPort *int, oldRelayPort *uint16) {
	if newRelayPort == nil || s.ic == nil || !s.ic.HasAPIKey() {
		return
	}
	var changed bool
	if oldRelayPort != nil && *oldRelayPort > 0 {
		if err := s.fw.CloseWanPort(int(*oldRelayPort), wanMarkerRelay); err != nil {
			slog.Warn("relay WAN port close failed", "port", *oldRelayPort, "err", err)
		} else {
			changed = true
		}
	}
	if *newRelayPort > 0 {
		if err := s.fw.OpenWanPort(*newRelayPort, wanMarkerRelay); err != nil {
			slog.Warn("relay WAN port open failed", "port", *newRelayPort, "err", err)
		} else {
			changed = true
		}
	}
	if changed {
		s.schedulePostPolicyRestore()
	}
}

func (s *Server) updateTailscaleWgPortRules(newPort *int) {
	if newPort == nil || s.ic == nil || !s.ic.HasAPIKey() {
		return
	}
	entry, _ := s.manifest.GetWanPortEntry(wanMarkerTailscaleWG)
	oldPort := entry.Port
	if oldPort == *newPort {
		return
	}
	var changed bool
	if oldPort > 0 {
		if err := s.fw.CloseWanPort(oldPort, wanMarkerTailscaleWG); err != nil {
			slog.Warn("tailscale WG WAN port close failed", "port", oldPort, "err", err)
		} else {
			changed = true
		}
	}
	if *newPort > 0 {
		if err := s.fw.OpenWanPort(*newPort, wanMarkerTailscaleWG); err != nil {
			slog.Warn("tailscale WG WAN port open failed", "port", *newPort, "err", err)
		} else {
			changed = true
		}
	}
	if changed {
		s.schedulePostPolicyRestore()
	}
}

func validateControlURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("control server URL must use HTTPS scheme")
	}
	if u.Host == "" {
		return fmt.Errorf("control server URL must have a host")
	}
	return nil
}

var portRe = regexp.MustCompile(`(?m)^PORT="(\d+)"`)

var cachedTailscaledPort atomic.Pointer[int]

func readTailscaledPort() int {
	if p := cachedTailscaledPort.Load(); p != nil {
		return *p
	}
	port := readPortFromFile()
	cachedTailscaledPort.Store(&port)
	return port
}

func readPortFromFile() int {
	data, err := os.ReadFile(tailscaledDefaultsPath)
	if err != nil {
		return defaultTailscalePort
	}
	m := portRe.FindSubmatch(data)
	if m == nil {
		return defaultTailscalePort
	}
	port, err := strconv.Atoi(string(m[1]))
	if err != nil || port < 1 || port > 65535 {
		return defaultTailscalePort
	}
	return port
}

func writeTailscaledPort(port int) error {
	data, err := os.ReadFile(tailscaledDefaultsPath)
	if err != nil {
		data = []byte(fmt.Sprintf("PORT=\"%d\"\nFLAGS=\"\"\n", port))
		if err := os.WriteFile(tailscaledDefaultsPath, data, configPerm); err != nil {
			return err
		}
		cachedTailscaledPort.Store(&port)
		return nil
	}
	content := string(data)
	newLine := fmt.Sprintf("PORT=\"%d\"", port)
	if portRe.MatchString(content) {
		content = portRe.ReplaceAllString(content, newLine)
	} else {
		content = newLine + "\n" + strings.TrimRight(content, "\n") + "\n"
	}
	if err := os.WriteFile(tailscaledDefaultsPath, []byte(content), configPerm); err != nil {
		return err
	}
	cachedTailscaledPort.Store(&port)
	return nil
}

func formatAddrPorts(addrs []netip.AddrPort) string {
	if len(addrs) == 0 {
		return ""
	}
	parts := make([]string, len(addrs))
	for i, ap := range addrs {
		parts[i] = ap.String()
	}
	return strings.Join(parts, ", ")
}

func parseAddrPorts(s string) ([]netip.AddrPort, error) {
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	result := make([]netip.AddrPort, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		ap, err := netip.ParseAddrPort(p)
		if err != nil {
			return nil, fmt.Errorf("invalid endpoint %q: %w", p, err)
		}
		result = append(result, ap)
	}
	return result, nil
}
