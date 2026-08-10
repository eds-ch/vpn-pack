package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// The rendered nginx snippet embeds the per-install X-VpnPack-Token secret.
// install.sh writes it 0640; the manager's self-heal path must not widen that.
// Regression: a unifi-core upgrade wipes /data/unifi-core/config/http, the
// watcher re-creates the snippet, and os.WriteFile with 0644 left the token
// world-readable (observed on the UDM-SE after the 5.1.26 firmware update).
func TestEnsureConfigCreatesSnippetWithoutWorldRead(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "nginx-vpnpack.conf")
	dest := filepath.Join(dir, "shared-runnable-vpnpack.conf")
	require.NoError(t, os.WriteFile(src, []byte("location /vpn-pack/ { }\n"), 0o640)) //nolint:gosec // G306: 0640 is the mode under test — the snippet embeds the token secret

	m := &NginxManager{configSrc: src, configDest: dest}
	// The reload is a device-side side effect and fails off-device; the write
	// is what this test is about.
	_ = m.EnsureConfig()

	fi, err := os.Stat(dest)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), fi.Mode().Perm())
}

// The damaged installs in the field carry the *right content* with the wrong
// mode: the snippet was re-created 0644 from an intact source. EnsureConfig
// runs unconditionally at manager start, so the upgrade to a fixed build is
// the moment to repair them — but the content comparison short-circuits before
// any chmod, so the mode must be enforced independently of a rewrite.
func TestEnsureConfigTightensPermsWhenContentAlreadyMatches(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "nginx-vpnpack.conf")
	dest := filepath.Join(dir, "shared-runnable-vpnpack.conf")
	content := []byte("location /vpn-pack/ { }\n")
	require.NoError(t, os.WriteFile(src, content, 0o640))  //nolint:gosec // G306: 0640 is the mode under test
	require.NoError(t, os.WriteFile(dest, content, 0o640)) //nolint:gosec // G306: same
	require.NoError(t, os.Chmod(dest, 0o644))              //nolint:gosec // G302: 0644 reproduces the damaged install

	m := &NginxManager{configSrc: src, configDest: dest}
	require.NoError(t, m.EnsureConfig())

	fi, err := os.Stat(dest)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), fi.Mode().Perm())
}

// Installs that already took the 0644 write must converge back to 0640 on the
// next self-heal, without requiring a reinstall. os.WriteFile does not touch
// the mode of an existing file, so this needs an explicit chmod.
func TestEnsureConfigTightensPermsOnExistingWorldReadableSnippet(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "nginx-vpnpack.conf")
	dest := filepath.Join(dir, "shared-runnable-vpnpack.conf")
	require.NoError(t, os.WriteFile(src, []byte("location /vpn-pack/ { }\n"), 0o640)) //nolint:gosec // G306: 0640 is the mode under test
	require.NoError(t, os.WriteFile(dest, []byte("stale\n"), 0o644))                  //nolint:gosec // G306: 0644 reproduces the damaged install this test fixes
	require.NoError(t, os.Chmod(dest, 0o644))                                         //nolint:gosec // G302: same — the pre-fix mode is the fixture

	m := &NginxManager{configSrc: src, configDest: dest}
	_ = m.EnsureConfig()

	fi, err := os.Stat(dest)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), fi.Mode().Perm())
}
