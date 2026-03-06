package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"unifi-tailscale/manager/internal/wgs2s"
	"unifi-tailscale/manager/service"
)

func spaHandler() http.Handler {
	sub, _ := fs.Sub(uiFS, "ui/dist")
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			http.NotFound(w, r)
			return
		}

		path := r.URL.Path
		if path == "/" {
			path = "index.html"
		} else {
			path = strings.TrimPrefix(path, "/")
		}

		if _, err := fs.Stat(sub, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("writeJSON encode failed", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeOK(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, OperationResponse{OK: true})
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return err
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return err
	}
	return nil
}

func writeServiceError(w http.ResponseWriter, err error) {
	var se *service.Error
	if errors.As(err, &se) {
		switch se.Kind {
		case service.ErrValidation:
			writeError(w, http.StatusBadRequest, se.Message)
		case service.ErrUpstream:
			writeError(w, http.StatusBadGateway, se.Message)
		case service.ErrPrecondition:
			writeError(w, http.StatusPreconditionFailed, se.Message)
		case service.ErrNotFound:
			writeError(w, http.StatusNotFound, se.Message)
		case service.ErrUnavailable:
			writeError(w, http.StatusServiceUnavailable, se.Message)
		default:
			writeError(w, http.StatusInternalServerError, se.Message)
		}
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func writeWgS2sError(w http.ResponseWriter, err error) {
	var sce *service.SubnetConflictError
	if errors.As(err, &sce) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":     sce.Msg,
			"conflicts": sce.Conflicts,
		})
		return
	}
	writeServiceError(w, err)
}

func isUDAPIReachable() bool {
	_, err := os.Stat(udapiSocketPath)
	return err == nil
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.state.snapshot())
}

