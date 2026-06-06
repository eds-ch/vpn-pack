package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"unifi-tailscale/manager/state"

	"github.com/stretchr/testify/require"
)

// TestManifestSaveLeavesNoOrphanTmp guards against regression to the
// pre-Phase-3 saveLocked that wrote "<path>.tmp" with os.WriteFile+os.Rename
// (no fsync). With state.WriteFile any *.tmp under dir is treated as orphan,
// so we assert none remains after a successful mutation.
func TestManifestSaveLeavesNoOrphanTmp(t *testing.T) {
	dir := t.TempDir()
	m, err := state.LoadManifest(filepath.Join(dir, "manifest.json"))
	require.NoError(t, err)

	require.NoError(t, m.SetTailscaleZone("z1", "VPN Pack: Tailscale", []string{"p1"}, "VPN"))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("orphan tmp left after save: %s", e.Name())
		}
	}
}
