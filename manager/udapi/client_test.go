package udapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Test helpers: fake UDAPI socket with overrideable framing ---

type fakeUDAPIOpt func(*fakeUDAPI)

type fakeUDAPI struct {
	path string
	stop chan struct{}

	stall      bool
	customSize string
	customBody string

	mu   sync.Mutex
	sets map[string]*ipsetEntry
}

func withStallingResponse() fakeUDAPIOpt {
	return func(f *fakeUDAPI) { f.stall = true }
}
func withSizeLine(size string) fakeUDAPIOpt {
	return func(f *fakeUDAPI) { f.customSize = size }
}
func withResponseBody(body string) fakeUDAPIOpt {
	return func(f *fakeUDAPI) { f.customBody = body }
}

func newFakeUDAPISocket(t *testing.T, opts ...fakeUDAPIOpt) *fakeUDAPI {
	t.Helper()
	f := &fakeUDAPI{stop: make(chan struct{})}
	for _, opt := range opts {
		opt(f)
	}
	dir := t.TempDir()
	f.path = filepath.Join(dir, "test.sock")
	ln, err := net.Listen("unix", f.path)
	require.NoError(t, err)
	t.Cleanup(func() {
		close(f.stop)
		_ = ln.Close()
	})

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeUDAPI) serve(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	reader := bufio.NewReader(conn)
	sizeLine, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	size, err := strconv.Atoi(strings.TrimSpace(sizeLine))
	if err != nil || size <= 0 {
		return
	}
	body := make([]byte, size)
	if _, err := io.ReadFull(reader, body); err != nil {
		return
	}

	if f.stall {
		// Hold the conn open until t.Cleanup fires or client closes.
		select {
		case <-f.stop:
		}
		return
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return
	}

	if f.customSize != "" {
		respBody := []byte("{}")
		frame := f.customSize + "\n" + string(respBody)
		_, _ = conn.Write([]byte(frame))
		return
	}
	if f.customBody != "" {
		frame := fmt.Sprintf("%d\n%s", len(f.customBody), f.customBody)
		_, _ = conn.Write([]byte(frame))
		return
	}

	resp := f.handleStateful(env)
	respBody, _ := json.Marshal(resp)
	frame := fmt.Sprintf("%d\n%s", len(respBody), respBody)
	_, _ = conn.Write([]byte(frame))
}

func (f *fakeUDAPI) installSet(name string, entries []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.sets == nil {
		f.sets = map[string]*ipsetEntry{}
	}
	e := &ipsetEntry{}
	e.Identification.Name = name
	e.Identification.Type = "ipv4"
	e.Entries = append([]string(nil), entries...)
	f.sets[name] = e
}

func (f *fakeUDAPI) entries(name string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e, ok := f.sets[name]; ok {
		return append([]string(nil), e.Entries...)
	}
	return nil
}

func (f *fakeUDAPI) handleStateful(env envelope) *Response {
	respOK := func(body string) *Response {
		return &Response{
			ID:       env.ID,
			Version:  "v1.0",
			Method:   env.Method,
			Entity:   env.Entity,
			Response: json.RawMessage(body),
		}
	}

	if env.Method == "GET" && env.Entity == "/firewall/sets" {
		f.mu.Lock()
		out := make([]ipsetEntry, 0, len(f.sets))
		for _, e := range f.sets {
			ec := *e
			ec.Entries = append([]string(nil), e.Entries...)
			out = append(out, ec)
		}
		f.mu.Unlock()
		// Force a small yield so concurrent goroutines can interleave their
		// GET-modify-PUT and surface RMW races when no serialisation is used.
		time.Sleep(50 * time.Microsecond)
		body, _ := json.Marshal(out)
		return respOK(string(body))
	}
	if env.Method == "PUT" && env.Entity == "/firewall/sets/set" {
		b, _ := json.Marshal(env.Request)
		var req ipsetEntry
		_ = json.Unmarshal(b, &req)
		f.mu.Lock()
		if existing, ok := f.sets[req.Identification.Name]; ok {
			existing.Entries = append([]string(nil), req.Entries...)
		} else {
			cp := req
			cp.Entries = append([]string(nil), req.Entries...)
			f.sets[req.Identification.Name] = &cp
		}
		f.mu.Unlock()
		return respOK(`{"meta":{"rc":"ok"}}`)
	}
	return respOK(`{}`)
}

// --- Tests ---

