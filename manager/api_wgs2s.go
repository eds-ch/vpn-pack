package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
	"unifi-tailscale/manager/internal/wgs2s"
)

func (s *Server) findTunnelByID(id string) *wgs2s.TunnelConfig {
	tunnels := s.wgManager.GetTunnels()
	for i := range tunnels {
		if tunnels[i].ID == id {
			return &tunnels[i]
		}
	}
	return nil
}

func (s *Server) wgManagerOrError(w http.ResponseWriter) bool {
	if s.wgManager == nil {
		writeError(w, http.StatusServiceUnavailable, "WG S2S manager not initialized")
		return false
	}
	return true
}

type wgS2sTunnelResponse struct {
	wgs2s.TunnelConfig
	PublicKey string              `json:"publicKey,omitempty"`
	Status    *wgs2s.WgS2sStatus `json:"status,omitempty"`
	ZoneID    string              `json:"zoneId,omitempty"`
	ZoneName  string              `json:"zoneName,omitempty"`
}

type wgS2sCreateRequest struct {
	wgs2s.TunnelConfig
	PrivateKey string `json:"privateKey,omitempty"`
	ZoneID     string `json:"zoneId,omitempty"`
	ZoneName   string `json:"zoneName,omitempty"`
}

func (s *Server) handleWgS2sListTunnels(w http.ResponseWriter, r *http.Request) {
	if s.wgManager == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	tunnels := s.wgManager.GetTunnels()
	statuses := s.wgManager.GetStatuses()

	statusMap := make(map[string]*wgs2s.WgS2sStatus, len(statuses))
	for i := range statuses {
		statusMap[statuses[i].ID] = &statuses[i]
	}

	result := make([]wgS2sTunnelResponse, 0, len(tunnels))
	for _, t := range tunnels {
		resp := wgS2sTunnelResponse{TunnelConfig: t}
		if st, ok := statusMap[t.ID]; ok {
			resp.Status = st
		}
		if pubKey, err := s.wgManager.GetPublicKey(t.ID); err == nil {
			resp.PublicKey = pubKey
		}
		if zm, ok := s.manifest.WgS2s[t.ID]; ok {
			resp.ZoneID = zm.ZoneID
			resp.ZoneName = zm.ZoneName
		}
		result = append(result, resp)
	}

	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleWgS2sCreateTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgManagerOrError(w) {
		return
	}

	var req wgS2sCreateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validateWgS2sCreateRequest(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tunnel, err := s.wgManager.CreateTunnel(req.TunnelConfig, req.PrivateKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, humanizeWgS2sError(err))
		return
	}

	s.setupTunnelZone(tunnel.ID, req.ZoneID, req.ZoneName)

	s.sendFirewallRequest(FirewallRequest{Action: "apply-wg-s2s", TunnelID: tunnel.ID, Interface: tunnel.InterfaceName})
	if s.fw != nil {
		if err := s.fw.OpenWanPort(tunnel.ListenPort, "wg-s2s:"+tunnel.InterfaceName); err != nil {
			slog.Warn("wg-s2s WAN port open failed", "port", tunnel.ListenPort, "err", err)
		}
	}

	resp := wgS2sTunnelResponse{TunnelConfig: *tunnel}
	if pubKey, err := s.wgManager.GetPublicKey(tunnel.ID); err == nil {
		resp.PublicKey = pubKey
	}
	if zm, ok := s.manifest.WgS2s[tunnel.ID]; ok {
		resp.ZoneID = zm.ZoneID
		resp.ZoneName = zm.ZoneName
	}

	writeJSON(w, http.StatusCreated, resp)
}

func validateWgS2sCreateRequest(req *wgS2sCreateRequest) error {
	cfg := req.TunnelConfig
	if cfg.Name == "" {
		return fmt.Errorf("name is required")
	}
	if cfg.ListenPort <= 0 {
		return fmt.Errorf("listenPort must be positive")
	}
	if err := validateCIDR(cfg.TunnelAddress); err != nil {
		return fmt.Errorf("invalid tunnelAddress: %s", err)
	}
	if err := validateBase64Key(cfg.PeerPublicKey); err != nil {
		return fmt.Errorf("invalid peerPublicKey: %s", err)
	}
	for _, cidr := range cfg.AllowedIPs {
		if err := validateCIDR(cidr); err != nil {
			return fmt.Errorf("invalid allowedIP %q: %s", cidr, err)
		}
	}
	return nil
}


