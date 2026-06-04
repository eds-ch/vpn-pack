package udapi

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	errConnectionRefused = errors.New("udapi: connection refused")
	errTimeout           = errors.New("udapi: request timeout")
	errBadResponse       = errors.New("udapi: invalid response from server")
)

const (
	defaultSocketPath = "/run/ubnt-udapi-server.sock"
	requestTimeout    = 10 * time.Second
	bindPathTemplate  = "/tmp/vpn-pack-manager-udapi-%d"
	// maxUDAPIBodyBytes caps the size of a single UDAPI response we are
	// willing to allocate. UDAPI replies are config snapshots; 8 MiB is
	// well above any observed maximum and bounds a single malicious or
	// corrupt size-line from forcing an unbounded allocation.
	maxUDAPIBodyBytes = 8 << 20
)

type udapiMeta struct {
	RC  string `json:"rc"`
	Msg string `json:"msg"`
}

type udapiEnvelope struct {
	Meta udapiMeta `json:"meta"`
}

// enforceEnvelope inspects the response of a mutating call and surfaces
// any server-side error reported via the standard UDAPI envelope
// ({"meta":{"rc":"error","msg":"..."}}). GET responses are not required
// to carry this envelope and are skipped.
func enforceEnvelope(method string, raw json.RawMessage) error {
	if method == "GET" || method == "" || len(raw) == 0 {
		return nil
	}
	var env udapiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil
	}
	if env.Meta.RC == "error" {
		return fmt.Errorf("udapi: %s: %s", env.Meta.RC, env.Meta.Msg)
	}
	return nil
}

type UDAPIClient struct {
	socketPath string
}

type envelope struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Method  string `json:"method"`
	Entity  string `json:"entity"`
	Request any    `json:"request,omitempty"`
}

type Response struct {
	ID       string          `json:"id"`
	Version  string          `json:"version"`
	Method   string          `json:"method"`
	Entity   string          `json:"entity"`
	Response json.RawMessage `json:"response"`
}

func NewClient(socketPath string) *UDAPIClient {
	if socketPath == "" {
		socketPath = defaultSocketPath
	}
	return &UDAPIClient{socketPath: socketPath}
}

func (c *UDAPIClient) Request(method, entity string, payload any) (*Response, error) {
	return c.RequestCtx(context.Background(), method, entity, payload)
}

func (c *UDAPIClient) RequestCtx(ctx context.Context, method, entity string, payload any) (*Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	conn, bindPath, err := c.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	if bindPath != "" {
		defer func() { _ = os.Remove(bindPath) }()
	}

	deadline := time.Now().Add(requestTimeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("udapi: set deadline: %w", err)
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()

	env := envelope{
		ID:      "manager-" + randomID(),
		Version: "v1.0",
		Method:  method,
		Entity:  entity,
	}
	if payload != nil {
		env.Request = payload
	}

	body, err := json.Marshal(env)
	if err != nil {
		return nil, fmt.Errorf("udapi: marshal request: %w", err)
	}

	frame := fmt.Sprintf("%d\n%s", len(body), body)
	if _, err := conn.Write([]byte(frame)); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return nil, cerr
		}
		if isTimeout(err) {
			return nil, errTimeout
		}
		return nil, fmt.Errorf("udapi: write: %w", err)
	}

	reader := bufio.NewReader(conn)
	sizeLine, err := reader.ReadString('\n')
	if err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return nil, cerr
		}
		if isTimeout(err) {
			return nil, errTimeout
		}
		return nil, fmt.Errorf("udapi: read size: %w", err)
	}

	size, err := strconv.Atoi(strings.TrimSpace(sizeLine))
	if err != nil || size <= 0 || size > maxUDAPIBodyBytes {
		return nil, fmt.Errorf("%w: invalid size %q (max %d)", errBadResponse, sizeLine, maxUDAPIBodyBytes)
	}

	respBody := make([]byte, size)
	if _, err := io.ReadFull(reader, respBody); err != nil {
		if cerr := ctx.Err(); cerr != nil {
			return nil, cerr
		}
		if isTimeout(err) {
			return nil, errTimeout
		}
		return nil, fmt.Errorf("udapi: read body: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("%w: %v", errBadResponse, err)
	}

	if err := enforceEnvelope(method, resp.Response); err != nil {
		return nil, err
	}

	return &resp, nil
}

func (c *UDAPIClient) connect(ctx context.Context) (net.Conn, string, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err == nil {
		return conn, "", nil
	}

	bindPath := fmt.Sprintf(bindPathTemplate, os.Getpid())
	_ = os.Remove(bindPath)

	local := &net.UnixAddr{Name: bindPath, Net: "unix"}
	dLocal := net.Dialer{LocalAddr: local}

	uconn, err2 := dLocal.DialContext(ctx, "unix", c.socketPath)
	if err2 != nil {
		_ = os.Remove(bindPath)
		if isConnectionRefused(err) || isConnectionRefused(err2) {
			return nil, "", errConnectionRefused
		}
		return nil, "", fmt.Errorf("udapi: connect: %w (bind fallback: %v)", err, err2)
	}

	return uconn, bindPath, nil
}

func randomID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func isConnectionRefused(err error) bool {
	return err != nil && strings.Contains(err.Error(), "connection refused")
}
