package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"unifi-tailscale/manager/state"
)

// BUG-M14: tailscaled occasionally emits log lines larger than 64 KiB
// (the bufio.Scanner default). When that happens the collector returns
// `bufio.Scanner: token too long`, drops the rest of the stream, and
// reconnects — leaking log records. A 4 MiB buffer covers observed
// extremes.
func TestTailLogsAcceptsOneMegabyteLine(t *testing.T) {
	const bigLen = 1 << 20 // 1 MiB
	big := strings.Repeat("x", bigLen)
	line := `{"text":"` + big + `","logtail":{"client_time":"2026-06-04T00:00:00Z"}}` + "\n"

	mock := &mockTailscaleControl{
		tailDaemonLogsFn: func(ctx context.Context) (io.Reader, error) {
			return strings.NewReader(line), nil
		},
	}

	buf := state.NewLogBuffer(10)
	err := tailLogs(context.Background(), mock, buf)
	if err != nil {
		t.Fatalf("tailLogs returned error on 1 MiB line: %v", err)
	}

	snap := buf.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("expected one log entry, got %d", len(snap))
	}
	if len(snap[0].Message) != bigLen {
		t.Fatalf("entry truncated: got %d bytes, want %d", len(snap[0].Message), bigLen)
	}
}

// tailLogs must route both parsed tailscaled text and raw (unparseable)
// lines through logredact before buffering. Without the RedactString wrap
// an auth key emitted by tailscaled leaks verbatim into the in-memory log
// buffer served to the UI.
func TestTailLogsRedactsSecrets(t *testing.T) {
	const secret = "CafeDeadBeef"
	jsonLine := `{"text":"authkey accepted tskey-auth-kFoo123-` + secret + `","logtail":{"client_time":"2026-07-18T00:00:00Z"}}`
	rawLine := "plain log tskey-auth-kBar456-" + secret + " trailing"

	mock := &mockTailscaleControl{
		tailDaemonLogsFn: func(ctx context.Context) (io.Reader, error) {
			return strings.NewReader(jsonLine + "\n" + rawLine + "\n"), nil
		},
	}
	buf := state.NewLogBuffer(16)

	if err := tailLogs(context.Background(), mock, buf); err != nil {
		t.Fatalf("tailLogs returned error: %v", err)
	}

	entries := buf.Snapshot()
	if len(entries) != 2 {
		t.Fatalf("expected 2 buffered entries, got %d", len(entries))
	}
	for _, e := range entries {
		if strings.Contains(e.Message, secret) {
			t.Fatalf("secret leaked into log buffer: %q", e.Message)
		}
		if !strings.Contains(e.Message, "tskey-auth-***") {
			t.Fatalf("expected redaction marker, got: %q", e.Message)
		}
	}
}
