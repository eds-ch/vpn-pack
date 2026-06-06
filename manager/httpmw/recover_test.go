package httpmw

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRecover_ConvertsPanicTo500(t *testing.T) {
	h := Recover()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("code=%d want 500", rec.Code)
	}
}

func TestRecover_NoPanicPassthrough(t *testing.T) {
	h := Recover()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("code=%d want 202", rec.Code)
	}
}
