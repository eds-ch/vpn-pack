package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Syncer abstracts file and directory fsync so tests can verify both stages
// are issued. Production code uses defaultSyncer (real fsync); tests inject
// a counting fake to prove durability without relying on filesystem state.
type Syncer interface {
	SyncFile(*os.File) error
	SyncDir(path string) error
}

type defaultSyncer struct{}

func (defaultSyncer) SyncFile(f *os.File) error { return f.Sync() }

func (defaultSyncer) SyncDir(path string) error {
	d, err := os.Open(path)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// WriteFile atomically writes data to path: tmp file in same dir → fsync(tmp)
// → rename → fsync(dir). Crash at any point leaves either the old file intact
// or an orphan .tmp (cleaned up by SweepOrphanTmp on next start).
func WriteFile(path string, data []byte, perm os.FileMode) error {
	return WriteFileWith(defaultSyncer{}, path, data, perm)
}

// WriteFileWith is WriteFile with an injectable Syncer. Production callers
// should use WriteFile; tests use this directly to assert fsync invocations.
func WriteFileWith(sync Syncer, path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create tmp: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod tmp: %w", err)
	}
	if err := sync.SyncFile(tmp); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("fsync tmp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close tmp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return fmt.Errorf("rename: %w", err)
	}
	if err := sync.SyncDir(dir); err != nil {
		return fmt.Errorf("fsync dir: %w", err)
	}
	return nil
}

// LoadJSON reads and JSON-decodes path into T. Missing file → def, recovered=false.
// Read or parse failure → quarantine copy at path+".corrupt-<unix>" and return def
// with recovered=true. A genuine read error other than "not exist" surfaces as err.
func LoadJSON[T any](path string, def T) (T, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return def, false, nil
		}
		return def, false, fmt.Errorf("read: %w", err)
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		quarantine := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
		_ = os.WriteFile(quarantine, data, 0o600)
		return def, true, nil
	}
	return v, false, nil
}

// SweepOrphanTmp removes leftover *.tmp files (any name pattern) in dir.
// Called at startup to clean up after crashes between rename stages of WriteFile.
// Missing dir is a no-op.
func SweepOrphanTmp(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read dir: %w", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasSuffix(name, ".tmp") {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
	return nil
}
