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
