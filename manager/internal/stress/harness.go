// Package stress provides a tiny goroutine stress harness used in race tests.
package stress

import (
	"sync"
	"testing"
)

// Run launches `goroutines` goroutines, each calling fn `iterationsPerG` times.
// All goroutines start in parallel after a sync.WaitGroup barrier so the race
// detector has maximum interleaving opportunity.
func Run(t testing.TB, goroutines, iterationsPerG int, fn func(g int)) {
	t.Helper()
	var start, done sync.WaitGroup
	start.Add(1)
	done.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer done.Done()
			start.Wait()
			for i := 0; i < iterationsPerG; i++ {
				fn(g)
			}
		}()
	}
	start.Done()
	done.Wait()
}
