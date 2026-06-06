package client

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"

	"unifi-tailscale/manager/domain"
)

// mockTC is a domain.TailscaleControl mock that records context shape on
// each method, used to assert the bounded decorator's per-call deadline.
type mockTC struct {
	statusFn         func(context.Context) (*ipnstate.Status, error)
	statusNoPeersFn  func(context.Context) (*ipnstate.Status, error)
	editPrefsFn      func(context.Context, *ipn.MaskedPrefs) (*ipn.Prefs, error)
	getPrefsFn       func(context.Context) (*ipn.Prefs, error)
	startLoginFn     func(context.Context) error
	startFn          func(context.Context, ipn.Options) error
	logoutFn         func(context.Context) error
	bugReportFn      func(context.Context, string) (string, error)
	checkIPForwardFn func(context.Context) error
	derpMapFn        func(context.Context) (*tailcfg.DERPMap, error)
	watchFn          func(context.Context, ipn.NotifyWatchOpt) (domain.IPNWatcher, error)
	tailLogsFn       func(context.Context) (io.Reader, error)
}

func (m *mockTC) Status(ctx context.Context) (*ipnstate.Status, error) {
	if m.statusFn != nil {
		return m.statusFn(ctx)
	}
	return nil, nil
}
func (m *mockTC) StatusWithoutPeers(ctx context.Context) (*ipnstate.Status, error) {
	if m.statusNoPeersFn != nil {
		return m.statusNoPeersFn(ctx)
	}
	return nil, nil
}
func (m *mockTC) EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	if m.editPrefsFn != nil {
		return m.editPrefsFn(ctx, mp)
	}
	return nil, nil
}
func (m *mockTC) GetPrefs(ctx context.Context) (*ipn.Prefs, error) {
	if m.getPrefsFn != nil {
		return m.getPrefsFn(ctx)
	}
	return nil, nil
}
func (m *mockTC) StartLoginInteractive(ctx context.Context) error {
	if m.startLoginFn != nil {
		return m.startLoginFn(ctx)
	}
	return nil
}
func (m *mockTC) Start(ctx context.Context, opts ipn.Options) error {
	if m.startFn != nil {
		return m.startFn(ctx, opts)
	}
	return nil
}
func (m *mockTC) Logout(ctx context.Context) error {
	if m.logoutFn != nil {
		return m.logoutFn(ctx)
	}
	return nil
}
func (m *mockTC) BugReport(ctx context.Context, note string) (string, error) {
	if m.bugReportFn != nil {
		return m.bugReportFn(ctx, note)
	}
	return "", nil
}
func (m *mockTC) CheckIPForwarding(ctx context.Context) error {
	if m.checkIPForwardFn != nil {
		return m.checkIPForwardFn(ctx)
	}
	return nil
}
func (m *mockTC) CurrentDERPMap(ctx context.Context) (*tailcfg.DERPMap, error) {
	if m.derpMapFn != nil {
		return m.derpMapFn(ctx)
	}
	return nil, nil
}
func (m *mockTC) WatchIPNBus(ctx context.Context, mask ipn.NotifyWatchOpt) (domain.IPNWatcher, error) {
	if m.watchFn != nil {
		return m.watchFn(ctx, mask)
	}
	return nil, nil
}
func (m *mockTC) TailDaemonLogs(ctx context.Context) (io.Reader, error) {
	if m.tailLogsFn != nil {
		return m.tailLogsFn(ctx)
	}
	return nil, nil
}

// Task 10.13 / Phase 9 follow-up: the bounded decorator must apply a per-
// call deadline to EditPrefs even when the caller passes a deadline-free
// context. Without it, any new EditPrefs callsite added without manual
// config.WithTimeout (e.g. settings.go:169) re-introduces BUG-L1.
func TestBounded_AppliesTimeoutToEditPrefs(t *testing.T) {
	inner := &mockTC{
		editPrefsFn: func(ctx context.Context, _ *ipn.MaskedPrefs) (*ipn.Prefs, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	b := NewBoundedTailscaleControl(inner, 50*time.Millisecond)

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := b.EditPrefs(context.Background(), &ipn.MaskedPrefs{})
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected DeadlineExceeded, got %v", err)
		}
		if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
			t.Fatalf("bounded EditPrefs honored timeout too late: %v", elapsed)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("bounded EditPrefs did not honor per-call timeout")
	}
}

// Streams (WatchIPNBus, TailDaemonLogs) are intentionally long-lived and
// MUST NOT be bounded — capping them would truncate every IPN notify
// subscription after b.timeout.
func TestBounded_WatchIPNBusNotBounded(t *testing.T) {
	deadlineSeen := false
	inner := &mockTC{
		watchFn: func(ctx context.Context, _ ipn.NotifyWatchOpt) (domain.IPNWatcher, error) {
			_, deadlineSeen = ctx.Deadline()
			return nil, nil
		},
	}
	b := NewBoundedTailscaleControl(inner, 50*time.Millisecond)

	_, _ = b.WatchIPNBus(context.Background(), 0)

	if deadlineSeen {
		t.Fatal("WatchIPNBus must NOT receive a per-call deadline")
	}
}

func TestBounded_TailDaemonLogsNotBounded(t *testing.T) {
	deadlineSeen := false
	inner := &mockTC{
		tailLogsFn: func(ctx context.Context) (io.Reader, error) {
			_, deadlineSeen = ctx.Deadline()
			return nil, nil
		},
	}
	b := NewBoundedTailscaleControl(inner, 50*time.Millisecond)

	_, _ = b.TailDaemonLogs(context.Background())

	if deadlineSeen {
		t.Fatal("TailDaemonLogs must NOT receive a per-call deadline")
	}
}
