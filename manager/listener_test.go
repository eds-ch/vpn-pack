package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenManagerSocket_CreatesSocketWithMode0660(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "manager.sock")

	ln, err := openManagerSocket(sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	info, err := os.Stat(sockPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o660), info.Mode().Perm())
}

func TestOpenManagerSocket_RemovesStaleFile(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "manager.sock")

	// Simulate a leftover file from a crashed run.
	require.NoError(t, os.WriteFile(sockPath, []byte("stale"), 0o600))

	ln, err := openManagerSocket(sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	info, err := os.Stat(sockPath)
	require.NoError(t, err)
	assert.Equal(t, os.ModeSocket, info.Mode()&os.ModeSocket,
		"path must be a socket after openManagerSocket")
}

func TestOpenManagerSocket_CreatesParentDir(t *testing.T) {
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "deeper", "manager.sock")

	ln, err := openManagerSocket(sockPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })

	info, err := os.Stat(sockPath)
	require.NoError(t, err)
	assert.NotEqual(t, 0, int(info.Mode()&os.ModeSocket))
}

func TestOpenManagerSocket_EmptyPathFails(t *testing.T) {
	ln, err := openManagerSocket("")
	if ln != nil {
		_ = ln.Close()
	}
	require.Error(t, err)
	assert.True(t, errors.Is(err, errNoSocketPath) || err.Error() != "")
}

func TestSystemdListener_NotActivatedReturnsNilNil(t *testing.T) {
	// No LISTEN_PID/LISTEN_FDS → process was not socket-activated.
	// systemdListener must return (nil, nil), not an error, so the
	// dev fallback path can take over cleanly.
	t.Setenv("LISTEN_PID", "")
	t.Setenv("LISTEN_FDS", "")
	ln, err := systemdListener()
	require.NoError(t, err)
	assert.Nil(t, ln)
}

func TestSystemdListener_WrongPidIgnored(t *testing.T) {
	// LISTEN_PID belonging to another process must not be honoured —
	// otherwise env-var leakage from an unrelated socket-activated
	// parent would point us at a random fd.
	t.Setenv("LISTEN_PID", "1")
	t.Setenv("LISTEN_FDS", "1")
	ln, err := systemdListener()
	require.NoError(t, err)
	assert.Nil(t, ln)
}

func TestSystemdListener_RejectsMultipleFDs(t *testing.T) {
	// We only ever configure ListenStream= once in vpn-pack-manager.socket;
	// receiving more than one fd is a misconfiguration that should fail
	// loudly rather than silently take fd 3 and leak fd 4+.
	t.Setenv("LISTEN_PID", strconv.Itoa(os.Getpid()))
	t.Setenv("LISTEN_FDS", "2")
	ln, err := systemdListener()
	if ln != nil {
		_ = ln.Close()
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), "want exactly 1")
}
