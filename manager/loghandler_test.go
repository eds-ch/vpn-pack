package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"unifi-tailscale/manager/logredact"
)

func TestBufferHandlerEnabled(t *testing.T) {
	buf := NewLogBuffer(10)
	h := newBufferHandler(buf, "test", nil)

	tests := []struct {
		level slog.Level
		want  bool
	}{
		{slog.LevelDebug, false},
		{slog.LevelInfo, true},
		{slog.LevelWarn, true},
		{slog.LevelError, true},
	}

	for _, tt := range tests {
		t.Run(tt.level.String(), func(t *testing.T) {
			assert.Equal(t, tt.want, h.Enabled(context.Background(), tt.level))
		})
	}
}

func TestBufferHandlerHandle(t *testing.T) {
	buf := NewLogBuffer(10)
	h := newBufferHandler(buf, "manager", nil)

	now := time.Now()
	r := slog.NewRecord(now, slog.LevelWarn, "something happened", 0)
	err := h.Handle(context.Background(), r)
	require.NoError(t, err)

	snap := buf.Snapshot()
	require.Len(t, snap, 1)
	assert.Equal(t, "warn", snap[0].Level)
	assert.Equal(t, "something happened", snap[0].Message)
	assert.Equal(t, "manager", snap[0].Source)
}

func TestBufferHandlerHandleSkipsDebug(t *testing.T) {
	buf := NewLogBuffer(10)
	h := newBufferHandler(buf, "test", nil)
	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Fatal("Debug should not be enabled")
	}
	assert.Empty(t, buf.Snapshot())
}

// Verifies that wrapping bufferHandler with logredact.Wrap causes the
// LogBuffer (the source for /api/logs) to store redacted text, not raw
// secrets. Catches the regression where wiring puts logredact behind the
// bufferHandler and only the stderr chain gets sanitised.
func TestBufferHandlerThroughLogredactRedactsLogBuffer(t *testing.T) {
	buf := NewLogBuffer(10)
	h := logredact.Wrap(newBufferHandler(buf, "test", nil))
	l := slog.New(h)
	l.Info("login url tskey-auth-kFoo123-CafeDeadBeef", "key", "tskey-auth-kFoo123-CafeDeadBeef")

	snap := buf.Snapshot()
	require.Len(t, snap, 1)
	if strings.Contains(snap[0].Message, "CafeDeadBeef") {
		t.Fatalf("LogBuffer leaked secret: %q", snap[0].Message)
	}
	if !strings.Contains(snap[0].Message, "tskey-auth-***") {
		t.Fatalf("LogBuffer missing redaction marker: %q", snap[0].Message)
	}
}
