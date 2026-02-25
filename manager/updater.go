package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

func githubReleasesURL() string {
	return "https://api.github.com/repos/" + githubRepo + "/releases/latest"
}

type UpdateInfo struct {
	Available      bool   `json:"available"`
	Version        string `json:"version"`
	CurrentVersion string `json:"currentVersion"`
	ChangelogURL   string `json:"changelogURL"`
}

type updateChecker struct {
	mu        sync.Mutex
	info      *UpdateInfo
	checkedAt time.Time
	current   string
}

func newUpdateChecker() *updateChecker {
	current := version
	if current == "dev" {
		current = readVersionFile()
	}
	return &updateChecker{
		current: current,
	}
}

func readVersionFile() string {
	data, err := os.ReadFile(versionFilePath)
	if err != nil {
		return ""
	}
	lines := strings.SplitN(string(data), "\n", 2)
	if len(lines) > 0 {
		return strings.TrimSpace(lines[0])
	}
	return ""
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func (uc *updateChecker) check(ctx context.Context) *UpdateInfo {
	uc.mu.Lock()
	if uc.info != nil && time.Since(uc.checkedAt) < updateCheckPeriod {
		info := uc.info
		uc.mu.Unlock()
		return info
	}
	uc.mu.Unlock()

	info := uc.fetchLatest(ctx)

	uc.mu.Lock()
	uc.info = info
	uc.checkedAt = time.Now()
	uc.mu.Unlock()

	return info
}

func compareVersions(a, b string) int {
	if a == "" || b == "" || a == "dev" || b == "dev" {
		return 0
	}
	partsA := strings.Split(a, ".")
	partsB := strings.Split(b, ".")
	maxLen := len(partsA)
	if len(partsB) > maxLen {
		maxLen = len(partsB)
	}
	for i := 0; i < maxLen; i++ {
		var na, nb int
		if i < len(partsA) {
			na, _ = strconv.Atoi(partsA[i])
		}
		if i < len(partsB) {
			nb, _ = strconv.Atoi(partsB[i])
		}
		if na < nb {
			return -1
		}
		if na > nb {
			return 1
		}
	}
	return 0
}

func (uc *updateChecker) fetchLatest(ctx context.Context) *UpdateInfo {
	req, err := http.NewRequestWithContext(ctx, "GET", githubReleasesURL(), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: githubAPITimeout}
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("update check failed", "err", err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		slog.Debug("update check got non-200", "status", resp.StatusCode)
		return nil
	}

	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		slog.Debug("update check decode failed", "err", err)
		return nil
	}

	remoteVersion := strings.TrimPrefix(rel.TagName, "v")
	info := &UpdateInfo{
		CurrentVersion: uc.current,
		Version:        remoteVersion,
		ChangelogURL:   rel.HTMLURL,
	}

	if compareVersions(remoteVersion, uc.current) > 0 {
		info.Available = true
	}

	return info
}

func (s *Server) runUpdateChecker(ctx context.Context) {
	// Delay initial check to not slow startup
	select {
	case <-ctx.Done():
		return
	case <-time.After(updateInitialDelay):
	}

	for {
		info := s.updater.check(ctx)
		if info != nil && info.Available {
			slog.Info("update available", "current", info.CurrentVersion, "latest", info.Version)
			s.broadcastUpdate(info)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(updateCheckPeriod):
		}
	}
}

func (s *Server) broadcastUpdate(info *UpdateInfo) {
	data, err := json.Marshal(info)
	if err != nil {
		return
	}
	s.hub.BroadcastNamed("update-available", data)
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	info := s.updater.check(r.Context())
	if info == nil {
		writeJSON(w, http.StatusOK, UpdateInfo{Available: false, CurrentVersion: s.updater.current})
		return
	}
	writeJSON(w, http.StatusOK, info)
}
