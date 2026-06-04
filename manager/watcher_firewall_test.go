package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/internal/stress"
	"unifi-tailscale/manager/service"
)

func TestCheckAndRestoreRulesDeduplication(t *testing.T) {
	orig := interfaceExistsFunc
	interfaceExistsFunc = func(string) bool { return true }
	t.Cleanup(func() { interfaceExistsFunc = orig })

	var concurrent atomic.Int32
	var maxConcurrent atomic.Int32

	fw := &mockFirewallService{
		integrationReadyFn: func() bool { return true },
		checkTailscaleRulesPresentFn: func(ctx context.Context) (bool, bool, bool, bool) {
			cur := concurrent.Add(1)
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond)
			concurrent.Add(-1)
			return true, true, true, true
		},
	}
	manifest := &mockManifestStore{
		getTailscaleZoneFn: func() ZoneManifest { return ZoneManifest{ZoneID: "z1"} },
	}
	s := newTestServer(func(s *Server) {
		s.fw = fw
		s.manifest = manifest
	})

	ctx := context.Background()
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.checkAndRestoreRules(ctx)
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), maxConcurrent.Load(), "only one concurrent execution should happen")
}

func TestRetryLoop(t *testing.T) {
	var callCount int
	fn := func(context.Context) error {
		callCount++
		return nil
	}

	ctx := context.Background()
	retryLoop(ctx, 3, 10*time.Millisecond, fn)
	assert.Equal(t, 1, callCount, "should stop after first success")
}

func TestRetryLoopContextCancel(t *testing.T) {
	var callCount int
	fn := func(context.Context) error {
		callCount++
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	retryLoop(ctx, 5, 10*time.Millisecond, fn)
	assert.Equal(t, 1, callCount, "should stop after context cancel")
}

func TestRetryLoopContinuesOnError(t *testing.T) {
	var callCount int
	fn := func(context.Context) error {
		callCount++
		return fmt.Errorf("transient error")
	}

	ctx := context.Background()
	retryLoop(ctx, 3, 10*time.Millisecond, fn)
	assert.Equal(t, 3, callCount, "should retry all attempts even on errors")
}

// TestSetupTailscaleFirewall_ConcurrencyGuard verifies that the restoring CAS
// serialises SetupTailscaleFirewall callers so concurrent paths (SIGHUP
// reapply, OnKeyConfigured, repairMissingPolicies, boot apply) cannot run the
// inner setup in parallel and produce duplicate UDAPI zone-create attempts.
// BUG-M6.
func TestSetupTailscaleFirewall_ConcurrencyGuard(t *testing.T) {
	s := &Server{}

	var concurrent atomic.Int64
	var maxConcurrent atomic.Int64

	stress.Run(t, 8, 50, func(int) {
		s.guardedSetup(func() {
			cur := concurrent.Add(1)
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Microsecond)
			concurrent.Add(-1)
		})
	})

	if got := maxConcurrent.Load(); got != 1 {
		t.Fatalf("expected guard to serialize callers (max concurrency 1); got %d", got)
	}
	if s.restoring.Load() {
		t.Fatal("restoring flag leaked: still true after all goroutines completed")
	}
}

// TestIntegrationNotifier_OnKeyConfiguredUsesGuard verifies the adapter routes
// SetupTailscaleFirewall through the injected guard rather than calling
// FirewallOrchestrator directly. Regression guard for BUG-M6: if a refactor
// reintroduces the raw call, this test fires.
func TestIntegrationNotifier_OnKeyConfiguredUsesGuard(t *testing.T) {
	var guardCalls atomic.Int64
	guard := func(context.Context) (*service.SetupResult, bool) {
		guardCalls.Add(1)
		return &service.SetupResult{}, true
	}

	adapter := &integrationNotifierAdapter{
		fwOrch:       &service.FirewallOrchestrator{}, // any non-nil pointer
		guardedSetup: guard,
		health:       NewHealthTracker(&mockSSEHub{}),
		state:        domain.NewTailscaleState(),
		broadcast:    func() {},
		openWanPort:  func(context.Context) {},
	}

	adapter.OnKeyConfigured(context.Background(), &service.IntegrationStatus{SiteID: "site-1"})

	if got := guardCalls.Load(); got != 1 {
		t.Fatalf("expected OnKeyConfigured to call guardedSetup exactly once; got %d", got)
	}
}

// TestGuardedSetup_RejectsWhileHeld verifies a second caller arriving while
// the guard is held bails out with ran=false instead of waiting or running.
func TestGuardedSetup_RejectsWhileHeld(t *testing.T) {
	s := &Server{}

	enter := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})

	go func() {
		s.guardedSetup(func() {
			close(enter)
			<-release
		})
		close(done)
	}()

	<-enter
	if s.guardedSetup(func() {}) {
		t.Fatal("expected guardedSetup to return false while the CAS is held")
	}
	close(release)
	<-done
}
