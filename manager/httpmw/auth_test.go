package httpmw

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestPeerUID_AcceptsRoot(t *testing.T) {
	h := PeerUIDAuth(0)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(withPeerUID(req.Context(), 0))
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d want 200", rec.Code)
	}
}

func TestPeerUID_AcceptsSelfEuid(t *testing.T) {
	h := PeerUIDAuth(99999)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	req = req.WithContext(withPeerUID(req.Context(), uint32(os.Geteuid())))
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d want 200 (self euid must be auto-allowed)", rec.Code)
	}
}

func TestPeerUID_RejectsOther(t *testing.T) {
	h := PeerUIDAuth(0)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	// Choose a uid that is neither 0 nor our euid.
	uid := uint32(1000)
	if uid == uint32(os.Geteuid()) {
		uid = 1001
	}
	req = req.WithContext(withPeerUID(req.Context(), uid))
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code=%d want 403", rec.Code)
	}
}

func TestPeerUID_RejectsNoCred(t *testing.T) {
	// Request with no peer-uid in context (TCP-like) must be rejected.
	h := PeerUIDAuth(0)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("code=%d want 403 (TCP must be rejected)", rec.Code)
	}
}

func TestLookupAllowedUIDs_IncludesRootAndSelf(t *testing.T) {
	got := LookupAllowedUIDs()
	hasRoot, hasSelf := false, false
	self := uint32(os.Geteuid())
	for _, u := range got {
		if u == 0 {
			hasRoot = true
		}
		if u == self {
			hasSelf = true
		}
	}
	if !hasRoot {
		t.Fatalf("LookupAllowedUIDs() must include root: %v", got)
	}
	if !hasSelf {
		t.Fatalf("LookupAllowedUIDs() must include self euid %d: %v", self, got)
	}
}

func TestLookupAllowedUIDs_SkipsMissingUser(t *testing.T) {
	// Should not panic or error for missing user names.
	got := LookupAllowedUIDs("definitely-not-a-user-9f8e7d6c5b4a")
	if len(got) < 2 {
		t.Fatalf("expected at least 2 uids (root + self), got %v", got)
	}
}

func TestWithFakePeerUIDForTests_InjectsUID(t *testing.T) {
	fn := WithFakePeerUIDForTests(42)
	ctx := fn(t.Context(), nil)
	got, ok := peerUID(ctx)
	if !ok {
		t.Fatal("peer uid not set in context")
	}
	if got != 42 {
		t.Fatalf("uid=%d want 42", got)
	}
}