func validateWgS2sUpdateRequest(updates *wgs2s.TunnelConfig) error {
	if updates.ListenPort < 0 {
		return fmt.Errorf("listenPort must be non-negative")
	}
	if updates.TunnelAddress != "" {
		if err := validateCIDR(updates.TunnelAddress); err != nil {
			return fmt.Errorf("invalid tunnelAddress: %s", err)
		}
	}
	if updates.PeerPublicKey != "" {
		if err := validateBase64Key(updates.PeerPublicKey); err != nil {
			return fmt.Errorf("invalid peerPublicKey: %s", err)
		}
	}
	for _, cidr := range updates.AllowedIPs {
		if err := validateCIDR(cidr); err != nil {
			return fmt.Errorf("invalid allowedIP %q: %s", cidr, err)
		}
	}
	return nil
}
func (s *Server) setupTunnelZone(tunnelID, reqZoneID, reqZoneName string) {
	if !s.integrationReady() {
		return
	}
	existingZones := s.manifest.GetWgS2sZones()
	switch {
	case reqZoneID == "new":
		if err := s.fw.SetupWgS2sZone(tunnelID, "new", reqZoneName); err != nil {
			slog.Warn("wg-s2s zone setup failed", "err", err)
		}
	case reqZoneID != "":
		if err := s.fw.SetupWgS2sZone(tunnelID, reqZoneID, ""); err != nil {
			slog.Warn("wg-s2s zone assignment failed", "err", err)
		}
	case len(existingZones) == 0:
		if err := s.fw.SetupWgS2sZone(tunnelID, "new", "WireGuard S2S"); err != nil {
			slog.Warn("wg-s2s auto zone setup failed", "err", err)
		}
	}
}

