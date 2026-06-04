package client

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"unifi-tailscale/manager/config"
	"unifi-tailscale/manager/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// placeholderPin is accepted by NewIntegrationClient (length-checked) but
// never matched against any real cert — use in tests that exercise plain
// HTTP servers where VerifyPeerCertificate is never invoked.
func placeholderPin() []byte { return bytes.Repeat([]byte{0}, sha256.Size) }

func newTestIntegrationClient(t *testing.T, handler http.HandlerFunc) *IntegrationClient {
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	ic, err := NewIntegrationClient(IntegrationConfig{
		Endpoint: ts.URL,
		APIKey:   "test-api-key",
		SPKIPin:  placeholderPin(),
	})
	if err != nil {
		t.Fatalf("NewIntegrationClient: %v", err)
	}
	return ic
}

// TestIntegrationClient_AcceptsPinnedCert verifies that a leaf cert
// whose SPKI matches the pin is accepted through Validate over TLS.
// Closes SEC-C5 (positive path).
func TestIntegrationClient_AcceptsPinnedCert(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"applicationVersion":"9.0.1"}`))
	}))
	t.Cleanup(srv.Close)

	pin := sha256.Sum256(srv.Certificate().RawSubjectPublicKeyInfo)
	cli, err := NewIntegrationClient(IntegrationConfig{
		Endpoint: srv.URL,
		APIKey:   "test",
		SPKIPin:  pin[:],
	})
	require.NoError(t, err)

	info, err := cli.Validate(context.Background())
	require.NoError(t, err)
	require.NotNil(t, info)
	assert.Equal(t, "9.0.1", info.ApplicationVersion)
}

// TestIntegrationClient_RejectsUnpinnedCert verifies that a leaf cert
// whose SPKI does NOT match the pin is rejected — the Validate call
// must error out, with no integration request reaching the server.
// Closes SEC-C5 (negative path, primary).
func TestIntegrationClient_RejectsUnpinnedCert(t *testing.T) {
	var serverHit bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHit = true
		_, _ = w.Write([]byte(`{"applicationVersion":"9.0.1"}`))
	}))
	t.Cleanup(srv.Close)

	wrongPin := bytes.Repeat([]byte{0xaa}, sha256.Size)
	cli, err := NewIntegrationClient(IntegrationConfig{
		Endpoint: srv.URL,
		APIKey:   "test",
		SPKIPin:  wrongPin,
	})
	require.NoError(t, err)

	_, err = cli.Validate(context.Background())
	require.Error(t, err, "expected pin mismatch")
	assert.False(t, serverHit, "request must not be sent if pin verification fails")
}

// TestIntegrationClient_RefusesWithoutPin verifies fail-closed behaviour
// when no pin material is provided — the constructor returns an error
// and a nil client, so the integration features are simply disabled.
// Closes SEC-C5 (boot fail-closed).
func TestIntegrationClient_RefusesWithoutPin(t *testing.T) {
	cli, err := NewIntegrationClient(IntegrationConfig{
		Endpoint: "https://127.0.0.1/",
		APIKey:   "test",
		SPKIPin:  nil,
	})
	require.Error(t, err, "expected fail-closed when pin material absent")
	assert.Nil(t, cli, "client must be nil on fail-closed")
}

// TestIntegrationClient_RejectsWrongPinLength verifies that a pin of
// the wrong size is rejected at construction. This guards against a
// silent truncation/extension bug in LoadSPKIPin or its callers.
func TestIntegrationClient_RejectsWrongPinLength(t *testing.T) {
	cli, err := NewIntegrationClient(IntegrationConfig{
		Endpoint: "https://127.0.0.1/",
		APIKey:   "test",
		SPKIPin:  []byte{0x01, 0x02, 0x03},
	})
	require.Error(t, err)
	assert.Nil(t, cli)
}

func TestIntegrationValidate(t *testing.T) {
	tests := []struct {
		name       string
		apiKey     string
		status     int
		body       string
		wantErr    bool
		errIs      error
		wantAppVer string
	}{
		{
			name:       "success",
			apiKey:     "test-key",
			status:     200,
			body:       `{"applicationVersion":"8.6.9"}`,
			wantErr:    false,
			wantAppVer: "8.6.9",
		},
		{
			name:    "unauthorized 401",
			apiKey:  "bad-key",
			status:  401,
			body:    `{"error":"unauthorized"}`,
			wantErr: true,
			errIs:   domain.ErrUnauthorized,
		},
		{
			name:    "server error 500",
			apiKey:  "test-key",
			status:  500,
			body:    `internal error`,
			wantErr: true,
			errIs:   domain.ErrIntegrationAPI,
		},
		{
			name:    "no api key",
			apiKey:  "",
			wantErr: true,
			errIs:   domain.ErrUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var serverHit bool
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				serverHit = true
				assert.Equal(t, "GET", r.Method)
				assert.Equal(t, "/v1/info", r.URL.Path)
				assert.Equal(t, tt.apiKey, r.Header.Get("X-API-Key"))
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})

			ts := httptest.NewServer(handler)
			t.Cleanup(ts.Close)

			ic, err := NewIntegrationClient(IntegrationConfig{
				Endpoint: ts.URL,
				APIKey:   tt.apiKey,
				SPKIPin:  placeholderPin(),
			})
			if err != nil {
				t.Fatalf("NewIntegrationClient: %v", err)
			}

			info, err := ic.Validate(context.Background())

			if tt.apiKey == "" {
				assert.False(t, serverHit, "server should not be hit when apiKey is empty")
			}

			if tt.wantErr {
				require.Error(t, err)
				if tt.errIs != nil {
					assert.ErrorIs(t, err, tt.errIs)
				}
				assert.Nil(t, info)
			} else {
				require.NoError(t, err)
				require.NotNil(t, info)
				assert.Equal(t, tt.wantAppVer, info.ApplicationVersion)
			}
		})
	}
}

func TestIntegrationDiscoverSiteID(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantID  string
		wantErr bool
	}{
		{
			name:   "single site",
			body:   `{"data":[{"id":"site-abc","name":"Default"}]}`,
			wantID: "site-abc",
		},
		{
			name:    "empty data",
			body:    `{"data":[]}`,
			wantErr: true,
		},
		{
			name:   "multiple sites returns first",
			body:   `{"data":[{"id":"first-id","name":"Main"},{"id":"second-id","name":"Branch"}]}`,
			wantID: "first-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ic := newTestIntegrationClient(t, func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/v1/sites", r.URL.Path)
				w.WriteHeader(200)
				_, _ = w.Write([]byte(tt.body))
			})

			id, err := ic.DiscoverSiteID(context.Background())
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantID, id)
			}
		})
	}
}

func TestFindExistingPolicy(t *testing.T) {
	policies := []domain.Policy{
		{ID: "pol-1", Name: "VPN Pack: Allow Tailscale to Internal"},
		{ID: "pol-2", Name: "VPN Pack: Allow Internal to Tailscale"},
		{ID: "pol-3", Name: "Block All"},
	}

	tests := []struct {
		name     string
		policies []domain.Policy
		search   string
		wantID   string
	}{
		{
			name:     "found first",
			policies: policies,
			search:   "VPN Pack: Allow Tailscale to Internal",
			wantID:   "pol-1",
		},
		{
			name:     "found last",
			policies: policies,
			search:   "Block All",
			wantID:   "pol-3",
		},
		{
			name:     "not found",
			policies: policies,
			search:   "Nonexistent Policy",
			wantID:   "",
		},
		{
			name:     "empty slice",
			policies: nil,
			search:   "Any",
			wantID:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findExistingPolicy(tt.policies, tt.search)
			assert.Equal(t, tt.wantID, got)
		})
	}
}

func TestWanPortPolicyName(t *testing.T) {
	tests := []struct {
		name   string
		port   int
		marker string
		want   string
	}{
		{
			name:   "wg-s2s marker",
			port:   51820,
			marker: config.WanMarkerWgS2sPrefix + "wg0",
			want:   "VPN Pack: WG S2S UDP 51820 (wg0)",
		},
		{
			name:   "relay-server marker",
			port:   3478,
			marker: config.WanMarkerRelay,
			want:   "VPN Pack: Relay Server UDP 3478",
		},
		{
			name:   "tailscale-wg marker",
			port:   41641,
			marker: config.WanMarkerTailscaleWG,
			want:   "VPN Pack: Tailscale WireGuard UDP 41641",
		},
		{
			name:   "unknown marker",
			port:   9999,
			marker: "custom-thing",
			want:   "VPN Pack: UDP 9999 (custom-thing)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := WanPortPolicyName(tt.port, tt.marker)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIntegrationValidateResponseParsing(t *testing.T) {
	ic := newTestIntegrationClient(t, func(w http.ResponseWriter, r *http.Request) {
		resp := domain.AppInfo{ApplicationVersion: "9.0.1"}
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(resp)
	})

	info, err := ic.Validate(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "9.0.1", info.ApplicationVersion)
}

// SEC-C7: error messages from non-2xx upstream responses must not include
// the raw response body. The body can carry session tokens, stack traces,
// or other sensitive data and propagates into operator-visible logs and
// /api/* responses through error wrapping.
func TestIntegrationErrorsOmitUpstreamBody(t *testing.T) {
	const secret = "INTERNAL_SECRET_TOKEN_xyz_12345"

	cases := []struct {
		name   string
		call   func(ic *IntegrationClient) error
		method string
		path   string
	}{
		{
			name:   "Validate",
			method: "GET",
			path:   "/v1/info",
			call: func(ic *IntegrationClient) error {
				_, err := ic.Validate(context.Background())
				return err
			},
		},
		{
			name:   "ListZones",
			method: "GET",
			path:   "/v1/sites/s1/firewall/zones",
			call: func(ic *IntegrationClient) error {
				_, err := ic.ListZones(context.Background(), "s1")
				return err
			},
		},
		{
			name:   "CreateZone",
			method: "POST",
			path:   "/v1/sites/s1/firewall/zones",
			call: func(ic *IntegrationClient) error {
				_, err := ic.CreateZone(context.Background(), "s1", "z")
				return err
			},
		},
		{
			name:   "CreatePolicy",
			method: "POST",
			path:   "/v1/sites/s1/firewall/policies",
			call: func(ic *IntegrationClient) error {
				_, err := ic.CreatePolicy(context.Background(), "s1", CreatePolicyRequest{Name: "x"})
				return err
			},
		},
		{
			name:   "DeletePolicy",
			method: "DELETE",
			path:   "/v1/sites/s1/firewall/policies/p1",
			call: func(ic *IntegrationClient) error {
				return ic.DeletePolicy(context.Background(), "s1", "p1")
			},
		},
		{
			name:   "DeleteZone",
			method: "DELETE",
			path:   "/v1/sites/s1/firewall/zones/z1",
			call: func(ic *IntegrationClient) error {
				return ic.DeleteZone(context.Background(), "s1", "z1")
			},
		},
		{
			name:   "CreateDNSPolicy",
			method: "POST",
			path:   "/v1/sites/s1/dns/policies",
			call: func(ic *IntegrationClient) error {
				_, err := ic.CreateDNSPolicy(context.Background(), "s1", createDNSPolicyRequest{Domain: "x"})
				return err
			},
		},
		{
			name:   "DeleteDNSPolicy",
			method: "DELETE",
			path:   "/v1/sites/s1/dns/policies/p1",
			call: func(ic *IntegrationClient) error {
				return ic.DeleteDNSPolicy(context.Background(), "s1", "p1")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ic := newTestIntegrationClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500)
				_, _ = w.Write([]byte(secret))
			})

			err := tc.call(ic)
			require.Error(t, err)
			assert.ErrorIs(t, err, domain.ErrIntegrationAPI)
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error message leaked upstream body %q: %v", secret, err)
			}
			if !strings.Contains(err.Error(), "500") {
				t.Fatalf("error message must include status code; got: %v", err)
			}
		})
	}
}
