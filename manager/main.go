package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:9090", "listen address (for development/testing override)")
	socket := flag.String("socket", "/run/tailscale/tailscaled.sock", "tailscaled socket path")
	showVersion := flag.Bool("version", false, "print version and exit")
	cleanup := flag.Bool("cleanup", false, "remove UDAPI rules, WG S2S interfaces, and Integration API zones/policies, then exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("vpn-pack %s (tailscale %s, commit: %s, built: %s)\n", version, tailscaleVersion, gitCommit, buildDate)
		os.Exit(0)
	}

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, nil)))

	if *cleanup {
		runCleanup()
		return
	}

	slog.Info("starting vpn-pack", "version", version, "tailscale", tailscaleVersion, "commit", gitCommit, "buildDate", buildDate)

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

	srv := NewServer(ctx, *listen, *socket, info)

	if err := srv.nginx.EnsureConfig(); err != nil {
		slog.Warn("nginx config setup failed", "err", err)
	}

	if err := srv.Run(ctx); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}

	slog.Info("shutting down")
}
