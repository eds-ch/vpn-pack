package main

import (
	"encoding/json"
	"sync"
	"time"
)

type WatcherStatus string

const (
	StatusHealthy   WatcherStatus = "healthy"
	StatusDegraded  WatcherStatus = "degraded"
	StatusUnhealthy WatcherStatus = "unhealthy"
)

type WatcherHealth struct {
	Status         WatcherStatus `json:"status"`
	LastSuccess    *time.Time    `json:"lastSuccess,omitempty"`
	ReconnectCount int           `json:"reconnects"`
	LastError      string        `json:"error,omitempty"`
	DegradedReason string        `json:"degradedReason,omitempty"`
}

type HealthSnapshot struct {
	Status   WatcherStatus            `json:"status"`
	Watchers map[string]WatcherHealth `json:"watchers"`
}

type watcherEntry struct {
	status         WatcherStatus
	lastSuccess    time.Time
	reconnectCount int
	lastError      string
	degradedReason string
	lastRetry      time.Time
}

type HealthTracker struct {
	mu       sync.RWMutex
	watchers map[string]*watcherEntry
	hub      SSEHub
	last     []byte
}

func NewHealthTracker(hub SSEHub) *HealthTracker {
	return &HealthTracker{
		watchers: make(map[string]*watcherEntry),
		hub:      hub,
	}
}

func (ht *HealthTracker) RecordSuccess(name string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	e := ht.getOrCreate(name)
	e.status = StatusHealthy
	e.lastSuccess = time.Now()
	e.lastError = ""
	e.degradedReason = ""
	e.reconnectCount = 0
	ht.broadcastIfChanged()
}

func (ht *HealthTracker) RecordError(name string, err error) {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	e := ht.getOrCreate(name)
	e.status = StatusUnhealthy
	e.lastError = err.Error()
	e.reconnectCount++
	ht.broadcastIfChanged()
}

func (ht *HealthTracker) SetDegraded(name string, reason string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	e := ht.getOrCreate(name)
	e.status = StatusDegraded
	e.degradedReason = reason
	ht.broadcastIfChanged()
}

func (ht *HealthTracker) ClearDegraded(name string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	e, ok := ht.watchers[name]
	if !ok || e.status != StatusDegraded {
		return
	}
	e.status = StatusHealthy
	e.degradedReason = ""
	ht.broadcastIfChanged()
}

func (ht *HealthTracker) IsDegraded(name string) bool {
	ht.mu.RLock()
	defer ht.mu.RUnlock()

	e, ok := ht.watchers[name]
	return ok && e.status == StatusDegraded
}

func (ht *HealthTracker) ShouldRetry(name string) bool {
	ht.mu.RLock()
	defer ht.mu.RUnlock()

	e, ok := ht.watchers[name]
	if !ok {
		return true
	}
	if e.status == StatusDegraded {
		return false
	}
	iv := retryInterval(e.reconnectCount)
	return iv == 0 || time.Since(e.lastRetry) >= iv
}

func (ht *HealthTracker) RecordRetryAttempt(name string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	e := ht.getOrCreate(name)
	e.lastRetry = time.Now()
	e.reconnectCount++
}

func (ht *HealthTracker) ResetRetries(name string) {
	ht.mu.Lock()
	defer ht.mu.Unlock()

	if e, ok := ht.watchers[name]; ok {
		e.reconnectCount = 0
	}
}

func (ht *HealthTracker) RetryCount(name string) int {
	ht.mu.RLock()
	defer ht.mu.RUnlock()

	if e, ok := ht.watchers[name]; ok {
		return e.reconnectCount
	}
	return 0
}

func (ht *HealthTracker) Snapshot() HealthSnapshot {
	ht.mu.RLock()
	defer ht.mu.RUnlock()
	return ht.snapshotLocked()
}

func (ht *HealthTracker) OverallStatus() WatcherStatus {
	ht.mu.RLock()
	defer ht.mu.RUnlock()

	worst := StatusHealthy
	for _, e := range ht.watchers {
		if e.status == StatusUnhealthy {
			return StatusUnhealthy
		}
		if e.status == StatusDegraded {
			worst = StatusDegraded
		}
	}
	return worst
}

func (ht *HealthTracker) getOrCreate(name string) *watcherEntry {
	e, ok := ht.watchers[name]
	if !ok {
		e = &watcherEntry{status: StatusHealthy}
		ht.watchers[name] = e
	}
	return e
}

func (ht *HealthTracker) snapshotLocked() HealthSnapshot {
	s := HealthSnapshot{
		Status:   StatusHealthy,
		Watchers: make(map[string]WatcherHealth, len(ht.watchers)),
	}
	for name, e := range ht.watchers {
		wh := WatcherHealth{
			Status:         e.status,
			ReconnectCount: e.reconnectCount,
			LastError:      e.lastError,
			DegradedReason: e.degradedReason,
		}
		if !e.lastSuccess.IsZero() {
			t := e.lastSuccess
			wh.LastSuccess = &t
		}
		s.Watchers[name] = wh

		if e.status == StatusUnhealthy {
			s.Status = StatusUnhealthy
		} else if e.status == StatusDegraded && s.Status != StatusUnhealthy {
			s.Status = StatusDegraded
		}
	}
	return s
}

func (ht *HealthTracker) broadcastIfChanged() {
	snap := ht.snapshotLocked()
	data, err := json.Marshal(snap)
	if err != nil {
		return
	}

	if ht.last != nil && string(data) == string(ht.last) {
		return
	}
	ht.last = data

	if ht.hub != nil {
		ht.hub.BroadcastNamed("health", data)
	}
}

func retryInterval(count int) time.Duration {
	intervals := [...]time.Duration{
		0,
		30 * time.Second,
		1 * time.Minute,
		2 * time.Minute,
		5 * time.Minute,
		10 * time.Minute,
	}
	if count >= len(intervals) {
		return intervals[len(intervals)-1]
	}
	return intervals[count]
}
