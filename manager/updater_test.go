package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"unifi-tailscale/manager/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"equal", "1.0.0", "1.0.0", 0},
		{"newer patch", "1.0.1", "1.0.0", 1},
		{"older patch", "1.0.0", "1.0.1", -1},
		{"newer minor", "1.1.0", "1.0.0", 1},
		{"newer major", "2.0.0", "1.99.99", 1},
		{"different lengths equal", "1.0", "1.0.0", 0},
		{"different lengths newer", "1.0.1", "1.0", 1},
		{"empty a", "", "1.0.0", 0},
		{"empty b", "1.0.0", "", 0},
		{"both empty", "", "", 0},
		{"dev a", "dev", "1.0.0", 0},
		{"dev b", "1.0.0", "dev", 0},
		{"real world newer", "1.95.0", "1.94.0", 1},
		{"real world equal", "1.95.0", "1.95.0", 0},
		{"real world downgrade", "1.94.0", "1.95.0", -1},
		{"stable newer than beta same base", "1.4.0", "1.4.0-beta.1", 1},
		{"beta older than stable same base", "1.4.0-beta.1", "1.4.0", -1},
		{"beta newer than old stable", "1.4.0-beta.1", "1.3.1", 1},
		{"old stable older than beta", "1.3.1", "1.4.0-beta.1", -1},
		{"equal betas", "1.4.0-beta.1", "1.4.0-beta.1", 0},
		{"both stable equal", "1.4.0", "1.4.0", 0},
		// BUG-L5: a user on a pre-release must still see upgrades to
		// newer pre-releases of the same base. The old comparator
		// returned 0 for any pair of pre-releases on the same base, so
		// 1.5.0-beta.3 -> 1.5.0-beta.4 was silently invisible.
		{"newer beta same base", "1.5.0-beta.4", "1.5.0-beta.3", 1},
		{"older beta same base", "1.5.0-beta.3", "1.5.0-beta.4", -1},
		{"rc newer than beta same base", "1.5.0-rc.1", "1.5.0-beta.3", 1},
		{"stable newer than rc same base", "1.5.0", "1.5.0-rc.1", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, compareVersions(tt.a, tt.b))
		})
	}
}

// BUG-L6: when GitHub returns 5xx (or network errors), the updater used
// to retry every call — a tight retry loop hits GitHub's anonymous rate
// limit in minutes. Cache the failure for UpdateFailCacheTTL so we make
// at most one upstream attempt per TTL window.
func TestUpdater_CachesFailedCheck(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	origURL := githubReleasesURLHook
	t.Cleanup(func() { githubReleasesURLHook = origURL })
	githubReleasesURLHook = func() string { return srv.URL }

	uc := &updateChecker{
		current:    "1.0.0",
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}

	for i := 0; i < 5; i++ {
		info := uc.check(context.Background())
		require.NotNil(t, info)
		require.False(t, info.Available)
	}

	require.Equal(t, int32(1), atomic.LoadInt32(&hits),
		"5 calls within UpdateFailCacheTTL should produce 1 upstream hit, got %d", hits)
	_ = config.UpdateFailCacheTTL
}

// A failed refresh of an expired cache must not downgrade a known-available
// update to "no update": the UI asks on every page load, and one GitHub
// outage would otherwise make the banner disappear until the next success.
func TestUpdater_KeepsLastKnownInfoWhenRefreshFails(t *testing.T) {
	var fail atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"tag_name":"v2.0.0","html_url":"https://example.test/rel"}`))
	}))
	t.Cleanup(srv.Close)

	origURL := githubReleasesURLHook
	t.Cleanup(func() { githubReleasesURLHook = origURL })
	githubReleasesURLHook = func() string { return srv.URL }

	uc := &updateChecker{
		current:    "1.0.0",
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}

	info := uc.check(context.Background())
	require.True(t, info.Available)
	require.Equal(t, "2.0.0", info.Version)

	// Expire both the success cache and the fail cache, then break upstream.
	uc.mu.Lock()
	uc.checkedAt = time.Now().Add(-config.UpdateCheckPeriod - time.Minute)
	uc.mu.Unlock()
	fail.Store(true)

	stale := uc.check(context.Background())
	require.True(t, stale.Available, "known update must survive a failed refresh")
	assert.Equal(t, "2.0.0", stale.Version)

	// The fail-cache path must report the same thing.
	cached := uc.check(context.Background())
	assert.True(t, cached.Available)
	assert.Equal(t, "2.0.0", cached.Version)
}

// The fetch is shared through singleflight, so it must not inherit the
// caller's context: one browser aborting /api/update-check would otherwise
// cancel the check for everyone waiting on it.
func TestUpdater_CheckIgnoresCallerCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v2.0.0","html_url":"https://example.test/rel"}`))
	}))
	t.Cleanup(srv.Close)

	origURL := githubReleasesURLHook
	t.Cleanup(func() { githubReleasesURLHook = origURL })
	githubReleasesURLHook = func() string { return srv.URL }

	uc := &updateChecker{
		current:    "1.0.0",
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	info := uc.check(ctx)
	require.True(t, info.Available)
	assert.Equal(t, "2.0.0", info.Version)
}
