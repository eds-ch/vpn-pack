package main

import (
	"context"
	"embed"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"unifi-tailscale/manager/internal/wgs2s"
	"unifi-tailscale/manager/service"
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
	ts             TailscaleControl
	hub            SSEHub
	deviceInfo     DeviceInfo
	httpServer     *http.Server
	state          *TailscaleState
	fw             FirewallService
	ic             IntegrationAPI
	manifest       ManifestStore
	nginx          *NginxManager
	watcherRunning atomic.Bool
	lastRestore    atomic.Pointer[time.Time]
	restoring      atomic.Bool
	intRetry       integrationRetryState
	logBuf         *LogBuffer
	wgManager      WgS2sControl
	vpnClientsMu   sync.Mutex
	updater        *updateChecker
	fwOrch         *service.FirewallOrchestrator
	settings       *service.SettingsService
	diagnostics    *service.DiagnosticsService
	integration    *service.IntegrationService
	routing        *service.RoutingService
	tailscaleSvc   *service.TailscaleService
	wgS2sSvc       *service.WgS2sService
}

func NewServer(ctx context.Context, opts ServerOptions) *Server {
	s := &Server{
		ts:         opts.Tailscale,
		hub:        opts.Hub,
		deviceInfo: opts.DeviceInfo,
		state:      &TailscaleState{data: stateData{BackendState: "Unavailable"}},
		fw:         opts.Firewall,
		ic:         opts.Integration,
		manifest:   opts.Manifest,
		nginx:      opts.Nginx,
		logBuf:     opts.LogBuf,
		updater:    opts.Updater,
	}
	s.settings = service.NewSettingsService(
		opts.Tailscale, opts.Firewall, opts.Integration,
		settingsManifestAdapter{opts.Manifest}, opts.DeviceInfo.HasUDAPISocket,
		&settingsNotifierAdapter{
			restart:   s.restartTailscaled,
			state:     s.state,
			broadcast: s.broadcastState,
		},
	)
	s.diagnostics = service.NewDiagnosticsService(opts.Tailscale, opts.Firewall, nil)

	if opts.Firewall != nil {
		s.fwOrch = service.NewFirewallOrchestrator(
			&firewallIntegrationAdapter{ic: opts.Integration},
			&firewallManifestAdapter{ms: opts.Manifest},
			&firewallOpsAdapter{fw: opts.Firewall},
		)
	}

	s.integration = service.NewIntegrationService(
		integrationICAdapter{opts.Integration}, opts.Manifest,
		&integrationNotifierAdapter{
			fw:          opts.Firewall,
			fwOrch:      s.fwOrch,
			intRetry:    &s.intRetry,
			state:       s.state,
			broadcast:   s.broadcastState,
			openWanPort: s.openTailscaleWanPort,
		},
		fileKeyStore{},
	)
	s.routing = service.NewRoutingService(
		opts.Tailscale, opts.Firewall, opts.Integration, opts.Manifest,
		localSubnetProvider,
	)
	s.tailscaleSvc = service.NewTailscaleService(opts.Tailscale, opts.Firewall)

	var wgFw service.WgS2sFirewall
	if opts.Firewall != nil {
		wgFw = &wgS2sFirewallAdapter{fw: opts.Firewall, orch: s.fwOrch}
	}
	s.wgS2sSvc = service.NewWgS2sService(
		nil, // wg set later in initWgS2s
		wgFw,
		&wgS2sManifestAdapter{ms: opts.Manifest},
		&wgS2sLogAdapter{buf: opts.LogBuf},
		subnetValidatorProvider,
		getWanIP,
		localSubnetProvider,
	)

	mux := s.routes()

	// WriteTimeout omitted: SSE endpoint requires long-lived writes
	s.httpServer = &http.Server{
		Addr:              opts.ListenAddr,
		Handler:           mux,
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

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/status", s.handleStatus)
	mux.HandleFunc("POST /api/tailscale/up", s.handleUp)
	mux.HandleFunc("POST /api/tailscale/down", s.handleDown)
	mux.HandleFunc("POST /api/tailscale/login", s.handleLogin)
	mux.HandleFunc("POST /api/tailscale/logout", s.handleLogout)
	mux.HandleFunc("GET /api/events", s.handleSSE)
	mux.HandleFunc("GET /api/device", s.handleDevice)
	mux.HandleFunc("GET /api/routes", s.handleGetRoutes)
	mux.HandleFunc("POST /api/routes", s.handleSetRoutes)
	mux.HandleFunc("POST /api/tailscale/auth-key", s.handleAuthKey)
	mux.HandleFunc("GET /api/subnets", s.handleGetSubnets)
	mux.HandleFunc("GET /api/firewall", s.handleFirewallStatus)
	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("POST /api/settings", s.handleSetSettings)
	mux.HandleFunc("GET /api/diagnostics", s.handleDiagnostics)
	mux.HandleFunc("POST /api/bugreport", s.handleBugReport)
	mux.HandleFunc("GET /api/logs", s.handleLogs)

	mux.HandleFunc("GET /api/integration/status", s.handleIntegrationStatus)
	mux.HandleFunc("POST /api/integration/api-key", s.handleSetIntegrationKey)
	mux.HandleFunc("DELETE /api/integration/api-key", s.handleDeleteIntegrationKey)
	mux.HandleFunc("POST /api/integration/test", s.handleTestIntegrationKey)

	mux.HandleFunc("GET /api/wg-s2s/tunnels", s.handleWgS2sListTunnels)
	mux.HandleFunc("POST /api/wg-s2s/tunnels", s.handleWgS2sCreateTunnel)
	mux.HandleFunc("PATCH /api/wg-s2s/tunnels/{id}", s.handleWgS2sUpdateTunnel)
	mux.HandleFunc("DELETE /api/wg-s2s/tunnels/{id}", s.handleWgS2sDeleteTunnel)
	mux.HandleFunc("POST /api/wg-s2s/tunnels/{id}/enable", s.handleWgS2sEnableTunnel)
	mux.HandleFunc("POST /api/wg-s2s/tunnels/{id}/disable", s.handleWgS2sDisableTunnel)
	mux.HandleFunc("POST /api/wg-s2s/generate-keypair", s.handleWgS2sGenerateKeypair)
	mux.HandleFunc("GET /api/wg-s2s/tunnels/{id}/config", s.handleWgS2sGetConfig)
	mux.HandleFunc("GET /api/wg-s2s/wan-ip", s.handleWgS2sWanIP)
	mux.HandleFunc("GET /api/wg-s2s/local-subnets", s.handleWgS2sLocalSubnets)
	mux.HandleFunc("GET /api/wg-s2s/zones", s.handleWgS2sListZones)

	mux.HandleFunc("GET /api/update-check", s.handleUpdateCheck)

	mux.Handle("/", spaHandler())

	return mux
}

func (s *Server) Run(ctx context.Context) error {
	if err := connectWithBackoff(ctx, s.ts); err != nil {
		return err
	}

	go s.runNginxWatcher(ctx)

	if s.deviceInfo.HasUDAPISocket {
		if s.integrationReady() {
			if result := s.fwOrch.SetupTailscaleFirewall(ctx); result.Err() != nil {
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
	s.wgS2sSvc.SetWireGuard(wgMgr)
	s.diagnostics.SetWgS2s(wgMgr)
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

func (s *Server) restartTailscaled() {
	go func() {
		if out, err := exec.Command("systemctl", "restart", "tailscaled").CombinedOutput(); err != nil {
			slog.Warn("tailscaled restart failed", "err", err, "output", string(out))
		} else {
			slog.Info("tailscaled restarted for settings change")
		}
	}()
}

func (s *Server) integrationReady() bool {
	return s.fw != nil && s.fw.IntegrationReady()
}

func (s *Server) openTailscaleWanPort(ctx context.Context) {
	port := service.ReadTailscaledPort()
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
			_ = service.DeleteAPIKey()
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
