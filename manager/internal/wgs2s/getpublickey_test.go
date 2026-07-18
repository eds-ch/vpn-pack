package wgs2s

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestGetPublicKeyRejectsPathTraversal proves GetPublicKey gates its id
// argument with validTunnelID. Without the gate, an id like "../leak" is
// joined onto configDir and reads an arbitrary *.pub file outside the
// config directory (path traversal, read as root). This test seeds a
// secret file one level above configDir and asserts GetPublicKey neither
// returns its contents nor succeeds. It fails if the validTunnelID check
// is removed from GetPublicKey.
func TestGetPublicKeyRejectsPathTraversal(t *testing.T) {
	mgr, _ := newTestManager(t)

	parent := filepath.Dir(mgr.configDir)
	const secret = "SECRET-PUBKEY-DO-NOT-LEAK" //nolint:gosec // G101: test fixture, not a real credential
	if err := os.WriteFile(filepath.Join(parent, "leak.pub"), []byte(secret), 0o600); err != nil {
		t.Fatalf("seed leak file: %v", err)
	}

	got, err := mgr.GetPublicKey("../leak")
	require.Error(t, err, "traversal id must be rejected")
	require.NotContains(t, got, secret, "traversal must not leak file contents")
}

// TestGetPublicKeyReturnsGeneratedKey confirms the validity gate does not
// reject legitimate tunnel IDs.
func TestGetPublicKeyReturnsGeneratedKey(t *testing.T) {
	mgr, _ := newTestManager(t)
	const id = "valid-tunnel"

	priv, err := generateKeypair(mgr.configDir, id)
	require.NoError(t, err)

	got, err := mgr.GetPublicKey(id)
	require.NoError(t, err)
	require.Equal(t, priv.PublicKey().String(), got)
}
