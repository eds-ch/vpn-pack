package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"unifi-tailscale/manager/domain"
	"unifi-tailscale/manager/httpmw"
	"unifi-tailscale/manager/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	hub := &mockSSEHub{}
	s := &Server{
		ts:       &mockTailscaleControl{},
		hub:      hub,
		state:    domain.NewTailscaleState(),
		fw:       &mockFirewallService{},
		ic:       &mockIntegrationAPI{},
		manifest: &mockManifestStore{},
		logBuf:   NewLogBuffer(100),
		updater:  &updateChecker{current: "1.0.0-test", httpClient: &http.Client{}},
		health:   NewHealthTracker(hub),
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
		settingsManifestAdapter{s.manifest}, false, nil, nil,
	)
	s.diagnostics = service.NewDiagnosticsService(s.ts, s.fw, nil)
	s.exitSvc = service.NewExitNodeService(s.manifest, nil)
	s.remoteExitSvc = service.NewRemoteExitService(s.ts, s.exitSvc, s.manifest)
	s.routing = service.NewRoutingService(s.ts, s.fw, s.ic, s.manifest, nil)

	var wgFw service.WgS2sFirewall
	if s.fw != nil {
		wgFw = &wgS2sFirewallAdapter{fw: s.fw}
	}
	s.wgS2sSvc = service.NewWgS2sService(service.WgS2sConfig{
		WG:       s.wgManager,
		Firewall: wgFw,
		Manifest: &wgS2sManifestAdapter{ms: s.manifest},
		Logger:   &wgS2sLogAdapter{buf: s.logBuf},
	})
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

// roundTripperFunc adapts a plain function into an http.RoundTripper.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// newAuthedTestClient stands up an httptest.Server backed by the real
// routes() chain. ConnContext is wired so PeerUIDAuth sees the test
// process's euid (httptest uses TCP, which has no SO_PEERCRED); the
// CSRF cookie's Secure flag is flipped off so the cookie jar will echo
// it back over plain http. The returned client primes the CSRF cookie
// and copies it as X-Csrf-Token on every non-GET request.
func newAuthedTestClient(t *testing.T, srv *Server) (*httptest.Server, *http.Client) {
	t.Helper()

	httpmw.CSRFSetSecureForTests(false)
	t.Cleanup(func() { httpmw.CSRFSetSecureForTests(true) })

	h := httptest.NewUnstartedServer(srv.routes())
	h.Config.ConnContext = httpmw.WithFakePeerUIDForTests(uint32(os.Geteuid()))
	h.Start()
	t.Cleanup(h.Close)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	cl := &http.Client{Jar: jar}

	// Prime the CSRF cookie by hitting a safe endpoint.
	resp, err := cl.Get(h.URL + "/api/status")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	base := cl.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	cl.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			u, _ := url.Parse(h.URL)
			for _, c := range jar.Cookies(u) {
				if c.Name == "vp_csrf" {
					r.Header.Set("X-Csrf-Token", c.Value)
				}
			}
			if r.Header.Get("Content-Type") == "" {
				r.Header.Set("Content-Type", "application/json")
			}
		}
		return base.RoundTrip(r)
	})
	return h, cl
}

func TestRouteRegistration(t *testing.T) {
	s := newTestServer()
	h, cl := newAuthedTestClient(t, s)

	routes := []struct {
		method string
		path   string
	}{
		{"GET", "/api/status"},
		{"GET", "/api/health"},
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
			body := strings.NewReader("{}")
			req, err := http.NewRequest(r.method, h.URL+path, body)
			require.NoError(t, err)
			resp, err := cl.Do(req)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			// 405 = route not registered or wrong method.
			// 403 = middleware rejected — also indicates routing reached chain.
			// PeerUIDAuth is satisfied by the test client, so 403 means
			// something deeper than auth rejected.
			assert.NotEqual(t, http.StatusMethodNotAllowed, resp.StatusCode,
				"route %s %s returned 405 — not registered or wrong method", r.method, r.path)
			assert.NotEqual(t, http.StatusForbidden, resp.StatusCode,
				"route %s %s returned 403 — middleware chain rejected, but auth was satisfied", r.method, r.path)
		})
	}
}

func TestRoutes_MutationWithoutCSRFRejected(t *testing.T) {
	s := newTestServer()
	httpmw.CSRFSetSecureForTests(false)
	t.Cleanup(func() { httpmw.CSRFSetSecureForTests(true) })

	h := httptest.NewUnstartedServer(s.routes())
	h.Config.ConnContext = httpmw.WithFakePeerUIDForTests(uint32(os.Geteuid()))
	h.Start()
	t.Cleanup(h.Close)

	// No cookie jar, no CSRF header — POST must be 403.
	req, err := http.NewRequest(http.MethodPost, h.URL+"/api/tailscale/up", strings.NewReader("{}"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{}).Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode)
}

func TestRoutes_MutationWithWrongContentTypeRejected(t *testing.T) {
	s := newTestServer()
	h, cl := newAuthedTestClient(t, s)

	// CSRF is satisfied by the helper; wrong Content-Type must yield 415.
	req, err := http.NewRequest(http.MethodPost, h.URL+"/api/tailscale/up", strings.NewReader("plain"))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "text/plain")
	resp, err := cl.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode)
}

func TestRoutes_NoPeerCredentialsForbidden(t *testing.T) {
	s := newTestServer()
	// httptest.NewServer with no ConnContext = no peer-uid in request context.
	h := httptest.NewServer(s.routes())
	t.Cleanup(h.Close)

	resp, err := http.Get(h.URL + "/api/status")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusForbidden, resp.StatusCode,
		"a TCP request without peer credentials must be 403, even on a safe method")
}

func TestHandleStatusWithMocks(t *testing.T) {
	s := newTestServer()
	s.state.Update(func(d *domain.StateData) {
		d.BackendState = "Running"
		d.TailscaleIPs = []string{"100.64.0.1"}
	})

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
