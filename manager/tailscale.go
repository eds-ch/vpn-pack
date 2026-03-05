package main

import (
	"context"
	"log/slog"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
)

type tailscaleClient struct {
	lc *local.Client
}

func NewTailscaleControl(socketPath string) TailscaleControl {
	return &tailscaleClient{
		lc: &local.Client{
			Socket:        socketPath,
			UseSocketOnly: true,
		},
	}
}

func (t *tailscaleClient) Status(ctx context.Context) (*ipnstate.Status, error) {
	return t.lc.Status(ctx)
}

func (t *tailscaleClient) StatusWithoutPeers(ctx context.Context) (*ipnstate.Status, error) {
	return t.lc.StatusWithoutPeers(ctx)
}

func (t *tailscaleClient) EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	return t.lc.EditPrefs(ctx, mp)
}

func (t *tailscaleClient) GetPrefs(ctx context.Context) (*ipn.Prefs, error) {
	return t.lc.GetPrefs(ctx)
}

func (t *tailscaleClient) StartLoginInteractive(ctx context.Context) error {
	return t.lc.StartLoginInteractive(ctx)
}

func (t *tailscaleClient) Start(ctx context.Context, opts ipn.Options) error {
	return t.lc.Start(ctx, opts)
}

func (t *tailscaleClient) Logout(ctx context.Context) error {
	return t.lc.Logout(ctx)
}

func (t *tailscaleClient) BugReport(ctx context.Context, note string) (string, error) {
	return t.lc.BugReport(ctx, note)
}

func (t *tailscaleClient) CheckIPForwarding(ctx context.Context) error {
	return t.lc.CheckIPForwarding(ctx)
}

func (t *tailscaleClient) CurrentDERPMap(ctx context.Context) (*tailcfg.DERPMap, error) {
	return t.lc.CurrentDERPMap(ctx)
}

func (t *tailscaleClient) WatchIPNBus(ctx context.Context, mask ipn.NotifyWatchOpt) (IPNWatcher, error) {
	return t.lc.WatchIPNBus(ctx, mask)
}

func connectWithBackoff(ctx context.Context, ts TailscaleControl) error {
	delay := backoffInitial

	for {
		_, err := ts.StatusWithoutPeers(ctx)
		if err == nil {
			slog.Info("connected to tailscaled")
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		slog.Warn("tailscaled not ready, retrying", "err", err, "delay", delay)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}

		delay *= 2
		if delay > backoffMax {
			delay = backoffMax
		}
	}
}
