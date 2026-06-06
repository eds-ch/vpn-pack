package client

import (
	"context"
	"io"
	"time"

	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"

	"unifi-tailscale/manager/domain"
)

// BoundedTailscaleControl wraps a domain.TailscaleControl and applies a
// per-call timeout to every RPC method except the intentionally long-lived
// streams (WatchIPNBus, TailDaemonLogs). It exists so any new EditPrefs /
// Status / GetPrefs / Logout / Start / etc. callsite added in the future
// inherits the deadline automatically — earlier callsites in service/*
// still apply their own config.WithTimeout for defense-in-depth, and Go
// honors the earliest deadline so the behavior is identical.
type BoundedTailscaleControl struct {
	inner   domain.TailscaleControl
	timeout time.Duration
}

func NewBoundedTailscaleControl(inner domain.TailscaleControl, timeout time.Duration) *BoundedTailscaleControl {
	return &BoundedTailscaleControl{inner: inner, timeout: timeout}
}

func (b *BoundedTailscaleControl) bound(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, b.timeout)
}

func (b *BoundedTailscaleControl) Status(ctx context.Context) (*ipnstate.Status, error) {
	cctx, cancel := b.bound(ctx)
	defer cancel()
	return b.inner.Status(cctx)
}

func (b *BoundedTailscaleControl) StatusWithoutPeers(ctx context.Context) (*ipnstate.Status, error) {
	cctx, cancel := b.bound(ctx)
	defer cancel()
	return b.inner.StatusWithoutPeers(cctx)
}

func (b *BoundedTailscaleControl) EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	cctx, cancel := b.bound(ctx)
	defer cancel()
	return b.inner.EditPrefs(cctx, mp)
}

func (b *BoundedTailscaleControl) GetPrefs(ctx context.Context) (*ipn.Prefs, error) {
	cctx, cancel := b.bound(ctx)
	defer cancel()
	return b.inner.GetPrefs(cctx)
}

func (b *BoundedTailscaleControl) StartLoginInteractive(ctx context.Context) error {
	cctx, cancel := b.bound(ctx)
	defer cancel()
	return b.inner.StartLoginInteractive(cctx)
}

func (b *BoundedTailscaleControl) Start(ctx context.Context, opts ipn.Options) error {
	cctx, cancel := b.bound(ctx)
	defer cancel()
	return b.inner.Start(cctx, opts)
}

func (b *BoundedTailscaleControl) Logout(ctx context.Context) error {
	cctx, cancel := b.bound(ctx)
	defer cancel()
	return b.inner.Logout(cctx)
}

func (b *BoundedTailscaleControl) BugReport(ctx context.Context, note string) (string, error) {
	cctx, cancel := b.bound(ctx)
	defer cancel()
	return b.inner.BugReport(cctx, note)
}

func (b *BoundedTailscaleControl) CheckIPForwarding(ctx context.Context) error {
	cctx, cancel := b.bound(ctx)
	defer cancel()
	return b.inner.CheckIPForwarding(cctx)
}

func (b *BoundedTailscaleControl) CurrentDERPMap(ctx context.Context) (*tailcfg.DERPMap, error) {
	cctx, cancel := b.bound(ctx)
	defer cancel()
	return b.inner.CurrentDERPMap(cctx)
}

// WatchIPNBus is a long-lived stream subscription — bounding it would
// truncate every notification stream after b.timeout. Pass ctx through.
func (b *BoundedTailscaleControl) WatchIPNBus(ctx context.Context, mask ipn.NotifyWatchOpt) (domain.IPNWatcher, error) {
	return b.inner.WatchIPNBus(ctx, mask)
}

// TailDaemonLogs is a long-lived log tail — same rationale as WatchIPNBus.
func (b *BoundedTailscaleControl) TailDaemonLogs(ctx context.Context) (io.Reader, error) {
	return b.inner.TailDaemonLogs(ctx)
}
