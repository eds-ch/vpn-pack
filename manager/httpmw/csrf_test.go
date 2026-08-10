package httpmw

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRF_IssuesCookieOnSafeRequest(t *testing.T) {
	h := CSRF()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	res := rec.Result()
	got := ""
	for _, c := range res.Cookies() {
		if c.Name == csrfCookie {
			got = c.Value
		}
	}
	if got == "" {
		t.Fatal("missing csrf cookie")
	}
}

func TestCSRF_RejectsMutationWithoutHeader(t *testing.T) {
	h := CSRF()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "abc"})
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code=%d want 403", rec.Code)
	}
}

func TestCSRF_AcceptsMatchingHeader(t *testing.T) {
	h := CSRF()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "abc"})
	req.Header.Set(csrfHeader, "abc")
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d want 200", rec.Code)
	}
}

func TestCSRF_RejectsMismatchedHeader(t *testing.T) {
	h := CSRF()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "abc"})
	req.Header.Set(csrfHeader, "xyz")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code=%d want 403", rec.Code)
	}
}

func TestCSRF_RejectsMutationWithoutAnyToken(t *testing.T) {
	h := CSRF()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	// No cookie, no header — a fresh request that didn't first hit a safe endpoint.
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code=%d want 403 (cannot mutate without first establishing a token)", rec.Code)
	}
}

// UniFi OS owns the X-Csrf-Token header. Its nginx forwards whatever the
// client puts there to its own auth backend as X-Provided-Csrf-Token and
// answers 403 before the request ever reaches us. Our token must therefore
// ride in a header of our own, and X-Csrf-Token must not satisfy our check —
// otherwise the UI has to choose which layer to satisfy and always loses one.
func TestCSRF_DoesNotAcceptUniFiOwnedHeader(t *testing.T) {
	h := CSRF()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "abc"})
	req.Header.Set("X-Csrf-Token", "abc")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code=%d want 403: X-Csrf-Token belongs to UniFi OS and must not satisfy our check", rec.Code)
	}
}

func TestCSRF_AcceptsOwnHeaderAlongsideUniFiHeader(t *testing.T) {
	h := CSRF()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookie, Value: "abc"})
	req.Header.Set("X-VpnPack-Csrf", "abc")
	req.Header.Set("X-Csrf-Token", "unifi-session-token")
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d want 200: both tokens present, each in its own header", rec.Code)
	}
}

func TestCSRF_CookieHasSameSiteStrict(t *testing.T) {
	h := CSRF()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	for _, c := range rec.Result().Cookies() {
		if c.Name == csrfCookie {
			if c.SameSite != http.SameSiteStrictMode {
				t.Fatalf("SameSite=%v want strict", c.SameSite)
			}
			return
		}
	}
	t.Fatal("missing csrf cookie")
}

func TestCSRFSetSecureForTests_FlipsCookieSecureFlag(t *testing.T) {
	CSRFSetSecureForTests(false)
	t.Cleanup(func() { CSRFSetSecureForTests(true) })

	h := CSRF()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	for _, c := range rec.Result().Cookies() {
		if c.Name == csrfCookie {
			if c.Secure {
				t.Fatalf("Secure=true want false after CSRFSetSecureForTests(false)")
			}
			return
		}
	}
	t.Fatal("missing csrf cookie")
}
