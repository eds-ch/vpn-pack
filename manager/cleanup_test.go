package main

import (
	"errors"
	"testing"
)

// TestRunCleanup_RefusesWhenManagerActive locks in the architectural
// invariant: cleanup must never race with a live manager process. The
// guard short-circuits before any UDAPI work, so the test does not need
// a UDAPI socket or root.
func TestRunCleanup_RefusesWhenManagerActive(t *testing.T) {
	orig := cleanupManagerActiveCheck
	t.Cleanup(func() { cleanupManagerActiveCheck = orig })

	cleanupManagerActiveCheck = func() bool { return true }

	err := runCleanup()
	if err == nil {
		t.Fatal("expected refusal error when manager service active")
	}
	if !errors.Is(err, errCleanupRefused) {
		t.Fatalf("expected errCleanupRefused, got %v", err)
	}
}
