package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"tailscale.com/client/local"
)

type logEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	Source    string `json:"source,omitempty"`
}

type LogBuffer struct {
	mu      sync.Mutex
	entries []logEntry
	head    int
	count   int
	maxSize int
}

func NewLogBuffer(maxSize int) *LogBuffer {
	return &LogBuffer{
		entries: make([]logEntry, maxSize),
		maxSize: maxSize,
	}
}

func (lb *LogBuffer) Add(e logEntry) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.entries[lb.head] = e
	lb.head = (lb.head + 1) % lb.maxSize
	if lb.count < lb.maxSize {
		lb.count++
	}
}

func (lb *LogBuffer) Snapshot() []logEntry {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	out := make([]logEntry, lb.count)
	for i := range lb.count {
		idx := (lb.head - 1 - i + lb.maxSize) % lb.maxSize
		out[i] = lb.entries[idx]
	}
	return out
}

func runLogCollector(ctx context.Context, lc *local.Client, buf *LogBuffer) {
	for {
		if ctx.Err() != nil {
			return
		}
		if err := tailLogs(ctx, lc, buf); err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Warn("log collector disconnected, reconnecting", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(logReconnectDelay):
		}
	}
}

func tailLogs(ctx context.Context, lc *local.Client, buf *LogBuffer) error {
	reader, err := lc.TailDaemonLogs(ctx)
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
			buf.Add(logEntry{
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				Level:     "info",
				Message:   line,
			})
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

		buf.Add(logEntry{
			Timestamp: ts,
			Level:     level,
			Message:   text,
		})
	}
	return scanner.Err()
}
