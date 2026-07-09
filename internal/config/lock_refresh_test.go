package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteRefreshLockRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := RefreshMetadata{
		Version:     "1.2.3",
		Commit:      "0123456789abcdef0123456789abcdef01234567", // 41 chars — proves no length cap
		Describe:    "v1.2.3",
		RefreshedAt: "2026-05-30T10:00:00Z",
	}
	if err := WriteRefreshLock(dir, in); err != nil {
		t.Fatalf("WriteRefreshLock: %v", err)
	}

	got, ok, err := ReadRefreshLock(dir)
	if err != nil {
		t.Fatalf("ReadRefreshLock: %v", err)
	}
	if !ok {
		t.Fatal("ReadRefreshLock: ok = false, want true")
	}
	if got != in {
		t.Errorf("ReadRefreshLock: got %+v, want %+v", got, in)
	}
}

func TestReadRefreshLock_AbsentReportsFalse(t *testing.T) {
	dir := t.TempDir()
	got, ok, err := ReadRefreshLock(dir)
	if err != nil {
		t.Fatalf("ReadRefreshLock on project with no lock: %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false with no refresh stamp, got %+v", got)
	}
}

// TestWriteRefreshLock_PreservesSiblingSections proves the refresh writer
// stages only its own "refresh" section (via agentslock.SetSection), leaving
// a sibling "units" section written by another process/writer intact —
// mirroring the install lock-section precedent.
func TestWriteRefreshLock_PreservesSiblingSections(t *testing.T) {
	dir := t.TempDir()
	units := UnitsLock{InputsDigest: "sha256:inputs", Units: map[string]LockedUnit{
		"acme:org/base@a1b2": {Kind: UnitKindLayer, Digest: "sha256:base"},
	}}
	if err := WriteUnitsLock(dir, units); err != nil {
		t.Fatalf("WriteUnitsLock: %v", err)
	}

	if err := WriteRefreshLock(dir, RefreshMetadata{Version: "1.0.0", Commit: "abc123"}); err != nil {
		t.Fatalf("WriteRefreshLock: %v", err)
	}

	gotUnits, err := ReadUnits(dir)
	if err != nil {
		t.Fatalf("ReadUnits: %v", err)
	}
	if gotUnits.InputsDigest != units.InputsDigest {
		t.Errorf("units section clobbered: inputs_digest = %q, want %q", gotUnits.InputsDigest, units.InputsDigest)
	}
	if _, ok := gotUnits.Units["acme:org/base@a1b2"]; !ok {
		t.Errorf("units section clobbered: layer entry missing, got %+v", gotUnits.Units)
	}

	gotRefresh, ok, err := ReadRefreshLock(dir)
	if err != nil {
		t.Fatalf("ReadRefreshLock: %v", err)
	}
	if !ok || gotRefresh.Commit != "abc123" {
		t.Errorf("refresh section not written: ok=%v got=%+v", ok, gotRefresh)
	}
}

// TestWriteRefreshLock_NeverTouchesManifest proves the lock writer is
// independent of .agentsrc.json: it neither requires nor creates one.
func TestWriteRefreshLock_NeverTouchesManifest(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRefreshLock(dir, RefreshMetadata{Version: "1.0.0", Commit: "abc123"}); err != nil {
		t.Fatalf("WriteRefreshLock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, AgentsRCFile)); !os.IsNotExist(err) {
		t.Fatalf("WriteRefreshLock must not create %s, stat err = %v", AgentsRCFile, err)
	}
	if _, err := os.Stat(AgentsLockPath(dir)); err != nil {
		t.Fatalf("expected %s to be written: %v", AgentsLockFile, err)
	}
}


// A directory at the lockfile path makes agentslock.Open/Flush fail, exercising
// the error returns in WriteRefreshLock/ReadRefreshLock.
func TestWriteRefreshLock_LockPathIsDirErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(AgentsLockPath(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteRefreshLock(dir, RefreshMetadata{Version: "1.0.0", Commit: "abc123"}); err == nil {
		t.Fatal("expected error when the lock path is a directory")
	}
}

func TestReadRefreshLock_LockPathIsDirErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(AgentsLockPath(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadRefreshLock(dir); err == nil {
		t.Fatal("expected error when the lock path is a directory")
	}
}

// A "refresh" section whose JSON is not a RefreshMetadata object makes
// lf.Section fail to unmarshal, exercising ReadRefreshLock's decode-error path.
func TestReadRefreshLock_MalformedSectionErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(AgentsLockPath(dir), []byte(`{"lock_version":1,"refresh":"not-an-object"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadRefreshLock(dir); err == nil {
		t.Fatal("expected a decode error for a malformed refresh section")
	}
}