func TestRequestCtx_CancelInterrupts(t *testing.T) {
	fake := newFakeUDAPISocket(t, withStallingResponse())
	cli := NewClient(fake.path)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	_, err := cli.RequestCtx(ctx, "GET", "/firewall/sets", nil)
	if err == nil {
		t.Fatal("expected cancel error")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
	if time.Since(start) > 200*time.Millisecond {
		t.Fatalf("RequestCtx did not honour cancel within 200ms; took %v", time.Since(start))
	}
}

func TestRequestCtx_RejectsOversizedSize(t *testing.T) {
	fake := newFakeUDAPISocket(t, withSizeLine("99999999"))
	cli := NewClient(fake.path)
	_, err := cli.RequestCtx(context.Background(), "GET", "/x", nil)
	if err == nil {
		t.Fatal("expected size-too-large error")
	}
	if !errors.Is(err, errBadResponse) {
		t.Fatalf("err=%v, want errBadResponse", err)
	}
}

func TestRequestCtx_DecodesMutationEnvelopeError(t *testing.T) {
	body := `{"id":"1","version":"v1.0","method":"POST","entity":"firewall/sets/set",` +
		`"response":{"meta":{"rc":"error","msg":"already-exists"}}}`
	fake := newFakeUDAPISocket(t, withResponseBody(body))
	cli := NewClient(fake.path)
	_, err := cli.RequestCtx(context.Background(), "POST", "/firewall/sets/set", nil)
	if err == nil {
		t.Fatal("expected app-level error from envelope")
	}
	if !strings.Contains(err.Error(), "already-exists") {
		t.Fatalf("err does not surface server msg: %v", err)
	}
}

func TestRequestCtx_GetSkipsEnvelopeCheck(t *testing.T) {
	// GET responses are not required to carry the standard envelope; an
	// envelope-shaped error inside a GET response must not be re-surfaced.
	body := `{"id":"1","version":"v1.0","method":"GET","entity":"/firewall/sets",` +
		`"response":{"meta":{"rc":"error","msg":"this-is-data-not-an-error"}}}`
	fake := newFakeUDAPISocket(t, withResponseBody(body))
	cli := NewClient(fake.path)
	_, err := cli.RequestCtx(context.Background(), "GET", "/firewall/sets", nil)
	if err != nil {
		t.Fatalf("GET should not enforce envelope, got err=%v", err)
	}
}

func TestNewClient(t *testing.T) {
	tests := []struct {
		name       string
		socketPath string
		wantPath   string
	}{
		{
			name:       "empty uses default",
			socketPath: "",
			wantPath:   defaultSocketPath,
		},
		{
			name:       "custom path",
			socketPath: "/custom/udapi.sock",
			wantPath:   "/custom/udapi.sock",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient(tt.socketPath)
			require.NotNil(t, c)
			assert.Equal(t, tt.wantPath, c.socketPath)
		})
	}
}

func startMockUDAPI(t *testing.T, handler func(env envelope) *Response) string {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()

				reader := bufio.NewReader(c)
				sizeLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}

				size, err := strconv.Atoi(strings.TrimSpace(sizeLine))
				if err != nil || size <= 0 {
					return
				}

				body := make([]byte, size)
				if _, err := io.ReadFull(reader, body); err != nil {
					return
				}

				var env envelope
				if err := json.Unmarshal(body, &env); err != nil {
					return
				}

				resp := handler(env)

				respBody, err := json.Marshal(resp)
				if err != nil {
					return
				}

				frame := fmt.Sprintf("%d\n%s", len(respBody), respBody)
				_, _ = c.Write([]byte(frame))
			}(conn)
		}
	}()

	return sockPath
}

func TestUDAPIRequestSocketNotFound(t *testing.T) {
	c := NewClient("/nonexistent/path/udapi.sock")
	_, err := c.Request("GET", "/test", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "udapi: connect:")
}

func TestUDAPIRequestSuccess(t *testing.T) {
	tests := []struct {
		name     string
		method   string
		entity   string
		payload  any
		respData string
	}{
		{
			name:     "GET request",
			method:   "GET",
			entity:   "/firewall/filter/FORWARD_IN",
			payload:  nil,
			respData: `{"rules":[]}`,
		},
		{
			name:   "POST request with payload",
			method: "POST",
			entity: "/firewall/filter/FORWARD_IN/rule",
			payload: map[string]any{
				"target":      "TS_IN",
				"description": "test rule",
			},
			respData: `{"status":"ok"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sockPath := startMockUDAPI(t, func(env envelope) *Response {
				assert.Equal(t, tt.method, env.Method)
				assert.Equal(t, tt.entity, env.Entity)
				assert.Equal(t, "v1.0", env.Version)
				assert.True(t, strings.HasPrefix(env.ID, "manager-"))

				return &Response{
					ID:       env.ID,
					Version:  "v1.0",
					Method:   env.Method,
					Entity:   env.Entity,
					Response: json.RawMessage(tt.respData),
				}
			})

			c := NewClient(sockPath)
			resp, err := c.Request(tt.method, tt.entity, tt.payload)
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, tt.method, resp.Method)
			assert.Equal(t, tt.entity, resp.Entity)
			assert.JSONEq(t, tt.respData, string(resp.Response))
		})
	}
}

func TestUDAPIRequestPreservesEnvelopeFields(t *testing.T) {
	sockPath := startMockUDAPI(t, func(env envelope) *Response {
		return &Response{
			ID:       env.ID,
			Version:  env.Version,
			Method:   env.Method,
			Entity:   env.Entity,
			Response: json.RawMessage(`{}`),
		}
	})

	c := NewClient(sockPath)
	resp, err := c.Request("GET", "/test/entity", nil)
	require.NoError(t, err)
	assert.Equal(t, "v1.0", resp.Version)
	assert.Equal(t, "GET", resp.Method)
	assert.Equal(t, "/test/entity", resp.Entity)
}
