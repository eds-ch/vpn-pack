package main

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthTrackerInitialState(t *testing.T) {
	ht := NewHealthTracker(&mockSSEHub{})
	snap := ht.Snapshot()
	assert.Equal(t, StatusHealthy, snap.Status)
	assert.Empty(t, snap.Watchers)
	assert.Equal(t, StatusHealthy, ht.OverallStatus())
}

func TestRecordSuccess(t *testing.T) {
	ht := NewHealthTracker(&mockSSEHub{})
	ht.RecordSuccess("tailscale")

	snap := ht.Snapshot()
	require.Contains(t, snap.Watchers, "tailscale")
	w := snap.Watchers["tailscale"]
	assert.Equal(t, StatusHealthy, w.Status)
	assert.NotNil(t, w.LastSuccess)
	assert.Equal(t, 0, w.ReconnectCount)
	assert.Empty(t, w.LastError)
}

func TestRecordError(t *testing.T) {
	ht := NewHealthTracker(&mockSSEHub{})
	ht.RecordError("tailscale", fmt.Errorf("connection lost"))

	snap := ht.Snapshot()
	w := snap.Watchers["tailscale"]
	assert.Equal(t, StatusUnhealthy, w.Status)
	assert.Equal(t, "connection lost", w.LastError)
	assert.Equal(t, 1, w.ReconnectCount)

	ht.RecordError("tailscale", fmt.Errorf("still down"))
	snap = ht.Snapshot()
	assert.Equal(t, 2, snap.Watchers["tailscale"].ReconnectCount)
}

func TestSetDegraded(t *testing.T) {
	ht := NewHealthTracker(&mockSSEHub{})
	ht.SetDegraded("firewall", "key_expired")

	snap := ht.Snapshot()
	w := snap.Watchers["firewall"]
	assert.Equal(t, StatusDegraded, w.Status)
	assert.Equal(t, "key_expired", w.DegradedReason)
	assert.True(t, ht.IsDegraded("firewall"))
}

func TestClearDegraded(t *testing.T) {
	ht := NewHealthTracker(&mockSSEHub{})
	ht.SetDegraded("firewall", "key_expired")
	assert.True(t, ht.IsDegraded("firewall"))

	ht.ClearDegraded("firewall")
	assert.False(t, ht.IsDegraded("firewall"))
	assert.Equal(t, StatusHealthy, ht.Snapshot().Watchers["firewall"].Status)
}

func TestClearDegradedNoopWhenNotDegraded(t *testing.T) {
	ht := NewHealthTracker(&mockSSEHub{})
	ht.RecordSuccess("firewall")
	ht.ClearDegraded("firewall")
	assert.Equal(t, StatusHealthy, ht.Snapshot().Watchers["firewall"].Status)
}

func TestRecordSuccessClearsDegraded(t *testing.T) {
	ht := NewHealthTracker(&mockSSEHub{})
	ht.SetDegraded("firewall", "key_expired")
	ht.RecordSuccess("firewall")

	assert.False(t, ht.IsDegraded("firewall"))
	w := ht.Snapshot().Watchers["firewall"]
	assert.Equal(t, StatusHealthy, w.Status)
	assert.Empty(t, w.DegradedReason)
}

func TestOverallStatus(t *testing.T) {
	t.Run("healthy when all healthy", func(t *testing.T) {
		ht := NewHealthTracker(&mockSSEHub{})
		ht.RecordSuccess("a")
		ht.RecordSuccess("b")
		assert.Equal(t, StatusHealthy, ht.OverallStatus())
	})

	t.Run("degraded when any degraded", func(t *testing.T) {
		ht := NewHealthTracker(&mockSSEHub{})
		ht.RecordSuccess("a")
		ht.SetDegraded("b", "reason")
		assert.Equal(t, StatusDegraded, ht.OverallStatus())
	})

	t.Run("unhealthy when any unhealthy", func(t *testing.T) {
		ht := NewHealthTracker(&mockSSEHub{})
		ht.SetDegraded("a", "reason")
		ht.RecordError("b", fmt.Errorf("fail"))
		assert.Equal(t, StatusUnhealthy, ht.OverallStatus())
	})
}

