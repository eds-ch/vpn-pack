package main

import (
	"errors"
	"strings"
	"testing"

	"unifi-tailscale/manager/service"
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

// SEC-B1: subnetValidatorProvider must refuse tunnel creation when the
// underlying interface enumeration fails. Without this, a transient netlink
// error would silently downgrade validation to "no conflicts" and a tunnel
// that overlaps a local /24 would be accepted.
func TestSubnetValidatorProvider_FailsClosedOnCollectError(t *testing.T) {
	orig := collectSystemSubnetsHook
	t.Cleanup(func() { collectSystemSubnetsHook = orig })
	collectSystemSubnetsHook = func(...string) (*service.SystemSubnets, error) {
		return nil, errors.New("interfaces unavailable")
	}

	warnings, blocks := subnetValidatorProvider([]string{"10.0.0.0/24"})
	if len(blocks) == 0 {
		t.Fatal("expected synthetic block on collection failure")
	}
	if !strings.Contains(blocks[0].Message, "subnet validation unavailable") {
		t.Fatalf("block message: %q", blocks[0].Message)
	}
	_ = warnings
}
