package main

import (
	"log/slog"

	"unifi-tailscale/manager/state"
)

// sweepStartupOrphanTmps removes leftover *.tmp files from durable state
// directories before any load. Errors are logged but not fatal — a failed
// sweep is preferable to refusing to start.
func sweepStartupOrphanTmps(dirs []string) {
	for _, d := range dirs {
		if err := state.SweepOrphanTmp(d); err != nil {
			slog.Warn("orphan tmp sweep failed", "dir", d, "err", err)
		}
	}
}
