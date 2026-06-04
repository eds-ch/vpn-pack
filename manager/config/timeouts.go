package config

import (
	"context"
	"time"
)

// Per-call timeouts. Declared as vars (not consts) so tests can shrink them
// to exercise the timeout-fired path without waiting the full duration.
var (
	TailscaleLocalAPITimeout = 10 * time.Second
	SubprocessTimeout        = 15 * time.Second
	UDAPIDefaultTimeout      = 10 * time.Second
)

func WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}
