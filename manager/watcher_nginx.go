package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

func (s *Server) runNginxWatcher(ctx context.Context) {
	if s.nginx == nil {
		return
	}

	if !s.waitForNginxDir(ctx) {
		return
	}

	s.nginx.EnsureConfig() //nolint:errcheck

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("nginx inotify unavailable, polling only", "err", err)
		s.nginxPollLoop(ctx)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(nginxConfigDir); err != nil {
		slog.Warn("nginx dir watch failed, polling only", "path", nginxConfigDir, "err", err)
		s.nginxPollLoop(ctx)
		return
	}

	slog.Info("nginx watcher started", "path", nginxConfigDir)
	s.nginxWatchLoop(ctx, watcher)
}

func (s *Server) waitForNginxDir(ctx context.Context) bool {
	if _, err := os.Stat(nginxConfigDir); err == nil {
		return true
	}

	slog.Info("waiting for nginx config dir", "path", nginxConfigDir)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	deadline := time.After(2 * time.Minute)
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline:
			slog.Warn("nginx config dir not found after 2m, continuing with polling")
			return true
		case <-ticker.C:
			if _, err := os.Stat(nginxConfigDir); err == nil {
				slog.Info("nginx config dir appeared")
				return true
			}
		}
	}
}

func (s *Server) nginxWatchLoop(ctx context.Context, watcher *fsnotify.Watcher) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-watcher.Events:
			if !ok {
				slog.Warn("nginx inotify channel closed, switching to polling")
				s.nginxPollLoop(ctx)
				return
			}
			s.handleNginxEvent(event)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("nginx inotify error", "err", err)

		case <-ticker.C:
			s.checkNginxConfig()
		}
	}
}

func (s *Server) nginxPollLoop(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkNginxConfig()
		}
	}
}

func (s *Server) handleNginxEvent(event fsnotify.Event) {
	base := filepath.Base(event.Name)

	if base == nginxConfigFile && (event.Has(fsnotify.Remove) || event.Has(fsnotify.Rename)) {
		slog.Info("nginx config removed, restoring", "file", event.Name)
		if err := s.nginx.EnsureConfig(); err != nil {
			slog.Warn("nginx config restore failed", "err", err)
		}
		return
	}

	if event.Has(fsnotify.Create) && base != nginxConfigFile {
		if _, err := os.Stat(s.nginx.configDest); os.IsNotExist(err) {
			slog.Info("nginx dir repopulated, restoring config")
			if err := s.nginx.EnsureConfig(); err != nil {
				slog.Warn("nginx config restore failed", "err", err)
			}
		}
	}
}

func (s *Server) checkNginxConfig() {
	if _, err := os.Stat(s.nginx.configDest); os.IsNotExist(err) {
		slog.Info("nginx config missing, restoring")
		if err := s.nginx.EnsureConfig(); err != nil {
			slog.Warn("nginx config restore failed", "err", err)
		}
	}
}
