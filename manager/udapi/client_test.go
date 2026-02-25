package udapi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	assert.ErrorIs(t, err, errSocketNotFound)
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
