// Package logredact provides a slog.Handler wrapper that redacts secrets
// (Tailscale auth/client keys, WireGuard public keys, bearer tokens) from
// log records before they reach the inner handler.
package logredact

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
)

type rule struct {
	re   *regexp.Regexp
	repl string
}

var rules = []rule{
	{regexp.MustCompile(`(tskey-(?:auth|client)-)[A-Za-z0-9-]+`), "${1}***"},
	{regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9._~+/=-]+`), "${1}***"},
	{regexp.MustCompile(`[A-Za-z0-9+/]{43}=`), "***"},
}

type Handler struct {
	inner slog.Handler
}

func Wrap(h slog.Handler) *Handler { return &Handler{inner: h} }

func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{inner: h.inner.WithAttrs(redactAttrs(attrs))}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{inner: h.inner.WithGroup(name)}
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	rr := slog.NewRecord(r.Time, r.Level, redactString(r.Message), r.PC)
	r.Attrs(func(a slog.Attr) bool {
		rr.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, rr)
}

func redactAttr(a slog.Attr) slog.Attr {
	v := a.Value.Resolve()
	switch v.Kind() {
	case slog.KindString:
		return slog.String(a.Key, redactString(v.String()))
	case slog.KindGroup:
		inner := v.Group()
		out := make([]slog.Attr, len(inner))
		for i, sub := range inner {
			out[i] = redactAttr(sub)
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(out...)}
	case slog.KindAny:
		// Non-string values (errors, structs, arbitrary types) can carry
		// secrets in their %v form. Render through fmt and redact; the
		// loss of JSON fidelity (errors become strings instead of nested
		// objects) is worth the safety guarantee.
		any := v.Any()
		if s, ok := any.(string); ok {
			return slog.String(a.Key, redactString(s))
		}
		return slog.String(a.Key, redactString(fmt.Sprint(any)))
	default:
		return a
	}
}

// RedactString applies the same secret-redaction rules used by the slog
// Handler. Callers that build log strings outside the slog pipeline
// (e.g. direct LogBuffer writes) can route them through this function
// to keep parity with slog-routed records.
func RedactString(s string) string { return redactString(s) }

func redactAttrs(in []slog.Attr) []slog.Attr {
	out := make([]slog.Attr, len(in))
	for i, a := range in {
		out[i] = redactAttr(a)
	}
	return out
}

func redactString(s string) string {
	for _, r := range rules {
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}
