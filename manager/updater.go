package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"

	"golang.org/x/mod/semver"
	"golang.org/x/sync/singleflight"
)

func githubReleasesURL() string {
	return "https://api.github.com/repos/" + config.GithubRepo + "/releases/latest"
}

// githubReleasesURLHook is overridden in tests to point at httptest.Server.
var githubReleasesURLHook = githubReleasesURL

type updateChecker struct {
	mu          sync.Mutex
	info        *UpdateInfo
	checkedAt   time.Time
	failedAt    time.Time
	current     string
	httpClient  *http.Client
	sf          singleflight.Group
}

func newUpdateChecker() *updateChecker {
	current := config.Version
	if current == "dev" {
		current = readVersionFile()
	}
	return &updateChecker{
		current:    current,
		httpClient: &http.Client{Timeout: config.GithubAPITimeout},
	}
}

func readVersionFile() string {
	return readFileTrimmed(config.VersionFilePath)
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func (uc *updateChecker) check(ctx context.Context) *UpdateInfo {
	uc.mu.Lock()
	if uc.info != nil && time.Since(uc.checkedAt) < config.UpdateCheckPeriod {
		info := uc.info
		uc.mu.Unlock()
		return info
	}
	// BUG-L6: if the last attempt failed within the fail-cache TTL,
	// short-circuit so a 5xx storm cannot stampede GitHub's rate limit.
	if !uc.failedAt.IsZero() && time.Since(uc.failedAt) < config.UpdateFailCacheTTL {
		uc.mu.Unlock()
		return &UpdateInfo{Available: false, CurrentVersion: uc.current}
	}
	uc.mu.Unlock()

	v, _, _ := uc.sf.Do("check", func() (any, error) {
		info := uc.fetchLatest(ctx)
		uc.mu.Lock()
		if info == nil {
			uc.failedAt = time.Now()
		} else {
			uc.info = info
			uc.checkedAt = time.Now()
			uc.failedAt = time.Time{}
		}
		uc.mu.Unlock()
		return info, nil
	})

	info, _ := v.(*UpdateInfo)
	if info == nil {
		return &UpdateInfo{Available: false, CurrentVersion: uc.current}
	}
	return info
}

// compareVersions returns -1, 0, +1 for a<b, a==b, a>b. Sentinel inputs
// ("", "dev") compare as equal so the updater never proposes a "downgrade"
// from a local dev build. Otherwise we delegate to golang.org/x/mod/semver
// which implements full SemVer 2.0 pre-release ordering (1.5.0-beta.3 <
// 1.5.0-beta.4 < 1.5.0-rc.1 < 1.5.0).
func compareVersions(a, b string) int {
	if a == "" || b == "" || a == "dev" || b == "dev" {
		return 0
	}
	return semver.Compare(canonSemver(a), canonSemver(b))
}

// canonSemver prefixes the leading "v" expected by golang.org/x/mod/semver.
// If the input has fewer than 3 dotted base components (e.g. "1.5"), pad
// with ".0" so semver.IsValid accepts it; otherwise semver.Compare returns 0
// for every comparison.
func canonSemver(v string) string {
	v = strings.TrimPrefix(v, "v")
	base, pre, _ := strings.Cut(v, "-")
	dots := strings.Count(base, ".")
	for ; dots < 2; dots++ {
		base += ".0"
	}
	if pre != "" {
		return "v" + base + "-" + pre
	}
	return "v" + base
}

func (uc *updateChecker) fetchLatest(ctx context.Context) *UpdateInfo {
	req, err := http.NewRequestWithContext(ctx, "GET", githubReleasesURLHook(), nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := uc.httpClient.Do(req)
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
	case <-time.After(config.UpdateInitialDelay):
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
		case <-time.After(config.UpdateCheckPeriod):
		}
	}
}

func (s *Server) broadcastUpdate(info *UpdateInfo) {
	domain.BroadcastEvent(s.hub, "update-available", info)
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	info := s.updater.check(r.Context())
	if info == nil {
		writeJSON(w, http.StatusOK, UpdateInfo{Available: false, CurrentVersion: s.updater.current})
		return
	}
	writeJSON(w, http.StatusOK, info)
}
