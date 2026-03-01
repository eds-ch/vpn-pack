package udapi

import (
	"bufio"
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
)

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
	conn, bindPath, err := c.connect()
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	if bindPath != "" {
		defer func() { _ = os.Remove(bindPath) }()
	}

	deadline := time.Now().Add(requestTimeout)
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("udapi: set deadline: %w", err)
	}

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
		if isTimeout(err) {
			return nil, errTimeout
		}
		return nil, fmt.Errorf("udapi: write: %w", err)
	}

	reader := bufio.NewReader(conn)
	sizeLine, err := reader.ReadString('\n')
	if err != nil {
		if isTimeout(err) {
			return nil, errTimeout
		}
		return nil, fmt.Errorf("udapi: read size: %w", err)
	}

	size, err := strconv.Atoi(strings.TrimSpace(sizeLine))
	if err != nil || size <= 0 {
		return nil, fmt.Errorf("%w: invalid size %q", errBadResponse, sizeLine)
	}

	respBody := make([]byte, size)
	if _, err := io.ReadFull(reader, respBody); err != nil {
		if isTimeout(err) {
			return nil, errTimeout
		}
		return nil, fmt.Errorf("udapi: read body: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("%w: %v", errBadResponse, err)
	}

	return &resp, nil
}

func (c *UDAPIClient) connect() (net.Conn, string, error) {
	conn, err := net.Dial("unix", c.socketPath)
	if err == nil {
		return conn, "", nil
	}

	bindPath := fmt.Sprintf("/tmp/vpn-pack-manager-udapi-%d", os.Getpid())
	_ = os.Remove(bindPath)

	local := &net.UnixAddr{Name: bindPath, Net: "unix"}
	remote := &net.UnixAddr{Name: c.socketPath, Net: "unix"}

	uconn, err2 := net.DialUnix("unix", local, remote)
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
