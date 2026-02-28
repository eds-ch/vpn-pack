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

	"tailscale.com/client/local"
	"unifi-tailscale/manager/internal/wgs2s"
)

//go:embed all:ui/dist
var uiFS embed.FS

type Server struct {
	lc                  *local.Client
	hub                 *Hub
	mux                 *http.ServeMux
	deviceInfo          DeviceInfo
	httpServer          *http.Server
	state               *TailscaleState
	fw                  *FirewallManager
	ic                  *IntegrationClient
	manifest            *Manifest
	nginx               *NginxManager
	firewallCh          chan FirewallRequest
	watcherRunning      atomic.Bool
	lastRestore         atomic.Pointer[time.Time]
	postPolicyRestore   atomic.Bool
	integrationDegraded    atomic.Bool
	integrationRetryCount int
	lastIntegrationRetry  time.Time
	logBuf                *LogBuffer
	wgManager           *wgs2s.TunnelManager
	vpnClientsMu       sync.Mutex
	updater             *updateChecker
}

func NewServer(ctx context.Context, listenAddr, socketPath string, info DeviceInfo) *Server {
	lc := &local.Client{
		Socket:        socketPath,
		UseSocketOnly: true,
	}

	apiKey := loadAPIKey()
	ic := NewIntegrationClient(apiKey)

	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		slog.Warn("manifest load failed", "err", err)
		manifest = &Manifest{path: manifestPath, Version: 2, CreatedAt: time.Now().UTC()}
	}

	s := &Server{
		lc:         lc,
		hub:        NewHub(),
		mux:        http.NewServeMux(),
		deviceInfo: info,
		state:      &TailscaleState{data: stateData{BackendState: "Unavailable"}},
		fw:         NewFirewallManager(udapiSocketPath, ic, manifest),
		ic:         ic,
		manifest:   manifest,
		nginx:      NewNginxManager(),
		firewallCh: make(chan FirewallRequest, firewallChBuffer),
		logBuf:     NewLogBuffer(logBufferSize),
		updater:    newUpdateChecker(),
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
		Addr:              listenAddr,
		Handler:           s.mux,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	s.validateIntegration()

	return s
}

func (s *Server) Run(ctx context.Context) error {
	if err := connectWithBackoff(ctx, s.lc); err != nil {
		return err
	}

	go s.runNginxWatcher(ctx)

	if s.deviceInfo.HasUDAPISocket {
		if s.integrationReady() {
			if err := s.fw.SetupTailscaleFirewall(); err != nil {
				slog.Warn("initial firewall apply failed", "err", err)
			}
		}
		go s.runFirewallWatcher(ctx)
	}

	wgs2sLog := slog.New(newBufferHandler(s.logBuf, "wgs2s", slog.NewJSONHandler(os.Stderr, nil)))
	wgMgr, err := wgs2s.NewTunnelManager(wgS2sConfigDir, wgs2sLog)
	if err != nil {
		slog.Warn("wg-s2s manager init failed", "err", err)
	} else {
		s.wgManager = wgMgr
		if restoreErr := wgMgr.RestoreAll(); restoreErr != nil {
			slog.Warn("wg-s2s restore failed", "err", restoreErr)
		}
		for _, t := range wgMgr.GetTunnels() {
			if t.Enabled {
				s.sendFirewallRequest(FirewallRequest{Action: "apply-wg-s2s", TunnelID: t.ID, Interface: t.InterfaceName})
			}
		}
	}

	if s.integrationReady() {
		if port := readTailscaledPort(); port > 0 {
			if err := s.fw.OpenWanPort(port, "tailscale-wg"); err != nil {
				slog.Warn("tailscale WG WAN port open failed", "port", port, "err", err)
			}
		}
		go s.reconcileWanPortPolicies()
	}

	go s.runWatcher(ctx)
	go runLogCollector(ctx, s.lc, s.logBuf)
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
	return s.ic != nil && s.ic.HasAPIKey() && s.manifest != nil && s.manifest.HasSiteID()
}

func (s *Server) validateIntegration() {
	if !s.ic.HasAPIKey() {
		return
	}

	_, err := s.ic.Validate()
	if err != nil {
		if errors.Is(err, ErrUnauthorized) {
			slog.Warn("API key invalid (likely factory reset), clearing")
			s.ic.SetAPIKey("")
			_ = deleteAPIKey()
			s.manifest.ResetIntegration()
			_ = s.manifest.Save()
			s.integrationDegraded.Store(true)
			return
		}
		slog.Warn("integration validation failed", "err", err)
		return
	}

	siteID, err := s.ic.DiscoverSiteID()
	if err != nil {
		slog.Warn("site discovery failed", "err", err)
		return
	}
	if s.manifest.GetSiteID() == "" {
		s.manifest.SetSiteID(siteID)
		_ = s.manifest.Save()
		slog.Info("discovered site ID", "siteId", siteID)
	} else if siteID != s.manifest.GetSiteID() {
		slog.Warn("site ID changed, resetting manifest", "old", s.manifest.GetSiteID(), "new", siteID)
		s.manifest.SetSiteID(siteID)
		s.manifest.ResetIntegration()
		_ = s.manifest.Save()
	}

	s.validateManifestZones(siteID)
}

func (s *Server) validateManifestZones(siteID string) {
	ts := s.manifest.GetTailscaleZone()
	if ts.ZoneID == "" {
		return
	}
	zones, err := s.ic.listZones(siteID)
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
		s.manifest.ResetIntegration()
		_ = s.manifest.Save()
		return
	}

	if len(ts.PolicyIDs) > 0 {
		policies, err := s.ic.ListPolicies(siteID)
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
			s.manifest.SetTailscaleZone(ts.ZoneID, valid, ts.ChainPrefix)
			_ = s.manifest.Save()
		}
	}
}
