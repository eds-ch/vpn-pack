package main

import (
	"context"
	"encoding/base64"
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

func (s *Server) enrichForwardINOk(ctx context.Context, statuses []wgs2s.WgS2sStatus) {
	if s.fw == nil {
		return
	}
	var ifaces []string
	for _, st := range statuses {
		if st.Enabled {
			ifaces = append(ifaces, st.InterfaceName)
		}
	}
	fwPresent := s.fw.CheckWgS2sRulesPresent(ctx, ifaces)
	for i := range statuses {
		statuses[i].ForwardINOk = fwPresent[statuses[i].InterfaceName]
	}
}

func (s *Server) teardownTunnelFirewall(ctx context.Context, t *wgs2s.TunnelConfig) {
	if s.fw == nil || t == nil {
		return
	}
	s.fw.RemoveWgS2sFirewall(ctx, t.ID, t.InterfaceName, t.AllowedIPs)
	s.closeWgS2sWanPort(ctx, t.ListenPort, t.InterfaceName)
}

func validateTunnelSubnets(allowedIPs []string, excludeIfaces ...string) (warnings, blocks []SubnetConflict) {
	sys, err := CollectSystemSubnets(excludeIfaces...)
	if err != nil {
		slog.Warn("subnet collection failed, skipping validation", "err", err)
		return nil, nil
	}
	vr := ValidateAllowedIPs(allowedIPs, sys)
	return vr.Warnings, vr.Blocked
}

type wgS2sTunnelResponse struct {
	wgs2s.TunnelConfig
	PublicKey string              `json:"publicKey,omitempty"`
	Status    *wgs2s.WgS2sStatus `json:"status,omitempty"`
	ZoneID    string              `json:"zoneId,omitempty"`
	ZoneName  string              `json:"zoneName,omitempty"`
	Warnings  []SubnetConflict    `json:"warnings,omitempty"`
}

type wgS2sCreateRequest struct {
	wgs2s.TunnelConfig
	PrivateKey string `json:"privateKey,omitempty"`
	ZoneID     string `json:"zoneId,omitempty"`
	ZoneName   string `json:"zoneName,omitempty"`
	CreateZone bool   `json:"createZone,omitempty"`
}

func (s *Server) handleWgS2sListTunnels(w http.ResponseWriter, r *http.Request) {
	if !s.wgManagerOrError(w) {
		return
	}

	tunnels := s.wgManager.GetTunnels()
	statuses := s.wgManager.GetStatuses()
	s.enrichForwardINOk(r.Context(), statuses)

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
		if zm, ok := s.manifest.GetWgS2sZone(t.ID); ok {
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
	if err := readJSON(w, r, &req); err != nil {
		return
	}

	if err := validateWgS2sCreateRequest(&req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	warnings, blocks := validateTunnelSubnets(req.AllowedIPs)
	if len(blocks) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":     fmt.Sprintf("Subnet conflict: %s", blocks[0].Message),
			"conflicts": blocks,
		})
		return
	}

	tunnel, err := s.wgManager.CreateTunnel(req.TunnelConfig, req.PrivateKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, humanizeWgS2sError(err))
		return
	}

	zoneResult := s.setupTunnelZone(r.Context(), tunnel.ID, req.CreateZone, req.ZoneID, req.ZoneName)

	fwErr := s.fw.SetupWgS2sFirewall(r.Context(), tunnel.ID, tunnel.InterfaceName, tunnel.AllowedIPs)
	if fwErr != nil {
		slog.Warn("wg-s2s firewall rules failed", "iface", tunnel.InterfaceName, "err", fwErr)
		s.logBuf.Add(newLogEntry("warn", fmt.Sprintf("firewall rules failed iface=%s err=%v", tunnel.InterfaceName, fwErr), "wgs2s"))
	}
	s.openWgS2sWanPort(r.Context(), tunnel.ListenPort, tunnel.InterfaceName)

	tunnelResp := wgS2sTunnelResponse{TunnelConfig: *tunnel, Warnings: warnings}
	if pubKey, err := s.wgManager.GetPublicKey(tunnel.ID); err == nil {
		tunnelResp.PublicKey = pubKey
	}
	if zm, ok := s.manifest.GetWgS2sZone(tunnel.ID); ok {
		tunnelResp.ZoneID = zm.ZoneID
		tunnelResp.ZoneName = zm.ZoneName
	}

	resp := TunnelCreateResponse{wgS2sTunnelResponse: tunnelResp}
	resp.Status = firewallStatus(zoneResult, fwErr)
	if resp.Status == "partial" {
		resp.Firewall = NewFirewallStatusBrief(zoneResult)
		if resp.Firewall == nil {
			resp.Firewall = &FirewallStatusBrief{}
		}
		if fwErr != nil {
			resp.Firewall.Errors = append(resp.Firewall.Errors, fwErr.Error())
		}
	}

	writeJSON(w, http.StatusCreated, resp)
}

