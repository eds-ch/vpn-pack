package config

import (
	"context"
	"time"
)

// Per-call timeouts. The Tailscale localapi cap is enforced once at the
// composition root by client.NewBoundedTailscaleControl (Task 10.13), so
// these are consts again — tests that need a shorter bound construct
// their own bounded wrapper instead of mutating a global.
const (
	TailscaleLocalAPITimeout = 10 * time.Second
	SubprocessTimeout        = 15 * time.Second
	UDAPIDefaultTimeout      = 10 * time.Second
)

func WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}
