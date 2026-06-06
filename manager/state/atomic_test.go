package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

type fakeSyncer struct {
	fileSyncs atomic.Int64
	dirSyncs  atomic.Int64
	failFile  bool
}

func (s *fakeSyncer) SyncFile(*os.File) error {
	s.fileSyncs.Add(1)
	if s.failFile {
		return errors.New("simulated fsync fail")
	}
	return nil
}

func (s *fakeSyncer) SyncDir(string) error {
	s.dirSyncs.Add(1)
	return nil
}

func TestWriteFileIssuesFsyncOnFileAndDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.json")
	fs := &fakeSyncer{}
	if err := WriteFileWith(fs, p, []byte(`{"k":1}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if fs.fileSyncs.Load() != 1 {
		t.Fatalf("file fsync called %d times, want 1", fs.fileSyncs.Load())
	}
	if fs.dirSyncs.Load() != 1 {
		t.Fatalf("dir fsync called %d times, want 1", fs.dirSyncs.Load())
	}
	got, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != `{"k":1}` {
		t.Fatalf("got %q", got)
	}
}

func TestWriteFileAbortsOnFileFsyncFailureAndRemovesTmp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.json")
	fs := &fakeSyncer{failFile: true}
	if err := WriteFileWith(fs, p, []byte("{}"), 0o600); err == nil {
		t.Fatal("expected fsync error")
	}
	if _, err := os.Stat(p); err == nil {
		t.Fatal("destination must not be created when fsync failed")
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("orphan tmp left after fsync failure: %s", e.Name())
		}
	}
}

func TestWriteFileEndToEnd(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.json")
	if err := WriteFile(p, []byte(`{"k":1}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != `{"k":1}` {
		t.Fatalf("got %q", got)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("orphan tmp remains: %s", e.Name())
		}
	}

	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v, want 0600", info.Mode().Perm())
	}
}

func TestWriteFileOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.json")
	if err := os.WriteFile(p, []byte("old"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := WriteFile(p, []byte("new"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "new" {
		t.Fatalf("got %q, want %q", got, "new")
	}
}

func TestLoadJSONRecoversFromTruncation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.json")
	if err := os.WriteFile(p, []byte("{not"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	type T struct{ K int }
	got, recovered, err := LoadJSON[T](p, T{K: 7})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !recovered {
		t.Fatal("recovered flag should be true on parse failure")
	}
	if got.K != 7 {
		t.Fatalf("got K=%d, want default 7", got.K)
	}
	bak, _ := filepath.Glob(filepath.Join(dir, "data.json.corrupt-*"))
	if len(bak) == 0 {
		t.Fatal("expected quarantine .corrupt-* file")
	}
	if _, err := json.Marshal(got); err != nil {
		t.Fatalf("marshal default: %v", err)
	}
}

func TestLoadJSONMissingFileReturnsDefaultNoRecovered(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "no-such-file.json")
	type T struct{ K int }
	got, recovered, err := LoadJSON[T](p, T{K: 42})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if recovered {
		t.Fatal("recovered should be false for missing file")
	}
	if got.K != 42 {
		t.Fatalf("got K=%d, want default 42", got.K)
	}
}

func TestLoadJSONValidFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "data.json")
	type T struct{ K int }
	if err := os.WriteFile(p, []byte(`{"K":99}`), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, recovered, err := LoadJSON[T](p, T{K: 1})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if recovered {
		t.Fatal("recovered should be false for valid file")
	}
	if got.K != 99 {
		t.Fatalf("got K=%d, want 99", got.K)
	}
}

func TestSweepOrphanTmp(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "data.json.tmp"), []byte("partial"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "data.json.123456.tmp"), []byte("partial-createtmp"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.json"), []byte("ok"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SweepOrphanTmp(dir); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("tmp remains: %s", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.json")); err != nil {
		t.Fatalf("keep.json was removed: %v", err)
	}
}

func TestSweepOrphanTmpMissingDir(t *testing.T) {
	if err := SweepOrphanTmp(filepath.Join(t.TempDir(), "does-not-exist")); err != nil {
		t.Fatalf("missing dir should be no-op, got: %v", err)
	}
}