func validateWgS2sCreateRequest(req *wgS2sCreateRequest) error {
	cfg := req.TunnelConfig
	if cfg.Name == "" {
		return fmt.Errorf("name is required")
	}
	if cfg.ListenPort < 1 || cfg.ListenPort > maxPort {
		return fmt.Errorf("listenPort must be between 1 and 65535")
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
	for _, cidr := range cfg.LocalSubnets {
		if err := validateCIDR(cidr); err != nil {
			return fmt.Errorf("invalid localSubnet %q: %s", cidr, err)
		}
	}
	return nil
}


func validateWgS2sUpdateRequest(updates *wgs2s.TunnelConfig) error {
	if updates.ListenPort < 0 || updates.ListenPort > maxPort {
		return fmt.Errorf("listenPort must be between 0 and 65535")
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
	for _, cidr := range updates.LocalSubnets {
		if err := validateCIDR(cidr); err != nil {
			return fmt.Errorf("invalid localSubnet %q: %s", cidr, err)
		}
	}
	return nil
}
func (s *Server) setupTunnelZone(ctx context.Context, tunnelID string, createZone bool, zoneID, zoneName string) *FirewallSetupResult {
	if !s.integrationReady() {
		return nil
	}
	var result *FirewallSetupResult
	switch {
	case createZone:
		result = s.fw.SetupWgS2sZone(ctx, tunnelID, "", zoneName)
	case zoneID != "":
		result = s.fw.SetupWgS2sZone(ctx, tunnelID, zoneID, "")
	case len(s.manifest.GetWgS2sZones()) == 0:
		result = s.fw.SetupWgS2sZone(ctx, tunnelID, "", "WireGuard S2S")
	}
	if result != nil && result.Err() != nil {
		slog.Warn("wg-s2s zone setup failed", "err", result.Err())
	}
	return result
}

func (s *Server) handleWgS2sUpdateTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgManagerOrError(w) {
		return
	}

	id := r.PathValue("id")

	var updates wgs2s.TunnelConfig
	if err := readJSON(w, r, &updates); err != nil {
		return
	}

	if err := validateWgS2sUpdateRequest(&updates); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	existing := s.findTunnelByID(id)

	var warnings []SubnetConflict
	if updates.AllowedIPs != nil && existing != nil {
		var blocks []SubnetConflict
		warnings, blocks = validateTunnelSubnets(updates.AllowedIPs, existing.InterfaceName)
		if len(blocks) > 0 {
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":     fmt.Sprintf("Subnet conflict: %s", blocks[0].Message),
				"conflicts": blocks,
			})
			return
		}
	}

	tunnel, err := s.wgManager.UpdateTunnel(id, updates)
	if err != nil {
		writeError(w, http.StatusInternalServerError, humanizeWgS2sError(err))
		return
	}

	var fwErr error
	if updates.AllowedIPs != nil && tunnel.Enabled && existing != nil {
		s.fw.RemoveWgS2sIPSetEntries(r.Context(), id, existing.AllowedIPs)
		fwErr = s.fw.SetupWgS2sFirewall(r.Context(), tunnel.ID, tunnel.InterfaceName, tunnel.AllowedIPs)
		if fwErr != nil {
			slog.Warn("wg-s2s firewall rules failed", "iface", tunnel.InterfaceName, "err", fwErr)
			s.logBuf.Add(newLogEntry("warn", fmt.Sprintf("firewall rules failed iface=%s err=%v", tunnel.InterfaceName, fwErr), "wgs2s"))
		}
	}

	tunnelResp := wgS2sTunnelResponse{TunnelConfig: *tunnel, Warnings: warnings}
	if pubKey, err := s.wgManager.GetPublicKey(tunnel.ID); err == nil {
		tunnelResp.PublicKey = pubKey
	}

	resp := TunnelCreateResponse{wgS2sTunnelResponse: tunnelResp}
	resp.Status = firewallStatus(nil, fwErr)
	if resp.Status == "partial" {
		resp.Firewall = &FirewallStatusBrief{Errors: []string{fwErr.Error()}}
	}

	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleWgS2sDeleteTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgManagerOrError(w) {
		return
	}

	id := r.PathValue("id")
	t := s.findTunnelByID(id)
	if t == nil {
		writeError(w, http.StatusNotFound, "tunnel not found")
		return
	}

	if err := s.wgManager.DeleteTunnel(id); err != nil {
		writeError(w, http.StatusInternalServerError, humanizeWgS2sError(err))
		return
	}

	s.teardownTunnelFirewall(r.Context(), t)

	if err := s.manifest.RemoveWgS2sTunnel(id); err != nil {
		slog.Warn("manifest save failed", "err", err)
	}

	writeOK(w)
}

