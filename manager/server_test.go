package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"unifi-tailscale/manager/service"
)

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		value      any
		wantBody   string
		wantStatus int
	}{
		{
			name:       "200 with map",
			status:     200,
			value:      map[string]string{"key": "value"},
			wantBody:   `{"key":"value"}`,
			wantStatus: 200,
		},
		{
			name:       "201 with struct",
			status:     201,
			value:      AppInfo{ApplicationVersion: "1.0"},
			wantBody:   `{"applicationVersion":"1.0"}`,
			wantStatus: 201,
		},
		{
			name:       "200 with nil produces null",
			status:     200,
			value:      nil,
			wantBody:   `null`,
			wantStatus: 200,
		},
		{
			name:       "500 with error object",
			status:     500,
			value:      map[string]string{"error": "something broke"},
			wantBody:   `{"error":"something broke"}`,
			wantStatus: 500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeJSON(w, tt.status, tt.value)

			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			assert.Equal(t, tt.wantStatus, w.Code)

			var got, want any
			require.NoError(t, json.Unmarshal([]byte(tt.wantBody), &want))
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			assert.Equal(t, want, got)
		})
	}
}

func newTestServer(opts ...func(*Server)) *Server {
	s := &Server{
		ts:         &mockTailscaleControl{},
		hub:        &mockSSEHub{},
		state:      &TailscaleState{data: stateData{BackendState: "Unavailable"}},
		fw:         &mockFirewallService{},
		ic:         &mockIntegrationAPI{},
		manifest:   &mockManifestStore{},
		logBuf:     NewLogBuffer(100),
	}
	for _, opt := range opts {
		opt(s)
	}
	s.integration = service.NewIntegrationService(
		integrationICAdapter{s.ic}, s.manifest,
	)
	s.tailscaleSvc = service.NewTailscaleService(s.ts, s.fw)

	var wgFw service.WgS2sFirewall
	if s.fw != nil {
		wgFw = &wgS2sFirewallAdapter{fw: s.fw}
	}
	s.wgS2sSvc = service.NewWgS2sService(
		s.wgManager,
		wgFw,
		&wgS2sManifestAdapter{ms: s.manifest},
		&wgS2sLogAdapter{buf: s.logBuf},
		nil, nil, nil,
	)
	return s
}

func TestNewServerWithMocks(t *testing.T) {
	s := newTestServer()
	assert.NotNil(t, s)
	assert.NotNil(t, s.ts)
	assert.NotNil(t, s.hub)
	assert.NotNil(t, s.fw)
	assert.NotNil(t, s.ic)
	assert.NotNil(t, s.manifest)
}

func TestHandleStatusWithMocks(t *testing.T) {
	s := newTestServer()
	s.state.mu.Lock()
	s.state.data.BackendState = "Running"
	s.state.data.TailscaleIPs = []string{"100.64.0.1"}
	s.state.mu.Unlock()

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	s.handleStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body stateData
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "Running", body.BackendState)
	assert.Equal(t, []string{"100.64.0.1"}, body.TailscaleIPs)
}

func TestHandleIntegrationStatusWithMocks(t *testing.T) {
	s := newTestServer(func(s *Server) {
		s.ic = &mockIntegrationAPI{
			hasAPIKeyFn: func() bool { return false },
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/api/integration/status", nil)
	w := httptest.NewRecorder()
	s.handleIntegrationStatus(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body service.IntegrationStatus
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.False(t, body.Configured)
}

func TestHandleDeviceWithMocks(t *testing.T) {
	s := newTestServer()
	s.deviceInfo = DeviceInfo{Model: "UDM-SE", ModelShort: "UDM-SE"}

	req := httptest.NewRequest(http.MethodGet, "/api/device", nil)
	w := httptest.NewRecorder()
	s.handleDevice(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var body DeviceInfo
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "UDM-SE", body.Model)
}

func TestIntegrationReady(t *testing.T) {
	t.Run("ready when fw returns true", func(t *testing.T) {
		s := newTestServer(func(s *Server) {
			s.fw = &mockFirewallService{
				integrationReadyFn: func() bool { return true },
			}
		})
		assert.True(t, s.integrationReady())
	})

	t.Run("not ready when fw returns false", func(t *testing.T) {
		s := newTestServer()
		assert.False(t, s.integrationReady())
	})

	t.Run("not ready when fw is nil", func(t *testing.T) {
		s := newTestServer(func(s *Server) { s.fw = nil })
		assert.False(t, s.integrationReady())
	})
}

func TestHandleUpRequiresIntegration(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/tailscale/up", nil)
	req = req.WithContext(context.Background())
	w := httptest.NewRecorder()
	s.handleUp(w, req)

	assert.Equal(t, http.StatusPreconditionFailed, w.Code)
}

func TestWriteError(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		msg        string
		wantStatus int
	}{
		{
			name:       "400 bad request",
			status:     400,
			msg:        "invalid input",
			wantStatus: 400,
		},
		{
			name:       "500 internal error",
			status:     500,
			msg:        "database connection failed",
			wantStatus: 500,
		},
		{
			name:       "404 not found",
			status:     404,
			msg:        "resource not found",
			wantStatus: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			writeError(w, tt.status, tt.msg)

			assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
			assert.Equal(t, tt.wantStatus, w.Code)

			var body map[string]string
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, tt.msg, body["error"])
			assert.Len(t, body, 1, "response should only contain 'error' key")
		})
	}
}
