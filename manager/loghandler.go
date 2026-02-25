package main

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

type bufferHandler struct {
	buf    *LogBuffer
	source string
	next   slog.Handler
	attrs  []slog.Attr
	group  string
}

func newBufferHandler(buf *LogBuffer, source string, next slog.Handler) *bufferHandler {
	return &bufferHandler{buf: buf, source: source, next: next}
}

func (h *bufferHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (h *bufferHandler) Handle(ctx context.Context, r slog.Record) error {
	var sb strings.Builder
	sb.WriteString(r.Message)

	prefix := ""
	if h.group != "" {
		prefix = h.group + "."
	}
	for _, a := range h.attrs {
		fmt.Fprintf(&sb, " %s%s=%v", prefix, a.Key, a.Value)
	}
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&sb, " %s%s=%v", prefix, a.Key, a.Value)
		return true
	})

	level := "info"
	switch {
	case r.Level >= slog.LevelError:
		level = "error"
	case r.Level >= slog.LevelWarn:
		level = "warn"
	}

	h.buf.Add(logEntry{
		Timestamp: r.Time.UTC().Format(time.RFC3339),
		Level:     level,
		Message:   sb.String(),
		Source:    h.source,
	})

	if h.next != nil {
		return h.next.Handle(ctx, r)
	}
	return nil
}

func (h *bufferHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)

	var next slog.Handler
	if h.next != nil {
		next = h.next.WithAttrs(attrs)
	}
	return &bufferHandler{buf: h.buf, source: h.source, next: next, attrs: newAttrs, group: h.group}
}

func (h *bufferHandler) WithGroup(name string) slog.Handler {
	var next slog.Handler
	if h.next != nil {
		next = h.next.WithGroup(name)
	}
	newGroup := name
	if h.group != "" {
		newGroup = h.group + "." + name
	}
	return &bufferHandler{buf: h.buf, source: h.source, next: next, attrs: h.attrs, group: newGroup}
}
