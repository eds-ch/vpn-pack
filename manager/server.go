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

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/httpmw"
	"unifi-tailscale/manager/internal/wgs2s"
	"unifi-tailscale/manager/service"
)

//go:embed all:ui/dist
var uiFS embed.FS

type ServerOptions struct {
	Listener    net.Listener
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
	listener       net.Listener
	state          *TailscaleState
	fw             FirewallService
	ic             IntegrationAPI
	manifest       ManifestStore
	nginx          *NginxManager
	watcherRunning atomic.Bool
	lastRestore    atomic.Pointer[time.Time]
	restoring      atomic.Bool
	health         *HealthTracker
	logBuf         *LogBuffer
	wgManager      WgS2sControl
	vpnClientsMu   sync.Mutex
	updater        *updateChecker
	fwOrch         *service.FirewallOrchestrator
	settings       *service.SettingsService
	diagnostics    *service.DiagnosticsService
	integration    *service.IntegrationService
	routing        *service.RoutingService
	exitSvc        *service.ExitNodeService
	remoteExitSvc  *service.RemoteExitService
	tailscaleSvc   *service.TailscaleService
	wgS2sSvc       *service.WgS2sService
	routingHealth  *service.RoutingHealthChecker
}

func NewServer(ctx context.Context, opts ServerOptions) *Server {
	s := &Server{
		ts:         opts.Tailscale,
		hub:        opts.Hub,
		deviceInfo: opts.DeviceInfo,
		listener:   opts.Listener,
		state:      domain.NewTailscaleState(),
		fw:         opts.Firewall,
		ic:         opts.Integration,
		manifest:   opts.Manifest,
		nginx:      opts.Nginx,
		logBuf:     opts.LogBuf,
		updater:    opts.Updater,
		health:     NewHealthTracker(opts.Hub),
	}
	s.settings = service.NewSettingsService(
		opts.Tailscale, opts.Firewall, opts.Integration,
		settingsManifestAdapter{opts.Manifest}, opts.DeviceInfo.HasUDAPISocket,
		&settingsNotifierAdapter{
			restart:   s.restartTailscaled,
			state:     s.state,
			broadcast: s.broadcastState,
		},
		s.activeS2sTunnels,
	)
	s.diagnostics = service.NewDiagnosticsService(opts.Tailscale, opts.Firewall, nil)
	s.diagnostics.SetZoneLookup(opts.Manifest)
	s.routingHealth = service.NewRoutingHealthChecker()

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
			fw:           opts.Firewall,
			fwOrch:       s.fwOrch,
			guardedSetup: s.guardedSetupTailscaleFirewall,
			health:       s.health,
			state:        s.state,
			broadcast:    s.broadcastState,
			openWanPort:  s.openTailscaleWanPort,
		},
		fileKeyStore{},
	)
	s.exitSvc = service.NewExitNodeService(opts.Manifest, nil)
	s.remoteExitSvc = service.NewRemoteExitService(opts.Tailscale, s.exitSvc, opts.Manifest)
	s.routing = service.NewRoutingService(
		opts.Tailscale, opts.Firewall, opts.Integration, opts.Manifest,
		localSubnetProvider,
	)
	s.tailscaleSvc = service.NewTailscaleService(opts.Tailscale, opts.Firewall)

	var wgFw service.WgS2sFirewall
	if opts.Firewall != nil {
		wgFw = &wgS2sFirewallAdapter{fw: opts.Firewall, orch: s.fwOrch}
	}
	s.wgS2sSvc = service.NewWgS2sService(service.WgS2sConfig{
		Firewall:        wgFw,
		Manifest:        &wgS2sManifestAdapter{ms: opts.Manifest},
		Logger:          &wgS2sLogAdapter{buf: opts.LogBuf},
		ValidateSubnets: subnetValidatorProvider,
		WanIP:           getWanIP,
		LocalSubnets:    localSubnetProvider,
	})

	mux := s.routes()

	// WriteTimeout omitted: SSE endpoint requires long-lived writes
	s.httpServer = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: config.ReadHeaderTimeout,
		ReadTimeout:       config.ReadTimeout,
		IdleTimeout:       config.IdleTimeout,
		MaxHeaderBytes:    config.MaxHeaderBytes,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
		ConnContext: httpmw.ConnContext,
	}

	s.validateIntegration(ctx)

	return s
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()

	allowedUIDs := httpmw.LookupAllowedUIDs("nginx")
	read := httpmw.Chain(
		httpmw.Recover(),
		httpmw.PeerUIDAuth(allowedUIDs...),
		httpmw.CSRF(),
	)
	mutate := httpmw.Chain(
		httpmw.Recover(),
		httpmw.PeerUIDAuth(allowedUIDs...),
		httpmw.CSRF(),
		httpmw.RequireJSON(config.MaxRequestBodyBytes),
	)
	get := func(p string, h http.HandlerFunc) { mux.Handle("GET "+p, read(h)) }
	post := func(p string, h http.HandlerFunc) { mux.Handle("POST "+p, mutate(h)) }
	patch := func(p string, h http.HandlerFunc) { mux.Handle("PATCH "+p, mutate(h)) }
	del := func(p string, h http.HandlerFunc) { mux.Handle("DELETE "+p, mutate(h)) }

	get("/api/status", s.handleStatus)
	get("/api/health", s.handleHealth)
	post("/api/tailscale/up", s.handleUp)
	post("/api/tailscale/down", s.handleDown)
	post("/api/tailscale/login", s.handleLogin)
	post("/api/tailscale/logout", s.handleLogout)
	get("/api/events", s.handleSSE)
	get("/api/device", s.handleDevice)
	get("/api/routes", s.handleGetRoutes)
	post("/api/routes", s.handleSetRoutes)
	post("/api/tailscale/auth-key", s.handleAuthKey)
	get("/api/subnets", s.handleGetSubnets)
	get("/api/firewall", s.handleFirewallStatus)
	get("/api/settings", s.handleGetSettings)
	post("/api/settings", s.handleSetSettings)
	get("/api/diagnostics", s.handleDiagnostics)
	post("/api/bugreport", s.handleBugReport)
	get("/api/logs", s.handleLogs)

	get("/api/integration/status", s.handleIntegrationStatus)
	post("/api/integration/api-key", s.handleSetIntegrationKey)
	del("/api/integration/api-key", s.handleDeleteIntegrationKey)
	post("/api/integration/test", s.handleTestIntegrationKey)

	get("/api/exit-node", s.handleGetRemoteExit)
	post("/api/exit-node", s.handleEnableRemoteExit)
	del("/api/exit-node", s.handleDisableRemoteExit)

	get("/api/wg-s2s/tunnels", s.handleWgS2sListTunnels)
	post("/api/wg-s2s/tunnels", s.handleWgS2sCreateTunnel)
	patch("/api/wg-s2s/tunnels/{id}", s.handleWgS2sUpdateTunnel)
	del("/api/wg-s2s/tunnels/{id}", s.handleWgS2sDeleteTunnel)
	post("/api/wg-s2s/tunnels/{id}/enable", s.handleWgS2sEnableTunnel)
	post("/api/wg-s2s/tunnels/{id}/disable", s.handleWgS2sDisableTunnel)
	post("/api/wg-s2s/tunnels/{id}/setup-zone", s.handleWgS2sSetupZone)
	post("/api/wg-s2s/generate-keypair", s.handleWgS2sGenerateKeypair)
	get("/api/wg-s2s/tunnels/{id}/config", s.handleWgS2sGetConfig)
	get("/api/wg-s2s/wan-ip", s.handleWgS2sWanIP)
	get("/api/wg-s2s/local-subnets", s.handleWgS2sLocalSubnets)
	get("/api/wg-s2s/zones", s.handleWgS2sListZones)

	get("/api/update-check", s.handleUpdateCheck)

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
			if result, ran := s.guardedSetupTailscaleFirewall(ctx); ran && result.Err() != nil {
				slog.Warn("initial firewall apply failed", "err", result.Err())
			}
		}
		go s.runFirewallWatcher(ctx)
	}

	s.initWgS2s(ctx)
	s.restoreExitNodeRules(ctx)

	if s.integrationReady() {
		s.openTailscaleWanPort(ctx)
		go s.reconcileWanPortPolicies(ctx)
	}

	go s.runWatcher(ctx)
	go runLogCollector(ctx, s.ts, s.logBuf)
	go s.runUpdateChecker(ctx)

	if s.listener == nil {
		return errors.New("server has no listener (unix socket required)")
	}
	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", s.listener.Addr().String())
		if err := s.httpServer.Serve(s.listener); err != nil && err != http.ErrServerClosed {
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
		if s.fw != nil {
			s.fw.WaitBackground()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	case err := <-errCh:
		if s.fw != nil {
			s.fw.WaitBackground()
		}
		return err
	}
}

