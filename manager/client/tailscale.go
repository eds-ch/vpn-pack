package client

import (
	"context"
	"io"
	"log/slog"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"
)

type TailscaleClient struct {
	lc *local.Client
}

func NewTailscaleControl(socketPath string) *TailscaleClient {
	return &TailscaleClient{
		lc: &local.Client{
			Socket:        socketPath,
			UseSocketOnly: true,
		},
	}
}

func (t *TailscaleClient) Status(ctx context.Context) (*ipnstate.Status, error) {
	return t.lc.Status(ctx)
}

func (t *TailscaleClient) StatusWithoutPeers(ctx context.Context) (*ipnstate.Status, error) {
	return t.lc.StatusWithoutPeers(ctx)
}

func (t *TailscaleClient) EditPrefs(ctx context.Context, mp *ipn.MaskedPrefs) (*ipn.Prefs, error) {
	return t.lc.EditPrefs(ctx, mp)
}

func (t *TailscaleClient) GetPrefs(ctx context.Context) (*ipn.Prefs, error) {
	return t.lc.GetPrefs(ctx)
}

func (t *TailscaleClient) StartLoginInteractive(ctx context.Context) error {
	return t.lc.StartLoginInteractive(ctx)
}

func (t *TailscaleClient) Start(ctx context.Context, opts ipn.Options) error {
	return t.lc.Start(ctx, opts)
}

func (t *TailscaleClient) Logout(ctx context.Context) error {
	return t.lc.Logout(ctx)
}

func (t *TailscaleClient) BugReport(ctx context.Context, note string) (string, error) {
	return t.lc.BugReport(ctx, note)
}

func (t *TailscaleClient) CheckIPForwarding(ctx context.Context) error {
	return t.lc.CheckIPForwarding(ctx)
}

func (t *TailscaleClient) CurrentDERPMap(ctx context.Context) (*tailcfg.DERPMap, error) {
	return t.lc.CurrentDERPMap(ctx)
}

func (t *TailscaleClient) WatchIPNBus(ctx context.Context, mask ipn.NotifyWatchOpt) (domain.IPNWatcher, error) {
	return t.lc.WatchIPNBus(ctx, mask)
}

func (t *TailscaleClient) TailDaemonLogs(ctx context.Context) (io.Reader, error) {
	return t.lc.TailDaemonLogs(ctx)
}

func ConnectWithBackoff(ctx context.Context, ts domain.TailscaleControl) error {
	delay := config.BackoffInitial

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
		if delay > config.BackoffMax {
			delay = config.BackoffMax
		}
	}
}