func (s *Server) handleWgS2sUpdateTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgManagerOrError(w) {
		return
	}

	id := r.PathValue("id")

	var updates wgs2s.TunnelConfig
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := validateWgS2sUpdateRequest(&updates); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tunnel, err := s.wgManager.UpdateTunnel(id, updates)
	if err != nil {
		writeError(w, http.StatusInternalServerError, humanizeWgS2sError(err))
		return
	}

	resp := wgS2sTunnelResponse{TunnelConfig: *tunnel}
	if pubKey, err := s.wgManager.GetPublicKey(tunnel.ID); err == nil {
		resp.PublicKey = pubKey
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleWgS2sDeleteTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgManagerOrError(w) {
		return
	}

	id := r.PathValue("id")
	t := s.findTunnelByID(id)

	if err := s.wgManager.DeleteTunnel(id); err != nil {
		writeError(w, http.StatusInternalServerError, humanizeWgS2sError(err))
		return
	}

	if s.fw != nil && t != nil {
		s.fw.RemoveWgS2sFirewall(t.InterfaceName)
		if t.ListenPort > 0 {
			if err := s.fw.CloseWanPort(t.ListenPort, "wg-s2s:"+t.InterfaceName); err != nil {
				slog.Warn("wg-s2s WAN port close failed", "port", t.ListenPort, "err", err)
			}
		}
	}

	s.manifest.RemoveWgS2sTunnel(id)
	if err := s.manifest.Save(); err != nil {
		slog.Warn("manifest save failed", "err", err)
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleWgS2sEnableTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgManagerOrError(w) {
		return
	}

	id := r.PathValue("id")
	if err := s.wgManager.EnableTunnel(id); err != nil {
		writeError(w, http.StatusInternalServerError, humanizeWgS2sError(err))
		return
	}

	if t := s.findTunnelByID(id); t != nil {
		s.sendFirewallRequest(FirewallRequest{Action: "apply-wg-s2s", TunnelID: t.ID, Interface: t.InterfaceName})
		if s.fw != nil {
			if err := s.fw.OpenWanPort(t.ListenPort, "wg-s2s:"+t.InterfaceName); err != nil {
				slog.Warn("wg-s2s WAN port open failed", "port", t.ListenPort, "err", err)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleWgS2sDisableTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgManagerOrError(w) {
		return
	}

	id := r.PathValue("id")
	t := s.findTunnelByID(id)

	if err := s.wgManager.DisableTunnel(id); err != nil {
		writeError(w, http.StatusInternalServerError, humanizeWgS2sError(err))
		return
	}

	if s.fw != nil && t != nil {
		s.fw.RemoveWgS2sFirewall(t.InterfaceName)
		if t.ListenPort > 0 {
			if err := s.fw.CloseWanPort(t.ListenPort, "wg-s2s:"+t.InterfaceName); err != nil {
				slog.Warn("wg-s2s WAN port close failed", "port", t.ListenPort, "err", err)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleWgS2sGenerateKeypair(w http.ResponseWriter, r *http.Request) {
	privKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate keypair")
		return
	}

	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{
		"publicKey":  privKey.PublicKey().String(),
		"privateKey": privKey.String(),
	})
}

func (s *Server) handleWgS2sGetConfig(w http.ResponseWriter, r *http.Request) {
	if !s.wgManagerOrError(w) {
		return
	}

	id := r.PathValue("id")
	tunnel := s.findTunnelByID(id)
	if tunnel == nil {
		writeError(w, http.StatusNotFound, "tunnel not found")
		return
	}

	pubKey, err := s.wgManager.GetPublicKey(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read public key")
		return
	}

	wanIP := s.wgManager.GetWanIP()
	localSubnets := wgs2s.ParseLocalSubnets()

	var allowedIPs []string
	for _, sub := range localSubnets {
		allowedIPs = append(allowedIPs, sub.CIDR)
	}
	if tunnel.TunnelAddress != "" {
		ip, _, err := net.ParseCIDR(tunnel.TunnelAddress)
		if err == nil {
			allowedIPs = append(allowedIPs, ip.String()+"/32")
		}
	}

	var b strings.Builder
	b.WriteString("[Interface]\n")
	b.WriteString("PrivateKey = <PRIVATE_KEY>\n")
	b.WriteString("Address = <TUNNEL_ADDRESS>\n")
	fmt.Fprintf(&b, "ListenPort = %d\n", tunnel.ListenPort)
	b.WriteString("\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", pubKey)
	if wanIP != "" {
		fmt.Fprintf(&b, "Endpoint = %s:%d\n", wanIP, tunnel.ListenPort)
	}
	if len(allowedIPs) > 0 {
		fmt.Fprintf(&b, "AllowedIPs = %s\n", strings.Join(allowedIPs, ", "))
	}
	if tunnel.PersistentKeepalive > 0 {
		fmt.Fprintf(&b, "PersistentKeepalive = %d\n", tunnel.PersistentKeepalive)
	}

	writeJSON(w, http.StatusOK, map[string]string{"config": b.String()})
}

func (s *Server) handleWgS2sWanIP(w http.ResponseWriter, r *http.Request) {
	if s.wgManager == nil {
		writeJSON(w, http.StatusOK, map[string]string{"ip": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ip": s.wgManager.GetWanIP()})
}

func (s *Server) handleWgS2sLocalSubnets(w http.ResponseWriter, r *http.Request) {
	if s.wgManager == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	subnets := wgs2s.ParseLocalSubnets()
	if subnets == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, subnets)
}

func (s *Server) handleWgS2sListZones(w http.ResponseWriter, r *http.Request) {
	zones := s.manifest.GetWgS2sZones()
	if zones == nil {
		zones = []WgS2sZoneInfo{}
	}
	writeJSON(w, http.StatusOK, zones)
}

func validateCIDR(s string) error {
	_, _, err := net.ParseCIDR(s)
	if err != nil {
		return fmt.Errorf("not a valid CIDR notation")
	}
	return nil
}

func validateBase64Key(s string) error {
	if len(s) != wgKeyBase64Len {
		return fmt.Errorf("must be 44 characters (base64-encoded 32 bytes)")
	}
	decoded, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return fmt.Errorf("invalid base64 encoding")
	}
	if len(decoded) != wgKeyBytes {
		return fmt.Errorf("decoded key must be 32 bytes")
	}
	return nil
}

func humanizeWgS2sError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "tunnel") && strings.Contains(lower, "not found"):
		return "Tunnel not found"
	case strings.Contains(lower, "port in use") || strings.Contains(lower, "address already in use"):
		return "Port is already in use. Choose a different port."
	case strings.Contains(lower, "permission denied") || strings.Contains(lower, "operation not permitted"):
		return "Insufficient permissions for network operations. Ensure the manager runs as root."
	default:
		return fmt.Sprintf("WG S2S error: %s", msg)
	}
}
