package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

func interfaceExists(name string) bool {
	_, err := os.Stat("/sys/class/net/" + name)
	return err == nil
}

type FirewallRequest struct {
	Action     string
	Interface  string
	TunnelID   string
	AllowedIPs []string
}


func (s *Server) sendFirewallRequest(req FirewallRequest) {
	select {
	case s.firewallCh <- req:
	default:
		slog.Debug("firewall request dropped", "action", req.Action, "iface", req.Interface)

	}
}
func (s *Server) runFirewallWatcher(ctx context.Context) {
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	defer signal.Stop(sighup)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	debounced := s.startInotifyWatcher(ctx)
	s.watcherRunning.Store(true)
	defer s.watcherRunning.Store(false)

	for {
		select {
		case <-ctx.Done():
			return

		case _, ok := <-debounced:
			if !ok {
				debounced = nil
				continue
			}
			s.checkAndRestoreRules()

		case <-ticker.C:
			s.checkAndRestoreRules()

		case <-sighup:
			slog.Info("SIGHUP received, forcing reapply")
			if err := s.fw.SetupTailscaleFirewall(); err != nil {
				slog.Warn("SIGHUP reapply failed", "err", err)
			}

		case req := <-s.firewallCh:
			s.handleFirewallRequest(req)
		}
	}
}

func (s *Server) startInotifyWatcher(ctx context.Context) <-chan struct{} {
	debounced := make(chan struct{}, 1)

	watcher, err := initFsWatcher()
	if err != nil {
		close(debounced)
		return debounced
	}

	go s.runInotifyLoop(ctx, watcher, debounced)
	return debounced
}

func initFsWatcher() (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("fsnotify unavailable, polling only", "err", err)
		return nil, err
	}

	configDir := filepath.Dir(udapiConfigPath)
	if err := watcher.Add(configDir); err != nil {
		slog.Warn("inotify watch failed, polling only", "path", configDir, "err", err)
		_ = watcher.Close()
		return nil, err
	}

	slog.Info("inotify watching", "path", configDir)
	return watcher, nil
}

func (s *Server) runInotifyLoop(ctx context.Context, watcher *fsnotify.Watcher, debounced chan<- struct{}) {
	defer func() { _ = watcher.Close() }()
	defer close(debounced)

	var debounceTimer *time.Timer
	var debounceCh <-chan time.Time

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if s.handleFsEvent(event) {
				if debounceTimer == nil {
					debounceTimer = time.NewTimer(debounceDuration)
					debounceCh = debounceTimer.C
				} else {
					debounceTimer.Reset(debounceDuration)
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("inotify error", "err", err)

		case <-debounceCh:
			debounceTimer = nil
			debounceCh = nil
			select {
			case debounced <- struct{}{}:
			default:
			}
		}
	}
}

func (s *Server) handleFsEvent(event fsnotify.Event) bool {
	base := filepath.Base(event.Name)
	return base == filepath.Base(udapiConfigPath) && event.Has(fsnotify.Write)
}

func (s *Server) checkAndRestoreRules() {
	s.retryIntegrationSetup()
	s.restoreTailscaleRules()
	s.restoreWgS2sRules()
}

func (s *Server) integrationRetryInterval() time.Duration {
	switch s.integrationRetryCount {
	case 0:
		return 0
	case 1:
		return 5 * time.Second
	default:
		return 10 * time.Second
	}
}

func (s *Server) retryIntegrationSetup() {
	if !s.integrationReady() {
		return
	}
	if s.integrationDegraded.Load() {
		return
	}

	ts := s.manifest.GetTailscaleZone()
	if ts.ZoneID != "" {
		if s.integrationRetryCount > 0 {
			s.integrationRetryCount = 0
		}
		return
	}

	interval := s.integrationRetryInterval()
	if interval > 0 && time.Since(s.lastIntegrationRetry) < interval {
		return
	}

	s.lastIntegrationRetry = time.Now()
	s.integrationRetryCount++

	slog.Info("retrying integration zone/policy setup", "attempt", s.integrationRetryCount)

	if err := s.fw.SetupTailscaleFirewall(); err != nil {
		slog.Warn("integration setup retry failed", "attempt", s.integrationRetryCount, "err", err)
		return
	}

	ts = s.manifest.GetTailscaleZone()
	if ts.ZoneID == "" {
		return
	}

	slog.Info("integration setup succeeded", "zoneId", ts.ZoneID, "attempt", s.integrationRetryCount)
	s.integrationRetryCount = 0

	if port := readTailscaledPort(); port > 0 {
		if err := s.fw.OpenWanPort(port, "tailscale-wg"); err != nil {
			slog.Warn("WAN port open after integration retry failed", "port", port, "err", err)
		}
	}
}

