package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/service"
	"unifi-tailscale/manager/sse"
	"unifi-tailscale/manager/state"
)

func main() {
	listenSocket := flag.String("listen-socket", "/run/vpn-pack/manager.sock", "manager API unix-socket listener path")
	socket := flag.String("socket", "/run/tailscale/tailscaled.sock", "tailscaled socket path")
	showVersion := flag.Bool("version", false, "print version and exit")
	cleanup := flag.Bool("cleanup", false, "remove UDAPI rules, WG S2S interfaces, and Integration API zones/policies, then exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("vpn-pack %s (tailscale %s, commit: %s, built: %s)\n", config.Version, config.TailscaleVersion, config.GitCommit, config.BuildDate)
		os.Exit(0)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	if *cleanup {
		if err := runCleanup(); err != nil {
			os.Exit(1)
		}
		return
	}

	slog.Info("starting vpn-pack", "version", config.Version, "tailscale", config.TailscaleVersion, "commit", config.GitCommit, "buildDate", config.BuildDate)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	info := detectDevice()
	slog.Info("device detected",
		"model", info.Model,
		"modelShort", info.ModelShort,
		"firmware", info.Firmware,
		"unifiVersion", info.UniFiVersion,
		"hasTUN", info.HasTUN,
		"hasUDAPISocket", info.HasUDAPISocket,
		"persistentFree", info.PersistentFree,
		"activeVPNClients", info.ActiveVPNClients,
	)

	if err := checkMinUniFiVersion(info.UniFiVersion); err != nil {
		slog.Error("version requirement not met", "err", err)
		os.Exit(78)
	}

	apiKey := service.LoadAPIKey()
	ic := NewIntegrationClient(apiKey)

	sweepStartupOrphanTmps([]string{
		filepath.Dir(config.ManifestPath),
		config.WgS2sConfigDir,
	})

	manifest, err := LoadManifest(config.ManifestPath)
	if err != nil {
		slog.Warn("manifest load failed", "err", err)
		manifest = state.NewManifest(config.ManifestPath)
	}
	if manifest.Recovered() {
		// BUG-L8: with the manifest gone, the integration zones/policies in
		// UniFi may be orphaned. Scan-based cleanup that does not depend on
		// manifest state is the scope of BUG-L17 / Task 10.12; until then,
		// the operator can run `vpn-pack-manager --cleanup` to remove the
		// known resources (best-effort while the manifest is empty).
		slog.Warn("manifest was corrupted at load; quarantined and reset to empty; integration zones may be orphaned — run `vpn-pack-manager --cleanup` to remove residual resources",
			"path", config.ManifestPath)
	}

	ln, err := openManagerSocket(*listenSocket)
	if err != nil {
		slog.Error("listener open failed", "err", err, "path", *listenSocket)
		os.Exit(1)
	}

	srv := NewServer(ctx, ServerOptions{
		Listener:    ln,
		SocketPath:  *socket,
		DeviceInfo:  info,
		Tailscale:   NewTailscaleControl(*socket),
		Hub:         sse.NewHub(),
		Manifest:    manifest,
		Integration: ic,
		Firewall:    NewFirewallManager(config.UDAPISocketPath, ic, manifest),
		Nginx:       NewNginxManager(),
		LogBuf:      NewLogBuffer(config.LogBufferSize),
		Updater:     newUpdateChecker(),
	})

	if err := srv.Run(ctx); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}

	slog.Info("shutting down")
}
