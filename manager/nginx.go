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
		// Content is fine, but the mode may not be: installs whose snippet was
		// re-created by a pre-1.6.1 manager carry the right bytes at 0644.
		return m.ensurePerm()
	}

	// Parent directory is install-time responsibility (deploy/install.sh).
	// Under the systemd hardening introduced for SEC-B2 the manager
	// holds a file-level bind mount on configDest only, so the parent
	// is read-only and MkdirAll would fail. unifi-core re-creates the
	// directory itself on startup; there is no scenario at runtime
	// where the parent legitimately needs to be created by us.
	if err := os.WriteFile(m.configDest, src, config.NginxConfigPerm); err != nil {
		return err
	}
	if err := m.ensurePerm(); err != nil {
		return err
	}

	slog.Info("nginx config installed", "dest", m.configDest)
	return reloadNginx()
}

// ensurePerm holds the snippet at NginxConfigPerm. os.WriteFile only applies a
// mode when it creates the file, so a rewrite over a widened file keeps the
// widened mode; this is also the only repair path for installs that already
// have the right content at 0644.
func (m *NginxManager) ensurePerm() error {
	fi, err := os.Stat(m.configDest)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.Mode().Perm() == config.NginxConfigPerm {
		return nil
	}
	slog.Info("tightening nginx config perms", "dest", m.configDest, "was", fi.Mode().Perm().String())
	return os.Chmod(m.configDest, config.NginxConfigPerm)
}

func reloadNginx() error {
	if err := exec.Command("nginx", "-s", "reload").Run(); err != nil {
		slog.Warn("nginx reload failed", "err", err)
		return err
	}
	slog.Info("nginx reloaded")
	return nil
}
