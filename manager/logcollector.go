package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"time"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/state"
)

func runLogCollector(ctx context.Context, ts TailscaleControl, buf *state.LogBuffer) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := tailLogs(ctx, ts, buf); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("log collector disconnected, reconnecting", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(config.LogReconnectDelay):
		}
	}
}

func tailLogs(ctx context.Context, ts TailscaleControl, buf *state.LogBuffer) error {
	reader, err := ts.TailDaemonLogs(ctx)
	if err != nil {
		return err
	}
	if rc, ok := reader.(io.ReadCloser); ok {
		defer func() { _ = rc.Close() }()
	}

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var raw struct {
			Text    string `json:"text"`
			Logtail struct {
				ClientTime string `json:"client_time"`
			} `json:"logtail"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			buf.Add(state.NewLogEntry("info", line, ""))
			continue
		}

		text := strings.TrimSpace(raw.Text)
		if text == "" || text == "[logtap connected]" {
			continue
		}

		ts := raw.Logtail.ClientTime
		if ts == "" {
			ts = time.Now().UTC().Format(time.RFC3339)
		}

		level := "info"
		lower := strings.ToLower(text)
		if strings.Contains(lower, "[err") || strings.Contains(lower, "error") {
			level = "error"
		} else if strings.Contains(lower, "[warn") || strings.Contains(lower, "warning") {
			level = "warn"
		}

		e := state.NewLogEntry(level, text, "tailscale")
		e.Timestamp = ts
		buf.Add(e)
	}
	return scanner.Err()
}
