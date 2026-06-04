package stress

import (
	"sync/atomic"
	"testing"
)

func TestRunStartsAllGoroutines(t *testing.T) {
	var n atomic.Int64
	Run(t, 8, 100, func(_ int) { n.Add(1) })
	if got := n.Load(); got != 800 {
		t.Fatalf("ran %d ops, want 800", got)
	}
}
