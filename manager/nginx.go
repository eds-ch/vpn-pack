package main

import (
	"bytes"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
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

	if err := os.MkdirAll(filepath.Dir(m.configDest), config.DirPerm); err != nil {
		return err
	}

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
