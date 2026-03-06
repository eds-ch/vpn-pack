package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
)

func interfaceExists(name string) bool {
	_, err := os.Stat("/sys/class/net/" + name)
	return err == nil
}

type integrationRetryState struct {
	degraded   atomic.Bool
	retryCount int
	lastRetry  time.Time
}

func (r *integrationRetryState) isDegraded() bool  { return r.degraded.Load() }
func (r *integrationRetryState) markDegraded()      { r.degraded.Store(true) }
func (r *integrationRetryState) clearDegraded()     { r.degraded.Store(false) }

func (r *integrationRetryState) interval() time.Duration {
	switch r.retryCount {
	case 0:
		return 0
	case 1:
		return 5 * time.Second
	default:
		return 10 * time.Second
	}
}

func (r *integrationRetryState) shouldRetry() bool {
	if r.degraded.Load() {
		return false
	}
	iv := r.interval()
	return iv == 0 || time.Since(r.lastRetry) >= iv
}

func (r *integrationRetryState) recordAttempt() {
	r.lastRetry = time.Now()
	r.retryCount++
}

func (r *integrationRetryState) reset() { r.retryCount = 0 }
func (r *integrationRetryState) count() int { return r.retryCount }

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
			s.checkAndRestoreRules(ctx)

		case <-ticker.C:
			s.checkAndRestoreRules(ctx)

		case <-sighup:
			slog.Info("SIGHUP received, forcing reapply")
			if result := s.fwOrch.SetupTailscaleFirewall(ctx); result.Err() != nil {
				slog.Warn("SIGHUP reapply failed", "err", result.Err())
			}
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

func (s *Server) checkAndRestoreRules(ctx context.Context) {
	if !s.restoring.CompareAndSwap(false, true) {
		return
	}
	defer s.restoring.Store(false)

	s.retryIntegrationSetup(ctx)
	s.restoreTailscaleRules(ctx)
	s.restoreWgS2sRules(ctx)
}

func (s *Server) retryIntegrationSetup(ctx context.Context) {
	if !s.integrationReady() {
		return
	}

	ts := s.manifest.GetTailscaleZone()
	if ts.ZoneID != "" {
		s.intRetry.reset()
		return
	}

	if !s.intRetry.shouldRetry() {
		return
	}
	s.intRetry.recordAttempt()

	slog.Info("retrying integration zone/policy setup", "attempt", s.intRetry.count())

	result := s.fwOrch.SetupTailscaleFirewall(ctx)
	if result.Err() != nil {
		slog.Warn("integration setup retry failed", "attempt", s.intRetry.count(), "err", result.Err())
		return
	}

	ts = s.manifest.GetTailscaleZone()
	if ts.ZoneID == "" {
		return
	}

	slog.Info("integration setup succeeded", "zoneId", ts.ZoneID, "attempt", s.intRetry.count())
	s.intRetry.reset()

	s.openTailscaleWanPort(ctx)
}

func (s *Server) restoreTailscaleRules(ctx context.Context) {
	if !s.integrationReady() {
		return
	}
	if s.intRetry.isDegraded() {
		return
	}
	if !interfaceExists(tailscaleInterface) {
		return
	}

	forward, input, output, ipset := s.fw.CheckTailscaleRulesPresent(ctx)

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

	if err := s.fw.RestoreTailscaleRules(ctx); err != nil {
		slog.Warn("firewall restore failed", "err", err)
		return
	}

	t := time.Now()
	s.lastRestore.Store(&t)
	slog.Info("firewall rules restored")
}

func (s *Server) restoreWgS2sRules(ctx context.Context) {
	if s.wgManager == nil {
		return
	}
	if s.intRetry.isDegraded() {
		return
	}

	tunnels := s.wgManager.GetTunnels()
	var ifaces []string
	for _, t := range tunnels {
		if t.Enabled {
			ifaces = append(ifaces, t.InterfaceName)
		}
	}
	if len(ifaces) == 0 {
		return
	}

	present := s.fw.CheckWgS2sRulesPresent(ctx, ifaces)
	for _, t := range tunnels {
		if !t.Enabled {
			continue
		}
		if present[t.InterfaceName] {
			continue
		}
		slog.Info("wg-s2s firewall rules missing, restoring", "iface", t.InterfaceName)
		s.logBuf.Add(newLogEntry("info", fmt.Sprintf("firewall rules missing, restoring iface=%s", t.InterfaceName), "wgs2s"))
		if err := s.fw.SetupWgS2sFirewall(ctx, t.ID, t.InterfaceName, t.AllowedIPs); err != nil {
			slog.Warn("wg-s2s firewall restore failed", "iface", t.InterfaceName, "err", err)
			s.logBuf.Add(newLogEntry("warn", fmt.Sprintf("firewall restore failed iface=%s err=%v", t.InterfaceName, err), "wgs2s"))
		}
	}
}

func (s *Server) reconcileWanPortPolicies(ctx context.Context) {
	if s.intRetry.isDegraded() {
		return
	}
	wanPorts := s.manifest.GetWanPortsSnapshot()
	if len(wanPorts) == 0 {
		return
	}

	siteID := s.manifest.GetSiteID()
	policies, err := s.ic.ListPolicies(ctx, siteID)
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
		if err := s.manifest.RemoveWanPort(marker); err != nil {
			slog.Warn("failed to remove stale WAN port entry from manifest", "marker", marker, "err", err)
		}
		if err := s.fw.OpenWanPort(ctx, entry.Port, marker); err != nil {
			slog.Warn("WAN port policy recreation failed", "marker", marker, "err", err)
		}
	}
}