func (s *Server) restoreTailscaleRules() {
	if !s.integrationReady() {
		return
	}
	if s.integrationDegraded.Load() {
		return
	}
	if !interfaceExists("tailscale0") {
		return
	}

	forward, input, output, ipset := s.fw.CheckTailscaleRulesPresent()

	ts := s.manifest.GetTailscaleZone()
	noZone := ts.ZoneID == ""
	if noZone {
		ipset = true
	}

	if forward && input && output && ipset {
		return
	}

	var missing []string
	if !forward {
		missing = append(missing, "FORWARD_IN")
	}
	if !input {
		missing = append(missing, "INPUT")
	}
	if !output {
		missing = append(missing, "OUTPUT")
	}
	if !ipset {
		missing = append(missing, "VPN_subnets")
	}

	slog.Info("firewall rules missing, restoring", "missing", missing)

	if err := s.fw.RestoreTailscaleRules(); err != nil {
		slog.Warn("firewall restore failed", "err", err)
		return
	}

	t := time.Now()
	s.lastRestore.Store(&t)
	slog.Info("firewall rules restored")
}

func (s *Server) restoreWgS2sRules() {
	if s.wgManager == nil {
		return
	}
	if s.integrationDegraded.Load() {
		return
	}

	var ifaces []string
	for _, t := range s.wgManager.GetTunnels() {
		if t.Enabled {
			ifaces = append(ifaces, t.InterfaceName)
		}
	}
	if len(ifaces) == 0 {
		return
	}

	present := s.fw.CheckWgS2sRulesPresent(ifaces)
	for _, t := range s.wgManager.GetTunnels() {
		if !t.Enabled {
			continue
		}
		if present[t.InterfaceName] {
			continue
		}
		slog.Info("wg-s2s firewall rules missing, restoring", "iface", t.InterfaceName)
		s.logBuf.Add(logEntry{Timestamp: time.Now().UTC().Format(time.RFC3339), Level: "info", Message: "firewall rules missing, restoring iface=" + t.InterfaceName, Source: "wgs2s"})
		if err := s.fw.SetupWgS2sFirewall(t.ID, t.InterfaceName, t.AllowedIPs); err != nil {
			slog.Warn("wg-s2s firewall restore failed", "iface", t.InterfaceName, "err", err)
			s.logBuf.Add(logEntry{Timestamp: time.Now().UTC().Format(time.RFC3339), Level: "warn", Message: "firewall restore failed iface=" + t.InterfaceName + " err=" + err.Error(), Source: "wgs2s"})
		}
	}
}

func (s *Server) reconcileWanPortPolicies() {
	if s.integrationDegraded.Load() {
		return
	}
	wanPorts := s.manifest.GetWanPortsSnapshot()
	if len(wanPorts) == 0 {
		return
	}

	siteID := s.manifest.GetSiteID()
	policies, err := s.ic.ListPolicies(siteID)
	if err != nil {
		slog.Warn("WAN port reconciliation failed: cannot list policies", "err", err)
		return
	}

	policySet := make(map[string]bool, len(policies))
	for _, p := range policies {
		policySet[p.ID] = true
	}

	for marker, entry := range wanPorts {
		if policySet[entry.PolicyID] {
			continue
		}
		slog.Info("WAN port policy missing from API, recreating", "marker", marker, "port", entry.Port)
		s.manifest.RemoveWanPort(marker)
		if err := s.fw.OpenWanPort(entry.Port, marker); err != nil {
			slog.Warn("WAN port policy recreation failed", "marker", marker, "err", err)
		}
	}
}

func (s *Server) schedulePostPolicyRestore() {
	if !s.postPolicyRestore.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.postPolicyRestore.Store(false)
		for range 10 {
			time.Sleep(2 * time.Second)
			s.checkAndRestoreRules()
		}
	}()
}

func (s *Server) handleFirewallRequest(req FirewallRequest) {
	switch req.Action {
	case "apply-wg-s2s":
		if req.Interface != "" {
			if err := s.fw.SetupWgS2sFirewall(req.TunnelID, req.Interface, req.AllowedIPs); err != nil {
				slog.Warn("wg-s2s firewall rules failed", "iface", req.Interface, "err", err)
				s.logBuf.Add(logEntry{Timestamp: time.Now().UTC().Format(time.RFC3339), Level: "warn", Message: "firewall rules failed iface=" + req.Interface + " err=" + err.Error(), Source: "wgs2s"})
			}
		}
	default:
		slog.Warn("unknown firewall request action", "action", req.Action)
	}
}
