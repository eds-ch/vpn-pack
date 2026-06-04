package main

import (
	"strings"
	"testing"
)

// Direct LogBuffer writes (LogWarn, used by wg-s2s) bypass the slog
// pipeline and the logredact handler. Before the fix, an upstream
// error containing a tskey-auth-* literal would land in /api/logs
// verbatim.
func TestWgS2sLogAdapterRedactsSecrets(t *testing.T) {
	buf := NewLogBuffer(10)
	a := &wgS2sLogAdapter{buf: buf}

	a.LogWarn("firewall rules failed iface=wg0 err=auth tskey-auth-kFoo123-CafeDeadBeef rejected")

	snap := buf.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(snap))
	}
	if strings.Contains(snap[0].Message, "CafeDeadBeef") {
		t.Fatalf("LogWarn leaked secret to LogBuffer: %q", snap[0].Message)
	}
	if !strings.Contains(snap[0].Message, "tskey-auth-***") {
		t.Fatalf("LogWarn missing redaction marker: %q", snap[0].Message)
	}
}