func (s *Server) handleWgS2sEnableTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgManagerOrError(w) {
		return
	}

	id := r.PathValue("id")
	if s.findTunnelByID(id) == nil {
		writeError(w, http.StatusNotFound, "tunnel not found")
		return
	}
	if err := s.wgManager.EnableTunnel(id); err != nil {
		writeError(w, http.StatusInternalServerError, humanizeWgS2sError(err))
		return
	}

	if t := s.findTunnelByID(id); t != nil {
		fwErr := s.fw.SetupWgS2sFirewall(r.Context(), t.ID, t.InterfaceName, t.AllowedIPs)
		if fwErr != nil {
			slog.Warn("wg-s2s firewall rules failed", "iface", t.InterfaceName, "err", fwErr)
			s.logBuf.Add(newLogEntry("warn", fmt.Sprintf("firewall rules failed iface=%s err=%v", t.InterfaceName, fwErr), "wgs2s"))
		}
		s.openWgS2sWanPort(r.Context(), t.ListenPort, t.InterfaceName)
		if fwErr != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "partial", "firewall": &FirewallStatusBrief{Errors: []string{fwErr.Error()}}})
			return
		}
	}

	writeOK(w)
}

func (s *Server) handleWgS2sDisableTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgManagerOrError(w) {
		return
	}

	id := r.PathValue("id")
	t := s.findTunnelByID(id)
	if t == nil {
		writeError(w, http.StatusNotFound, "tunnel not found")
		return
	}

	if err := s.wgManager.DisableTunnel(id); err != nil {
		writeError(w, http.StatusInternalServerError, humanizeWgS2sError(err))
		return
	}

	s.teardownTunnelFirewall(r.Context(), t)

	writeOK(w)
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

	wanIP := getWanIP()

	var allowedIPs []string
	if len(tunnel.LocalSubnets) > 0 {
		allowedIPs = append(allowedIPs, tunnel.LocalSubnets...)
	} else {
		for _, sub := range parseLocalSubnets() {
			allowedIPs = append(allowedIPs, sub.CIDR)
		}
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
	writeJSON(w, http.StatusOK, map[string]string{"ip": getWanIP()})
}

func (s *Server) handleWgS2sLocalSubnets(w http.ResponseWriter, r *http.Request) {
	subnets := parseLocalSubnets()
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
