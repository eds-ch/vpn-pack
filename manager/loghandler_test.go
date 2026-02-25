package main

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