func (s *Server) initWgS2s(ctx context.Context) {
	wgs2sLog := slog.New(newBufferHandler(s.logBuf, "wgs2s", slog.NewJSONHandler(os.Stderr, nil)))
	wgMgr, err := wgs2s.NewTunnelManager(config.WgS2sConfigDir, wgs2sLog)
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
	s.wgS2sSvc.ReconcileZones(ctx)
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
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "systemctl", "restart", "tailscaled").CombinedOutput(); err != nil {
			slog.Warn("tailscaled restart failed", "err", err, "output", string(out))
		} else {
			slog.Info("tailscaled restarted for settings change")
		}
	}()
}

func (s *Server) activeS2sTunnels(ctx context.Context) []service.S2sTunnelInfo {
	if s.wgS2sSvc == nil || !s.wgS2sSvc.Available() {
		return nil
	}
	tunnels := s.wgS2sSvc.ListTunnels(ctx)
	var result []service.S2sTunnelInfo
	for _, t := range tunnels {
		if !t.Enabled || len(t.AllowedIPs) == 0 {
			continue
		}
		result = append(result, service.S2sTunnelInfo{
			Name:       t.Name,
			AllowedIPs: t.AllowedIPs,
		})
	}
	return result
}

func (s *Server) integrationReady() bool {
	return s.fw != nil && s.fw.IntegrationReady()
}

