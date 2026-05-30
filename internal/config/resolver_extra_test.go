package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestScalarKeyUnmarshalable exercises the json.Marshal error fallback: a channel
// cannot be marshaled, so scalarKey falls back to the %v form.
func TestScalarKeyUnmarshalable(t *testing.T) {
	got := scalarKey(make(chan int))
	if got == "" {
		t.Fatal("expected a non-empty fallback key")
	}
}

// TestDecodeObjectFileReadError points decodeObjectFile at a directory so
// os.ReadFile returns a non-NotExist error (distinct from the absent case).
func TestDecodeObjectFileReadError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "adir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := decodeObjectFile(dir); err == nil {
		t.Fatal("expected read error for directory path")
	}
}

// TestDecodeEffectiveUnmarshalError feeds a merged object whose typed field has the
// wrong JSON type (version is an int), tripping the AgentsRC unmarshal error branch.
func TestDecodeEffectiveUnmarshalError(t *testing.T) {
	if _, err := decodeEffective(map[string]any{"version": "not-a-number"}); err == nil {
		t.Fatal("expected decode error for mistyped version field")
	}
}

// TestWriteConfigLockOpenError makes the lockfile path a directory so agentslock.Open
// fails before any section is written.
func TestWriteConfigLockOpenError(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(AgentsLockPath(proj), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfigLock(proj, map[string]LockedLayer{}); err == nil {
		t.Fatal("expected open error when lock path is a directory")
	}
}

// TestReadLockedLayersOpenError mirrors the above for the read path.
func TestReadLockedLayersOpenError(t *testing.T) {
	proj := t.TempDir()
	if err := os.MkdirAll(AgentsLockPath(proj), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := readLockedLayers(proj); err == nil {
		t.Fatal("expected open error when lock path is a directory")
	}
}
