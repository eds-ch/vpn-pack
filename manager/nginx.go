package main

import (
	"bytes"
	"log/slog"
	"os"
	"os/exec"
	"unifi-tailscale/manager/config"
)

type NginxManager struct {
	configSrc  string
	configDest string
}

func NewNginxManager() *NginxManager {
	return &NginxManager{
		configSrc:  config.NginxConfigSrc,
		configDest: config.NginxConfigDest,
	}
}

func (m *NginxManager) EnsureConfig() error {
	src, err := os.ReadFile(m.configSrc)
	if err != nil {
		return err
	}

	dst, _ := os.ReadFile(m.configDest)
	if bytes.Equal(src, dst) {
		return nil
	}

	// Parent directory is install-time responsibility (deploy/install.sh).
	// Under the systemd hardening introduced for SEC-B2 the manager
	// holds a file-level bind mount on configDest only, so the parent
	// is read-only and MkdirAll would fail. unifi-core re-creates the
	// directory itself on startup; there is no scenario at runtime
	// where the parent legitimately needs to be created by us.
	if err := os.WriteFile(m.configDest, src, config.ConfigPerm); err != nil {
		return err
	}

	slog.Info("nginx config installed", "dest", m.configDest)
	return reloadNginx()
}

func reloadNginx() error {
	if err := exec.Command("nginx", "-s", "reload").Run(); err != nil {
		slog.Warn("nginx reload failed", "err", err)
		return err
	}
	slog.Info("nginx reloaded")
	return nil
}