func (s *Server) handleDevice(w http.ResponseWriter, r *http.Request) {
	s.vpnClientsMu.Lock()
	info := s.deviceInfo
	s.vpnClientsMu.Unlock()
	var si syscall.Sysinfo_t
	if err := syscall.Sysinfo(&si); err == nil {
		info.Uptime = si.Uptime
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleUp(w http.ResponseWriter, r *http.Request) {
	if err := s.tailscaleSvc.Activate(r.Context()); err != nil {
		writeServiceError(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleDown(w http.ResponseWriter, r *http.Request) {
	if err := s.tailscaleSvc.Deactivate(r.Context()); err != nil {
		writeServiceError(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if err := s.tailscaleSvc.Login(r.Context()); err != nil {
		writeServiceError(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.tailscaleSvc.Logout(r.Context()); err != nil {
		writeServiceError(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	resp, err := s.settings.GetSettings(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSetSettings(w http.ResponseWriter, r *http.Request) {
	var req service.SettingsRequest
	if err := readJSON(w, r, &req); err != nil {
		return
	}
	result, err := s.settings.SetSettings(r.Context(), &req)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	if result.NeedsRestart {
		s.restartTailscaled()
	}
	if result.DNSChanged {
		s.state.mu.Lock()
		s.state.data.AcceptDNS = result.AcceptDNSEnabled
		s.state.mu.Unlock()
		s.broadcastState()
	}
	writeJSON(w, http.StatusOK, result.Response)
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	resp, err := s.diagnostics.GetDiagnostics(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
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
	marker, err := s.diagnostics.BugReport(r.Context(), req.Note)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"marker": marker})
}

func (s *Server) handleIntegrationStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.integration.GetStatus(r.Context()))
}

func (s *Server) handleSetIntegrationKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey string `json:"apiKey"`
	}
	if err := readJSON(w, r, &req); err != nil {
		return
	}

	st, err := s.integration.SetKey(r.Context(), req.APIKey)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	if s.fwOrch != nil && st.SiteID != "" {
		if result := s.fwOrch.SetupTailscaleFirewall(r.Context()); result.Err() != nil {
			slog.Warn("firewall setup after key save failed", "err", result.Err())
		}
		s.openTailscaleWanPort(r.Context())
	}

	s.intRetry.clearDegraded()

	s.state.mu.Lock()
	s.state.data.IntegrationStatus = st
	s.state.mu.Unlock()
	s.broadcastState()

	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleDeleteIntegrationKey(w http.ResponseWriter, r *http.Request) {
	if s.fw != nil {
		if err := s.fw.RemoveDNSForwarding(r.Context()); err != nil {
			slog.Warn("DNS forwarding cleanup failed during key removal", "err", err)
		}
	}

	if err := s.integration.DeleteKey(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove API key")
		return
	}

	slog.Info("integration API key removed")

	s.state.mu.Lock()
	s.state.data.IntegrationStatus = &service.IntegrationStatus{Configured: false}
	s.state.mu.Unlock()
	s.broadcastState()

	writeOK(w)
}

func (s *Server) handleTestIntegrationKey(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.integration.TestKey(r.Context()))
}

func (s *Server) handleGetRoutes(w http.ResponseWriter, r *http.Request) {
	resp, err := s.routing.GetRoutes(r.Context())
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSetRoutes(w http.ResponseWriter, r *http.Request) {
	var req service.SetRoutesRequest
	if err := readJSON(w, r, &req); err != nil {
		return
	}
	var clients []string
	if req.ExitNode {
		s.refreshVPNClients()
		s.vpnClientsMu.Lock()
		clients = s.deviceInfo.ActiveVPNClients
		s.vpnClientsMu.Unlock()
	}
	result, err := s.routing.SetRoutes(r.Context(), &req, clients)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleAuthKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AuthKey string `json:"authKey"`
	}
	if err := readJSON(w, r, &req); err != nil {
		return
	}
	if err := s.routing.ActivateWithKey(r.Context(), req.AuthKey); err != nil {
		writeServiceError(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleGetSubnets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Subnets []service.SubnetEntry `json:"subnets"`
	}{Subnets: s.routing.GetSubnets()})
}

func (s *Server) handleFirewallStatus(w http.ResponseWriter, r *http.Request) {
	var lastRestore *time.Time
	if p := s.lastRestore.Load(); p != nil {
		t := *p
		lastRestore = &t
	}
	resp := s.routing.GetFirewallStatus(r.Context(), service.FirewallState{
		WatcherRunning: s.watcherRunning.Load(),
		LastRestore:    lastRestore,
		UDAPIReachable: isUDAPIReachable(),
	})
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	entries := s.logBuf.Snapshot()
	writeJSON(w, http.StatusOK, map[string]any{"lines": entries})
}

func (s *Server) handleWgS2sListTunnels(w http.ResponseWriter, r *http.Request) {
	if !s.wgS2sSvc.Available() {
		writeError(w, http.StatusServiceUnavailable, "WG S2S manager not initialized")
		return
	}
	writeJSON(w, http.StatusOK, s.wgS2sSvc.ListTunnels(r.Context()))
}

func (s *Server) handleWgS2sCreateTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgS2sSvc.Available() {
		writeError(w, http.StatusServiceUnavailable, "WG S2S manager not initialized")
		return
	}
	var req service.WgS2sCreateRequest
	if err := readJSON(w, r, &req); err != nil {
		return
	}
	result, err := s.wgS2sSvc.CreateTunnel(r.Context(), &req)
	if err != nil {
		writeWgS2sError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleWgS2sUpdateTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgS2sSvc.Available() {
		writeError(w, http.StatusServiceUnavailable, "WG S2S manager not initialized")
		return
	}
	var updates wgs2s.TunnelConfig
	if err := readJSON(w, r, &updates); err != nil {
		return
	}
	result, err := s.wgS2sSvc.UpdateTunnel(r.Context(), r.PathValue("id"), updates)
	if err != nil {
		writeWgS2sError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleWgS2sDeleteTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgS2sSvc.Available() {
		writeError(w, http.StatusServiceUnavailable, "WG S2S manager not initialized")
		return
	}
	if err := s.wgS2sSvc.DeleteTunnel(r.Context(), r.PathValue("id")); err != nil {
		writeWgS2sError(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleWgS2sEnableTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgS2sSvc.Available() {
		writeError(w, http.StatusServiceUnavailable, "WG S2S manager not initialized")
		return
	}
	result, err := s.wgS2sSvc.EnableTunnel(r.Context(), r.PathValue("id"))
	if err != nil {
		writeWgS2sError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleWgS2sDisableTunnel(w http.ResponseWriter, r *http.Request) {
	if !s.wgS2sSvc.Available() {
		writeError(w, http.StatusServiceUnavailable, "WG S2S manager not initialized")
		return
	}
	if err := s.wgS2sSvc.DisableTunnel(r.Context(), r.PathValue("id")); err != nil {
		writeWgS2sError(w, err)
		return
	}
	writeOK(w)
}

func (s *Server) handleWgS2sGenerateKeypair(w http.ResponseWriter, r *http.Request) {
	kp, err := s.wgS2sSvc.GenerateKeypair()
	if err != nil {
		writeServiceError(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, kp)
}

func (s *Server) handleWgS2sGetConfig(w http.ResponseWriter, r *http.Request) {
	if !s.wgS2sSvc.Available() {
		writeError(w, http.StatusServiceUnavailable, "WG S2S manager not initialized")
		return
	}
	config, err := s.wgS2sSvc.GetConfig(r.Context(), r.PathValue("id"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"config": config})
}

func (s *Server) handleWgS2sWanIP(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"ip": s.wgS2sSvc.GetWanIP()})
}

func (s *Server) handleWgS2sLocalSubnets(w http.ResponseWriter, r *http.Request) {
	subnets := s.wgS2sSvc.GetLocalSubnets()
	writeJSON(w, http.StatusOK, subnets)
}

func (s *Server) handleWgS2sListZones(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.wgS2sSvc.ListZones())
}
