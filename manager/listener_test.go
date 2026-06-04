package main

import (
	"errors"
	"os"
	"path/filepath"
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
