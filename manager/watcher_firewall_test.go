package main

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCheckAndRestoreRulesDeduplication(t *testing.T) {
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
