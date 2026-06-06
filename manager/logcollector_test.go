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
