package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"unifi-tailscale/manager/internal/wgs2s"
)

//go:embed all:ui/dist
var uiFS embed.FS

type ServerOptions struct {
	ListenAddr  string
	SocketPath  string
	DeviceInfo  DeviceInfo
	Tailscale   TailscaleControl
	Hub         SSEHub
	Manifest    ManifestStore
	Integration IntegrationAPI
	Firewall    FirewallService
	Nginx       *NginxManager
	LogBuf      *LogBuffer
	Updater     *updateChecker
}

type Server struct {
	ts                TailscaleControl
	hub               SSEHub
	mux               *http.ServeMux
	deviceInfo        DeviceInfo
	httpServer        *http.Server
	state             *TailscaleState
	fw                FirewallService
	ic                IntegrationAPI
	manifest          ManifestStore
	nginx             *NginxManager
	watcherRunning    atomic.Bool
	lastRestore       atomic.Pointer[time.Time]
	restoring         atomic.Bool
	intRetry          integrationRetryState
	logBuf            *LogBuffer
	wgManager         WgS2sControl
	vpnClientsMu     sync.Mutex
	updater           *updateChecker
	intCache          integrationCache
}

func NewServer(ctx context.Context, opts ServerOptions) *Server {
	s := &Server{
		ts:         opts.Tailscale,
		hub:        opts.Hub,
		mux:        http.NewServeMux(),
		deviceInfo: opts.DeviceInfo,
		state:      &TailscaleState{data: stateData{BackendState: "Unavailable"}},
		fw:         opts.Firewall,
		ic:         opts.Integration,
		manifest:   opts.Manifest,
		nginx:      opts.Nginx,
		logBuf:     opts.LogBuf,
		updater:    opts.Updater,
	}
	s.mux.HandleFunc("GET /api/status", s.handleStatus)
	s.mux.HandleFunc("POST /api/tailscale/up", s.handleUp)
	s.mux.HandleFunc("POST /api/tailscale/down", s.handleDown)
	s.mux.HandleFunc("POST /api/tailscale/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/tailscale/logout", s.handleLogout)
	s.mux.HandleFunc("GET /api/events", s.handleSSE)
	s.mux.HandleFunc("GET /api/device", s.handleDevice)
	s.mux.HandleFunc("GET /api/routes", s.handleGetRoutes)
	s.mux.HandleFunc("POST /api/routes", s.handleSetRoutes)
	s.mux.HandleFunc("POST /api/tailscale/auth-key", s.handleAuthKey)
	s.mux.HandleFunc("GET /api/subnets", s.handleGetSubnets)
	s.mux.HandleFunc("GET /api/firewall", s.handleFirewallStatus)
	s.mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.mux.HandleFunc("POST /api/settings", s.handleSetSettings)
	s.mux.HandleFunc("GET /api/diagnostics", s.handleDiagnostics)
	s.mux.HandleFunc("POST /api/bugreport", s.handleBugReport)
	s.mux.HandleFunc("GET /api/logs", s.handleLogs)

	s.mux.HandleFunc("GET /api/integration/status", s.handleIntegrationStatus)
	s.mux.HandleFunc("POST /api/integration/api-key", s.handleSetIntegrationKey)
	s.mux.HandleFunc("DELETE /api/integration/api-key", s.handleDeleteIntegrationKey)
	s.mux.HandleFunc("POST /api/integration/test", s.handleTestIntegrationKey)

	s.mux.HandleFunc("GET /api/wg-s2s/tunnels", s.handleWgS2sListTunnels)
	s.mux.HandleFunc("POST /api/wg-s2s/tunnels", s.handleWgS2sCreateTunnel)
	s.mux.HandleFunc("PATCH /api/wg-s2s/tunnels/{id}", s.handleWgS2sUpdateTunnel)
	s.mux.HandleFunc("DELETE /api/wg-s2s/tunnels/{id}", s.handleWgS2sDeleteTunnel)
	s.mux.HandleFunc("POST /api/wg-s2s/tunnels/{id}/enable", s.handleWgS2sEnableTunnel)
	s.mux.HandleFunc("POST /api/wg-s2s/tunnels/{id}/disable", s.handleWgS2sDisableTunnel)
	s.mux.HandleFunc("POST /api/wg-s2s/generate-keypair", s.handleWgS2sGenerateKeypair)
	s.mux.HandleFunc("GET /api/wg-s2s/tunnels/{id}/config", s.handleWgS2sGetConfig)
	s.mux.HandleFunc("GET /api/wg-s2s/wan-ip", s.handleWgS2sWanIP)
	s.mux.HandleFunc("GET /api/wg-s2s/local-subnets", s.handleWgS2sLocalSubnets)
	s.mux.HandleFunc("GET /api/wg-s2s/zones", s.handleWgS2sListZones)

	s.mux.HandleFunc("GET /api/update-check", s.handleUpdateCheck)

	s.mux.Handle("/", spaHandler())

	// WriteTimeout omitted: SSE endpoint requires long-lived writes
	s.httpServer = &http.Server{
		Addr:              opts.ListenAddr,
		Handler:           s.mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	s.validateIntegration(ctx)

	return s
}

func (s *Server) Run(ctx context.Context) error {
	if err := connectWithBackoff(ctx, s.ts); err != nil {
		return err
	}

	go s.runNginxWatcher(ctx)

	if s.deviceInfo.HasUDAPISocket {
		if s.integrationReady() {
			if result := s.fw.SetupTailscaleFirewall(ctx); result.Err() != nil {
				slog.Warn("initial firewall apply failed", "err", result.Err())
			}
		}
		go s.runFirewallWatcher(ctx)
	}

	s.initWgS2s(ctx)

	if s.integrationReady() {
		s.openTailscaleWanPort(ctx)
		go s.reconcileWanPortPolicies(ctx)
	}

	go s.runWatcher(ctx)
	go runLogCollector(ctx, s.ts, s.logBuf)
	go s.runUpdateChecker(ctx)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	defer func() {
		if hasDPIFingerprint() {
			if err := setDPIFingerprint(true); err != nil {
				slog.Warn("failed to restore DPI fingerprinting on shutdown", "err", err)
			}
		}
		if s.wgManager != nil {
			s.wgManager.Close()
		}
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func (s *Server) initWgS2s(ctx context.Context) {
	wgs2sLog := slog.New(newBufferHandler(s.logBuf, "wgs2s", slog.NewJSONHandler(os.Stderr, nil)))
	wgMgr, err := wgs2s.NewTunnelManager(wgS2sConfigDir, wgs2sLog)
	if err != nil {
		slog.Warn("wg-s2s manager init failed", "err", err)
		return
	}
	s.wgManager = wgMgr
	if restoreErr := wgMgr.RestoreAll(); restoreErr != nil {
		slog.Warn("wg-s2s restore failed", "err", restoreErr)
	}
	if s.fw == nil {
		return
	}
	for _, t := range wgMgr.GetTunnels() {
		if t.Enabled {
			if err := s.fw.SetupWgS2sFirewall(ctx, t.ID, t.InterfaceName, t.AllowedIPs); err != nil {
				slog.Warn("wg-s2s firewall rules failed", "iface", t.InterfaceName, "err", err)
			}
		}
	}
}

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
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
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

func isUDAPIReachable() bool {
	_, err := os.Stat(udapiSocketPath)
	return err == nil
}

func (s *Server) integrationReady() bool {
	return s.fw != nil && s.fw.IntegrationReady()
}

func (s *Server) openWgS2sWanPort(ctx context.Context, port int, iface string) {
	if s.fw == nil {
		return
	}
	if err := s.fw.OpenWanPort(ctx, port, wanMarkerWgS2sPrefix+iface); err != nil {
		slog.Warn("wg-s2s WAN port open failed", "port", port, "err", err)
	} else {
		s.fw.RestoreRulesWithRetry(ctx, 3, 2*time.Second)
	}
}

func (s *Server) closeWgS2sWanPort(ctx context.Context, port int, iface string) {
	if s.fw == nil || port <= 0 {
		return
	}
	if err := s.fw.CloseWanPort(ctx, port, wanMarkerWgS2sPrefix+iface); err != nil {
		slog.Warn("wg-s2s WAN port close failed", "port", port, "err", err)
	} else {
		s.fw.RestoreRulesWithRetry(ctx, 3, 2*time.Second)
	}
}

func (s *Server) openTailscaleWanPort(ctx context.Context) {
	port := readTailscaledPort()
	if port <= 0 {
		return
	}
	if err := s.fw.OpenWanPort(ctx, port, wanMarkerTailscaleWG); err != nil {
		slog.Warn("tailscale WG WAN port open failed", "port", port, "err", err)
	}
}

func (s *Server) validateIntegration(ctx context.Context) {
	if !s.ic.HasAPIKey() {
		return
	}

	_, err := s.ic.Validate(ctx)
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			slog.Warn("API key invalid (likely factory reset), clearing")
			s.ic.SetAPIKey("")
			_ = deleteAPIKey()
			_ = s.manifest.ResetIntegration()
			s.intRetry.markDegraded()
			return
		}
		slog.Warn("integration validation failed", "err", err)
		return
	}

	siteID, err := s.ic.DiscoverSiteID(ctx)
	if err != nil {
		slog.Warn("site discovery failed", "err", err)
		return
	}
	if s.manifest.GetSiteID() == "" {
		_ = s.manifest.SetSiteID(siteID)
		slog.Info("discovered site ID", "siteId", siteID)
	} else if siteID != s.manifest.GetSiteID() {
		slog.Warn("site ID changed, resetting manifest", "old", s.manifest.GetSiteID(), "new", siteID)
		_ = s.manifest.SetSiteID(siteID)
		_ = s.manifest.ResetIntegration()
	}

	s.validateManifestZones(ctx, siteID)
}

func (s *Server) validateManifestZones(ctx context.Context, siteID string) {
	ts := s.manifest.GetTailscaleZone()
	if ts.ZoneID == "" {
		return
	}
	zones, err := s.ic.ListZones(ctx, siteID)
	if err != nil {
		slog.Warn("zone validation failed", "err", err)
		return
	}
	zoneFound := false
	for _, z := range zones {
		if z.ID == ts.ZoneID {
			zoneFound = true
			break
		}
	}
	if !zoneFound {
		slog.Warn("manifest zone not found in API, resetting", "staleZoneId", ts.ZoneID)
		_ = s.manifest.ResetIntegration()
		return
	}

	if len(ts.PolicyIDs) > 0 {
		policies, err := s.ic.ListPolicies(ctx, siteID)
		if err != nil {
			slog.Warn("policy validation failed", "err", err)
			return
		}
		policySet := make(map[string]bool, len(policies))
		for _, p := range policies {
			policySet[p.ID] = true
		}
		var valid []string
		for _, id := range ts.PolicyIDs {
			if policySet[id] {
				valid = append(valid, id)
			}
		}
		if len(valid) != len(ts.PolicyIDs) {
			slog.Warn("stale policy IDs in manifest, clearing", "had", len(ts.PolicyIDs), "valid", len(valid))
			_ = s.manifest.SetTailscaleZone(ts.ZoneID, ts.ZoneName, valid, ts.ChainPrefix)
		}
	}
}