// guardedSetup runs fn under the restoring CAS so that two concurrent callers
// cannot enter the inner setup path simultaneously. Returns true if fn ran,
// false if another caller was already in flight. The CAS is always released
// before returning.
func (s *Server) guardedSetup(fn func()) bool {
	if !s.restoring.CompareAndSwap(false, true) {
		return false
	}
	defer s.restoring.Store(false)
	fn()
	return true
}

// guardedSetupTailscaleFirewall runs SetupTailscaleFirewall under the restoring
// CAS so concurrent callers (SIGHUP reapply, OnKeyConfigured, repairMissing-
// Policies, boot apply) cannot trigger duplicate UDAPI zone-create attempts
// (BUG-M6). Returns the SetupResult plus a flag indicating whether the inner
// setup actually ran; a false flag means another caller held the guard and
// the call was skipped.
func (s *Server) guardedSetupTailscaleFirewall(ctx context.Context) (*service.SetupResult, bool) {
	var result *service.SetupResult
	ran := s.guardedSetup(func() {
		result = s.fwOrch.SetupTailscaleFirewall(ctx)
	})
	return result, ran
}

func (s *Server) openTailscaleWanPort(ctx context.Context) {
	port := service.ReadTailscaledPort()
	if port <= 0 {
		return
	}
	if err := s.fw.OpenWanPort(ctx, port, config.WanMarkerTailscaleWG); err != nil {
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
			if err := service.DeleteAPIKey(); err != nil {
				slog.Warn("failed to delete API key file", "err", err)
			}
			if err := s.manifest.ResetIntegration(); err != nil {
				slog.Warn("failed to reset manifest integration", "err", err)
			}
			s.health.SetDegraded(WatcherFirewall, "key_invalid")
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
		if err := s.manifest.SetSiteID(siteID); err != nil {
			slog.Warn("failed to save site ID", "err", err)
		}
		slog.Info("discovered site ID", "siteId", siteID)
	} else if siteID != s.manifest.GetSiteID() {
		slog.Warn("site ID changed, resetting manifest", "old", s.manifest.GetSiteID(), "new", siteID)
		if err := s.manifest.SetSiteID(siteID); err != nil {
			slog.Warn("failed to save site ID", "err", err)
		}
		if err := s.manifest.ResetIntegration(); err != nil {
			slog.Warn("failed to reset manifest integration", "err", err)
		}
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
		if err := s.manifest.ResetIntegration(); err != nil {
			slog.Warn("failed to reset manifest integration", "err", err)
		}
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
			if err := s.manifest.SetTailscaleZone(ts.ZoneID, ts.ZoneName, valid, ts.ChainPrefix); err != nil {
				slog.Warn("failed to save manifest tailscale zone", "err", err)
			}
		}
	}
}
