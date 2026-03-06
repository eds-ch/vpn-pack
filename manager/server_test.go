package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
		updater:    &updateChecker{current: "1.0.0-test", httpClient: &http.Client{}},
	}
	for _, opt := range opts {
		opt(s)
	}
	s.integration = service.NewIntegrationService(
		integrationICAdapter{s.ic}, s.manifest, nil,
		service.MemKeyStore{},
	)
	s.tailscaleSvc = service.NewTailscaleService(s.ts, s.fw)
	s.settings = service.NewSettingsService(
		s.ts, s.fw, s.ic,
		settingsManifestAdapter{s.manifest}, false, nil,
	)
	s.diagnostics = service.NewDiagnosticsService(s.ts, s.fw, nil)
	s.routing = service.NewRoutingService(s.ts, s.fw, s.ic, s.manifest, nil)

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

func TestRouteRegistration(t *testing.T) {
	s := newTestServer()
	mux := s.routes()

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/status"},
		{"POST", "/api/tailscale/up"},
		{"POST", "/api/tailscale/down"},
		{"POST", "/api/tailscale/login"},
		{"POST", "/api/tailscale/logout"},
		// GET /api/events (SSE) excluded — handler blocks on streaming.
		{"GET", "/api/device"},
		{"GET", "/api/routes"},
		{"POST", "/api/routes"},
		{"POST", "/api/tailscale/auth-key"},
		{"GET", "/api/subnets"},
		{"GET", "/api/firewall"},
		{"GET", "/api/settings"},
		{"POST", "/api/settings"},
		{"GET", "/api/diagnostics"},
		{"POST", "/api/bugreport"},
		{"GET", "/api/logs"},
		{"GET", "/api/integration/status"},
		{"POST", "/api/integration/api-key"},
		{"DELETE", "/api/integration/api-key"},
		{"POST", "/api/integration/test"},
		{"GET", "/api/wg-s2s/tunnels"},
		{"POST", "/api/wg-s2s/tunnels"},
		{"PATCH", "/api/wg-s2s/tunnels/{id}"},
		{"DELETE", "/api/wg-s2s/tunnels/{id}"},
		{"POST", "/api/wg-s2s/tunnels/{id}/enable"},
		{"POST", "/api/wg-s2s/tunnels/{id}/disable"},
		{"POST", "/api/wg-s2s/generate-keypair"},
		{"GET", "/api/wg-s2s/tunnels/{id}/config"},
		{"GET", "/api/wg-s2s/wan-ip"},
		{"GET", "/api/wg-s2s/local-subnets"},
		{"GET", "/api/wg-s2s/zones"},
		{"GET", "/api/update-check"},
	}

	for _, r := range routes {
		t.Run(r.method+" "+r.path, func(t *testing.T) {
			path := strings.ReplaceAll(r.path, "{id}", "test-id")
			req := httptest.NewRequest(r.method, path, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			assert.NotEqual(t, http.StatusMethodNotAllowed, w.Code,
				"route %s %s returned 405 — not registered or wrong method", r.method, r.path)
		})
	}
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
