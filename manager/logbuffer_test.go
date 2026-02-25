package main

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogBuffer(t *testing.T) {
	t.Run("empty buffer returns empty slice", func(t *testing.T) {
		buf := NewLogBuffer(10)
		snap := buf.Snapshot()
		require.NotNil(t, snap)
		assert.Len(t, snap, 0)
	})

	t.Run("single entry", func(t *testing.T) {
		buf := NewLogBuffer(10)
		buf.Add(logEntry{Timestamp: "t1", Level: "info", Message: "hello"})
		snap := buf.Snapshot()
		require.Len(t, snap, 1)
		assert.Equal(t, "hello", snap[0].Message)
	})

	t.Run("snapshot returns newest first", func(t *testing.T) {
		buf := NewLogBuffer(10)
		buf.Add(logEntry{Message: "first"})
		buf.Add(logEntry{Message: "second"})
		buf.Add(logEntry{Message: "third"})
		snap := buf.Snapshot()
		require.Len(t, snap, 3)
		assert.Equal(t, "third", snap[0].Message)
		assert.Equal(t, "second", snap[1].Message)
		assert.Equal(t, "first", snap[2].Message)
	})

	t.Run("ring eviction at capacity", func(t *testing.T) {
		buf := NewLogBuffer(3)
		buf.Add(logEntry{Message: "a"})
		buf.Add(logEntry{Message: "b"})
		buf.Add(logEntry{Message: "c"})
		buf.Add(logEntry{Message: "d"})
		snap := buf.Snapshot()
		require.Len(t, snap, 3)
		assert.Equal(t, "d", snap[0].Message)
		assert.Equal(t, "c", snap[1].Message)
		assert.Equal(t, "b", snap[2].Message)
	})

	t.Run("maxSize 1", func(t *testing.T) {
		buf := NewLogBuffer(1)
		buf.Add(logEntry{Message: "a"})
		buf.Add(logEntry{Message: "b"})
		snap := buf.Snapshot()
		require.Len(t, snap, 1)
		assert.Equal(t, "b", snap[0].Message)
	})

	t.Run("concurrent Add no panic", func(t *testing.T) {
		buf := NewLogBuffer(50)
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				buf.Add(logEntry{Message: fmt.Sprintf("msg-%d", n)})
			}(i)
		}
		wg.Wait()
		snap := buf.Snapshot()
		assert.Equal(t, 50, len(snap))
	})

	t.Run("concurrent Add and Snapshot no race", func(t *testing.T) {
		buf := NewLogBuffer(50)
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				buf.Add(logEntry{Message: fmt.Sprintf("msg-%d", n)})
				_ = buf.Snapshot()
			}(i)
		}
		wg.Wait()

		snap := buf.Snapshot()
		assert.LessOrEqual(t, len(snap), 50, "snapshot should not exceed capacity")
		for i, entry := range snap {
			assert.NotEmpty(t, entry.Message, "entry %d has empty Message", i)
		}
	})
}
