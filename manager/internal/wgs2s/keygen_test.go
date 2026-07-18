package wgs2s

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestGenerateKeypair(t *testing.T) {
	dir := t.TempDir()
	key, err := generateKeypair(dir, "test-id")
	require.NoError(t, err)

	assert.NotEqual(t, wgtypes.Key{}, key)

	_, err = os.Stat(filepath.Join(dir, "test-id.key"))
	assert.NoError(t, err)

	_, err = os.Stat(filepath.Join(dir, "test-id.pub"))
	assert.NoError(t, err)
}

func TestLoadPrivateKey(t *testing.T) {
	dir := t.TempDir()
	original, err := generateKeypair(dir, "load-test")
	require.NoError(t, err)

	loaded, err := loadPrivateKey(dir, "load-test")
	require.NoError(t, err)
	assert.Equal(t, original, loaded)
}

func TestDeleteKeyFiles(t *testing.T) {
	t.Run("deletes existing files", func(t *testing.T) {
		dir := t.TempDir()
		_, err := generateKeypair(dir, "del-test")
		require.NoError(t, err)

		deleteKeyFiles(dir, "del-test")

		_, err = os.Stat(filepath.Join(dir, "del-test.key"))
		assert.True(t, os.IsNotExist(err))
		_, err = os.Stat(filepath.Join(dir, "del-test.pub"))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("nonexistent dir no panic", func(t *testing.T) {
		assert.NotPanics(t, func() {
			deleteKeyFiles("/nonexistent/path/that/does/not/exist", "nope")
		})
	})
}

func TestLoadPrivateKeyNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := loadPrivateKey(dir, "nonexistent")
	assert.Error(t, err)
}

// TestSaveKeyFilesAtomicTightensPerm proves saveKeyFiles writes through the
// atomic state.WriteFile path (tmp+chmod+rename) rather than os.WriteFile.
// os.WriteFile leaves a pre-existing destination's mode untouched, so a
// private key file previously created 0666 would keep leaking on re-save.
// The atomic path renames a fresh 0600 temp over it. This test fails if the
// writes are reverted to os.WriteFile (the 0666 mode would survive).
func TestSaveKeyFilesAtomicTightensPerm(t *testing.T) {
	dir := t.TempDir()
	const id = "perm-test"
	keyPath := filepath.Join(dir, id+".key")
	pubPath := filepath.Join(dir, id+".pub")

	// Seed pre-existing files with a lax mode.
	for _, p := range []string{keyPath, pubPath} {
		if err := os.WriteFile(p, []byte("stale"), 0o666); err != nil { //nolint:gosec // G306: deliberately lax to prove the rewrite tightens it
			t.Fatalf("seed %s: %v", p, err)
		}
	}

	priv, err := generateKeypair(dir, id)
	require.NoError(t, err)

	for _, p := range []string{keyPath, pubPath} {
		info, err := os.Stat(p)
		require.NoError(t, err)
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Fatalf("%s mode after rewrite: got %o, want 0600", p, mode)
		}
	}

	// Content must be the freshly generated key, not the stale seed.
	loaded, err := loadPrivateKey(dir, id)
	require.NoError(t, err)
	assert.Equal(t, priv, loaded)

	// No orphan temp files.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotEqual(t, ".tmp", filepath.Ext(e.Name()), "orphan tmp: %s", e.Name())
	}
}
