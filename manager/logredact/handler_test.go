package logredact

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func newTestLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(Wrap(slog.NewJSONHandler(buf, nil)))
}

func TestHandlerRedactsAuthKey(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf)
	l.Info("login", "url", "https://login.tailscale.com/?key=tskey-auth-kFoo123-CafeDeadBeef")

	out := buf.String()
	if strings.Contains(out, "CafeDeadBeef") {
		t.Fatalf("auth-key secret leaked: %s", out)
	}
	if strings.Contains(out, "tskey-auth-kFoo123-CafeDeadBeef") {
		t.Fatalf("full auth-key present: %s", out)
	}
	if !strings.Contains(out, "tskey-auth-***") {
		t.Fatalf("expected redaction marker `tskey-auth-***`; got %s", out)
	}
}

func TestHandlerRedactsAuthKeyInMessage(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf)
	l.Info("processing key tskey-auth-kFoo123-CafeDeadBeef from input")

	out := buf.String()
	if strings.Contains(out, "CafeDeadBeef") {
		t.Fatalf("secret leaked from message: %s", out)
	}
	if !strings.Contains(out, "tskey-auth-***") {
		t.Fatalf("expected marker in message: %s", out)
	}
}

func TestHandlerRedactsClientKey(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf)
	l.Info("oauth", "key", "tskey-client-abc123-SecretValueXYZ")

	out := buf.String()
	if strings.Contains(out, "SecretValueXYZ") {
		t.Fatalf("client-key leaked: %s", out)
	}
	if !strings.Contains(out, "tskey-client-***") {
		t.Fatalf("expected `tskey-client-***`: %s", out)
	}
}

func TestHandlerRedactsBearerToken(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf)
	l.Info("auth", "header", "Bearer eyJhbGciOiJIUzI1NiJ9.payload.signature")

	out := buf.String()
	if strings.Contains(out, "eyJhbGciOiJIUzI1NiJ9.payload.signature") {
		t.Fatalf("bearer token leaked: %s", out)
	}
	if !strings.Contains(out, "Bearer ***") {
		t.Fatalf("expected `Bearer ***`: %s", out)
	}
}

func TestHandlerRedactsWGKey(t *testing.T) {
	// WireGuard public key: 32 bytes → 44-char base64 ending with '='.
	wgKey := "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789AbCdEfG="
	if len(wgKey) != 44 {
		t.Fatalf("test fixture wrong length: %d", len(wgKey))
	}

	var buf bytes.Buffer
	l := newTestLogger(&buf)
	l.Info("peer", "pubkey", wgKey)

	out := buf.String()
	if strings.Contains(out, wgKey) {
		t.Fatalf("WG key leaked: %s", out)
	}
}

func TestHandlerRedactsInWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	base := Wrap(slog.NewJSONHandler(&buf, nil))
	l := slog.New(base).With("key", "tskey-auth-kFoo123-CafeDeadBeef")
	l.Info("hello")

	out := buf.String()
	if strings.Contains(out, "CafeDeadBeef") {
		t.Fatalf("WithAttrs leaked: %s", out)
	}
	if !strings.Contains(out, "tskey-auth-***") {
		t.Fatalf("expected redacted marker in WithAttrs path: %s", out)
	}
}

func TestHandlerPassesThroughCleanInput(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf)
	l.Info("clean message", "user", "alice", "count", 42)

	out := buf.String()
	for _, want := range []string{"clean message", "alice", "42"} {
		if !strings.Contains(out, want) {
			t.Fatalf("clean input %q missing: %s", want, out)
		}
	}
}

func TestHandlerEnabledDelegates(t *testing.T) {
	var buf bytes.Buffer
	h := Wrap(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Fatalf("Enabled should mirror inner: info should be disabled when min=warn")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Fatalf("Enabled should mirror inner: error should be enabled when min=warn")
	}
}

// Catches the bug where redactAttr only handled KindString and let
// errors through verbatim. `slog.Warn("...", "err", err)` is the most
// common shape for upstream errors and the most likely vector for
// auth-key leakage.
func TestHandlerRedactsErrorAttr(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf)
	err := errors.New("integration GET /v1/info failed: tskey-auth-kFoo123-CafeDeadBeef")
	l.Warn("upstream failed", "err", err)

	out := buf.String()
	if strings.Contains(out, "CafeDeadBeef") {
		t.Fatalf("error attr leaked secret: %s", out)
	}
	if !strings.Contains(out, "tskey-auth-***") {
		t.Fatalf("expected redaction marker for error attr: %s", out)
	}
}

// Verifies that values carried as slog.Any (e.g. a struct that
// embeds a key) are stringified-then-redacted instead of falling
// through unchanged.
func TestHandlerRedactsAnyAttr(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf)

	type payload struct {
		AuthKey string
	}
	l.Info("note", "payload", slog.AnyValue(payload{AuthKey: "tskey-auth-kFoo123-Secret"}))

	out := buf.String()
	if strings.Contains(out, "Secret") {
		t.Fatalf("Any value leaked secret: %s", out)
	}
	if !strings.Contains(out, "tskey-auth-***") {
		t.Fatalf("expected redaction marker in Any rendering: %s", out)
	}
}

// LogValuer values must be resolved before redaction so the eventual
// payload is sanitised (callers may pre-format a string with secrets).
type tskeyValuer string

func (k tskeyValuer) LogValue() slog.Value { return slog.StringValue(string(k)) }

func TestHandlerRedactsLogValuer(t *testing.T) {
	var buf bytes.Buffer
	l := newTestLogger(&buf)
	l.Info("auth", "key", tskeyValuer("tskey-auth-kFoo123-Secret"))

	out := buf.String()
	if strings.Contains(out, "Secret") {
		t.Fatalf("LogValuer leaked secret: %s", out)
	}
	if !strings.Contains(out, "tskey-auth-***") {
		t.Fatalf("expected redaction marker for LogValuer: %s", out)
	}
}

// RedactString must apply the same rules as the Handler so callers
// that write directly to a sink (e.g. LogBuffer) stay in parity.
func TestRedactStringMatchesHandler(t *testing.T) {
	in := "tskey-auth-kFoo123-Secret"
	got := RedactString(in)
	if strings.Contains(got, "Secret") {
		t.Fatalf("RedactString leaked secret: %q", got)
	}
	if !strings.Contains(got, "tskey-auth-***") {
		t.Fatalf("RedactString missing marker: %q", got)
	}
}
