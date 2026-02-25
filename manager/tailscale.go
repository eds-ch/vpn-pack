package main

import (
	"context"
	"log/slog"
	"time"

	"tailscale.com/client/local"
)

func connectWithBackoff(ctx context.Context, lc *local.Client) error {
	delay := backoffInitial

	for {
		_, err := lc.StatusWithoutPeers(ctx)
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