func TestExponentialBackoff(t *testing.T) {
	expected := []time.Duration{
		0,
		30 * time.Second,
		1 * time.Minute,
		2 * time.Minute,
		5 * time.Minute,
		10 * time.Minute,
	}
	for i, want := range expected {
		assert.Equal(t, want, retryInterval(i), "retryInterval(%d)", i)
	}
	assert.Equal(t, 10*time.Minute, retryInterval(10), "retryInterval(10) should be capped")
	assert.Equal(t, 10*time.Minute, retryInterval(100), "retryInterval(100) should be capped")
}

func TestShouldRetry(t *testing.T) {
	t.Run("true for unknown watcher", func(t *testing.T) {
		ht := NewHealthTracker(&mockSSEHub{})
		assert.True(t, ht.ShouldRetry("unknown"))
	})

	t.Run("false when degraded", func(t *testing.T) {
		ht := NewHealthTracker(&mockSSEHub{})
		ht.SetDegraded("firewall", "reason")
		assert.False(t, ht.ShouldRetry("firewall"))
	})

	t.Run("true on first attempt (interval 0)", func(t *testing.T) {
		ht := NewHealthTracker(&mockSSEHub{})
		ht.RecordSuccess("firewall")
		assert.True(t, ht.ShouldRetry("firewall"))
	})

	t.Run("false when backoff not elapsed", func(t *testing.T) {
		ht := NewHealthTracker(&mockSSEHub{})
		ht.RecordRetryAttempt("firewall")
		assert.False(t, ht.ShouldRetry("firewall"))
	})
}

func TestRetryCount(t *testing.T) {
	ht := NewHealthTracker(&mockSSEHub{})
	assert.Equal(t, 0, ht.RetryCount("firewall"))

	ht.RecordRetryAttempt("firewall")
	assert.Equal(t, 1, ht.RetryCount("firewall"))

	ht.RecordRetryAttempt("firewall")
	assert.Equal(t, 2, ht.RetryCount("firewall"))

	ht.ResetRetries("firewall")
	assert.Equal(t, 0, ht.RetryCount("firewall"))
}

func TestBroadcastOnChange(t *testing.T) {
	var broadcasts int
	var lastEvent string
	hub := &mockSSEHub{
		broadcastNamedFn: func(event string, data []byte) {
			broadcasts++
			lastEvent = event
		},
	}
	ht := NewHealthTracker(hub)

	ht.RecordSuccess("firewall")
	assert.Equal(t, 1, broadcasts)
	assert.Equal(t, "health", lastEvent)

	ht.RecordError("firewall", fmt.Errorf("fail"))
	assert.Equal(t, 2, broadcasts)
}

func TestNoBroadcastWhenUnchanged(t *testing.T) {
	var broadcasts int
	hub := &mockSSEHub{
		broadcastNamedFn: func(event string, data []byte) {
			broadcasts++
		},
	}
	ht := NewHealthTracker(hub)

	ht.RecordSuccess("firewall")
	assert.Equal(t, 1, broadcasts)

	// Second RecordSuccess with same state — lastSuccess changes but status is same.
	// Due to time change in LastSuccess, this will broadcast again. That's expected.
	// But calling ClearDegraded on healthy watcher should NOT broadcast.
	ht.ClearDegraded("firewall")
	assert.Equal(t, 1, broadcasts, "ClearDegraded on healthy watcher should not broadcast")
}

func TestNilHubDoesNotPanic(t *testing.T) {
	ht := NewHealthTracker(nil)
	assert.NotPanics(t, func() {
		ht.RecordSuccess("test")
		ht.RecordError("test", fmt.Errorf("err"))
		ht.SetDegraded("test", "reason")
	})
}

func TestConcurrentAccess(t *testing.T) {
	ht := NewHealthTracker(&mockSSEHub{})
	var wg sync.WaitGroup

	for i := range 10 {
		wg.Add(3)
		name := fmt.Sprintf("watcher-%d", i%3)
		go func() {
			defer wg.Done()
			ht.RecordSuccess(name)
		}()
		go func() {
			defer wg.Done()
			ht.RecordError(name, fmt.Errorf("err"))
		}()
		go func() {
			defer wg.Done()
			_ = ht.Snapshot()
			_ = ht.OverallStatus()
			_ = ht.IsDegraded(name)
			_ = ht.ShouldRetry(name)
		}()
	}
	wg.Wait()
}
