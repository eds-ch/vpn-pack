//go:build linux

package httpmw

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestConnContext_ExtractsPeerUIDFromRealUnixSocket spins up an http.Server
// on a real unix socket and verifies that ConnContext extracts the calling
// process's euid via SO_PEERCRED so that PeerUIDAuth sees it. This covers
// the production path that the synthetic withPeerUID tests don't exercise.
func TestConnContext_ExtractsPeerUIDFromRealUnixSocket(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	gotUID := make(chan uint32, 1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, ok := peerUID(r.Context())
		if !ok {
			t.Errorf("peer uid not in context")
			gotUID <- 999999
			w.WriteHeader(500)
			return
		}
		gotUID <- uid
		w.WriteHeader(http.StatusNoContent)
	})

	srv := &http.Server{
		Handler:           handler,
		ConnContext:       ConnContext,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	tr := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", sockPath)
		},
	}
	cl := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	resp, err := cl.Get("http://unix/")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	select {
	case uid := <-gotUID:
		require.Equal(t, uint32(os.Geteuid()), uid,
			"ConnContext must extract our own euid via SO_PEERCRED")
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not receive request in time")
	}
}

// TestPeerUIDAuth_AcceptsRealUnixConn end-to-end: unix socket + ConnContext
// + PeerUIDAuth middleware. Self euid is in the allow list (PeerUIDAuth
// always adds it), so request should succeed (204).
func TestPeerUIDAuth_AcceptsRealUnixConn(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "test.sock")
	ln, err := net.Listen("unix", sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	handler := PeerUIDAuth()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	srv := &http.Server{
		Handler:           handler,
		ConnContext:       ConnContext,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	tr := &http.Transport{
		DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", sockPath)
		},
	}
	cl := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	resp, err := cl.Get("http://unix/")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusNoContent, resp.StatusCode)
}
