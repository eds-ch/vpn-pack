package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestIntegrationClient(t *testing.T, handler http.HandlerFunc) *IntegrationClient {
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	ic := NewIntegrationClient("test-api-key")
	ic.baseURL = ts.URL
	return ic
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
			errIs:   ErrUnauthorized,
		},
		{
			name:    "server error 500",
			apiKey:  "test-key",
			status:  500,
			body:    `internal error`,
			wantErr: true,
			errIs:   ErrIntegrationAPI,
		},
		{
			name:    "no api key",
			apiKey:  "",
			wantErr: true,
			errIs:   ErrUnauthorized,
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

			ic := NewIntegrationClient(tt.apiKey)
			ic.baseURL = ts.URL

			info, err := ic.Validate()

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

			id, err := ic.DiscoverSiteID()
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
	policies := []Policy{
		{ID: "pol-1", Name: "VPN Pack: Allow Tailscale to Internal"},
		{ID: "pol-2", Name: "VPN Pack: Allow Internal to Tailscale"},
		{ID: "pol-3", Name: "Block All"},
	}

	tests := []struct {
		name     string
		policies []Policy
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
			marker: "wg-s2s:wg0",
			want:   "VPN Pack: WG S2S UDP 51820 (wg0)",
		},
		{
			name:   "relay-server marker",
			port:   3478,
			marker: "relay-server",
			want:   "VPN Pack: Relay Server UDP 3478",
		},
		{
			name:   "tailscale-wg marker",
			port:   41641,
			marker: "tailscale-wg",
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
			got := wanPortPolicyName(tt.port, tt.marker)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIntegrationValidateResponseParsing(t *testing.T) {
	ic := newTestIntegrationClient(t, func(w http.ResponseWriter, r *http.Request) {
		resp := AppInfo{ApplicationVersion: "9.0.1"}
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(resp)
	})

	info, err := ic.Validate()
	require.NoError(t, err)
	assert.Equal(t, "9.0.1", info.ApplicationVersion)
}
