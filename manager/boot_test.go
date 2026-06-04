package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSweepStartupOrphanTmpsRemovesTmpInGivenDirs(t *testing.T) {
	d1 := t.TempDir()
	d2 := t.TempDir()

	for _, p := range []string{
		filepath.Join(d1, "manifest.json.tmp"),
		filepath.Join(d1, "manifest.json.abc.tmp"),
		filepath.Join(d2, "tunnels.json.tmp"),
	} {
		if err := os.WriteFile(p, []byte("partial"), 0o600); err != nil {
			t.Fatalf("seed %s: %v", p, err)
		}
	}

	if err := os.WriteFile(filepath.Join(d1, "manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("seed real file: %v", err)
	}

	sweepStartupOrphanTmps([]string{d1, d2})

	for _, d := range []string{d1, d2} {
		entries, err := os.ReadDir(d)
		if err != nil {
			t.Fatalf("read %s: %v", d, err)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".tmp") {
				t.Fatalf("%s/%s remained after sweep", d, e.Name())
			}
		}
	}

	if _, err := os.Stat(filepath.Join(d1, "manifest.json")); err != nil {
		t.Fatalf("non-tmp file was removed: %v", err)
	}
}

func TestSweepStartupOrphanTmpsToleratesMissingDir(t *testing.T) {
	d := filepath.Join(t.TempDir(), "does-not-exist")
	sweepStartupOrphanTmps([]string{d})
}
