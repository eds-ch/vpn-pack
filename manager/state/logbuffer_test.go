package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// BUG-L7: NewLogBuffer(0) allocated a zero-length entries slice and Add then
// indexed entries[0] -- panic. Worse, lb.head = (head+1) % 0 dividing by
// zero. Clamp the size so the buffer is always usable.
func TestNewLogBuffer_ClampsZeroAndNegative(t *testing.T) {
	for _, ms := range []int{0, -1, -1000} {
		ms := ms
		t.Run("clamp", func(t *testing.T) {
			lb := NewLogBuffer(ms)
			assert.NotPanics(t, func() {
				lb.Add(NewLogEntry("info", "hello", ""))
				lb.Add(NewLogEntry("info", "world", ""))
				_ = lb.Snapshot()
			})
		})
	}
}

func TestLogBuffer_RoundTripPreservesEntriesAndIsLIFO(t *testing.T) {
	lb := NewLogBuffer(8)
	for _, m := range []string{"a", "b", "c", "d"} {
		lb.Add(NewLogEntry("info", m, ""))
	}

	snap := lb.Snapshot()
	assert.Len(t, snap, 4)
	assert.Equal(t, "d", snap[0].Message)
	assert.Equal(t, "c", snap[1].Message)
	assert.Equal(t, "b", snap[2].Message)
	assert.Equal(t, "a", snap[3].Message)
}
