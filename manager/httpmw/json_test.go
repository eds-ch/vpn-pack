package httpmw

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequireJSON_RejectsWrongContentType(t *testing.T) {
	h := RequireJSON(1024)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "text/plain")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("code=%d want 415", rec.Code)
	}
}

func TestRequireJSON_AcceptsJSONWithCharset(t *testing.T) {
	h := RequireJSON(1024)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d want 200", rec.Code)
	}
}

func TestRequireJSON_LimitsBodySize(t *testing.T) {
	h := RequireJSON(8)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(200)
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/", strings.NewReader("xxxxxxxxxxxxxxxx"))
	req.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code=%d want 413", rec.Code)
	}
}

func TestRequireJSON_SkipsForGET(t *testing.T) {
	h := RequireJSON(1024)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	// No Content-Type — should still pass.
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d want 200 (GET should bypass Content-Type check)", rec.Code)
	}
}

func TestRequireJSON_SkipsForDELETE(t *testing.T) {
	h := RequireJSON(1024)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d want 200 (DELETE should bypass)", rec.Code)
	}
}
