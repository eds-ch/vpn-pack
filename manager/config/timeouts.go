package config

import (
	"context"
	"time"
)

const (
	TailscaleLocalAPITimeout = 10 * time.Second
	SubprocessTimeout        = 15 * time.Second
	UDAPIDefaultTimeout      = 10 * time.Second
)

func WithTimeout(parent context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, d)
}
