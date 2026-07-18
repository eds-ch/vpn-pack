package httpmw

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// okHandler records that it was reached and returns 200.
func okHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

// TestToken_EnforcementOn catches the M1 bug: without this middleware any
// request that clears PeerUIDAuth reaches every endpoint. With a secret
// configured, a request must present a matching X-VpnPack-Token or be 403.
func TestToken_EnforcementOn(t *testing.T) {
	const secret = "s3cr3t-hex-token"

	tests := []struct {
		name       string
		header     string
		setHeader  bool
		wantStatus int
		wantReach  bool
	}{
		{name: "missing header -> 403", setHeader: false, wantStatus: http.StatusForbidden, wantReach: false},
		{name: "wrong token -> 403", header: "nope", setHeader: true, wantStatus: http.StatusForbidden, wantReach: false},
		{name: "correct token -> pass", header: secret, setHeader: true, wantStatus: http.StatusOK, wantReach: true},
		{name: "empty header value -> 403", header: "", setHeader: true, wantStatus: http.StatusForbidden, wantReach: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reached := false
			h := Token(secret)(okHandler(&reached))
			req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
			if tt.setHeader {
				req.Header.Set(TokenHeader, tt.header)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
			if reached != tt.wantReach {
				t.Fatalf("handler reached = %v, want %v", reached, tt.wantReach)
			}
		})
	}
}

// TestToken_EnforcementDisabled proves the fail-open default: with no
// secret configured (dev/MOCK, or a partial upgrade) requests pass even
// without the header, so the fix cannot lock out an unprovisioned install.
func TestToken_EnforcementDisabled(t *testing.T) {
	reached := false
	h := Token("")(okHandler(&reached))
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fail-open when no secret)", w.Code)
	}
	if !reached {
		t.Fatal("handler not reached with token factor disabled")
	}
}

// TestSameOrigin catches a cross-site mutation slipping past when the
// browser explicitly labels it cross-site; safe methods and requests
// without the header (curl, healthcheck) must still pass.
func TestSameOrigin(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		site       string
		wantStatus int
	}{
		{name: "GET ignores header", method: http.MethodGet, site: "cross-site", wantStatus: http.StatusOK},
		{name: "POST cross-site -> 403", method: http.MethodPost, site: "cross-site", wantStatus: http.StatusForbidden},
		{name: "POST same-origin -> pass", method: http.MethodPost, site: "same-origin", wantStatus: http.StatusOK},
		{name: "POST no header -> pass", method: http.MethodPost, site: "", wantStatus: http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reached := false
			h := SameOrigin()(okHandler(&reached))
			req := httptest.NewRequest(tt.method, "/api/x", nil)
			if tt.site != "" {
				req.Header.Set("Sec-Fetch-Site", tt.site)
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

// TestStaticChainRecoversPanic mirrors the SPA route chain (S2): a panic
// in the wrapped handler must be turned into 500, not escape, and the
// token factor must gate the route. Without Recover in the chain the
// panic would crash the serving goroutine.
func TestStaticChainRecoversPanic(t *testing.T) {
	const secret = "spa-secret"
	panicky := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom in spa handler")
	})
	chain := Chain(Recover(), Token(secret))(panicky)

	// With the correct token the request reaches the panicky handler and
	// Recover converts the panic to 500.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(TokenHeader, secret)
	w := httptest.NewRecorder()
	chain.ServeHTTP(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (panic recovered)", w.Code)
	}

	// Without the token the request is rejected before reaching the panic.
	req2 := httptest.NewRequest(http.MethodGet, "/", nil)
	w2 := httptest.NewRecorder()
	chain.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (token factor gates SPA route)", w2.Code)
	}
}
