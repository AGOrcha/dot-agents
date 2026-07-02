package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// demoWatermark is a representative task-owned watermark shape; the package
// treats it as opaque, so any struct exercises the full round trip.
type demoWatermark struct {
	LastIterProcessed int    `yaml:"last_iter_processed"`
	RubricVersion     string `yaml:"rubric_version"`
}

// Path composes the D3 sidecar location from repo root and task name.
func TestPathShape(t *testing.T) {
	got := Path("/repo", "iterlog-ingester")
	want := filepath.Join("/repo", ".agents", "active", "service-state", "iterlog-ingester.watermark.yaml")
	if got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

// Save then Load round-trips the watermark, creating the state dir on the way.
func TestSaveLoadRoundTrip(t *testing.T) {
	path := Path(t.TempDir(), "demo")
	in := demoWatermark{LastIterProcessed: 7, RubricVersion: "2.1.0"}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	var out demoWatermark
	found, err := Load(path, &out)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !found {
		t.Fatal("Load found = false, want true")
	}
	if out != in {
		t.Errorf("round trip = %+v, want %+v", out, in)
	}
}

// A missing watermark is the "start from scratch" case: found=false, nil error,
// and the destination value is left untouched.
func TestLoadAbsent(t *testing.T) {
	var out demoWatermark
	out.LastIterProcessed = 3
	found, err := Load(Path(t.TempDir(), "demo"), &out)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Error("Load found = true for absent watermark, want false")
	}
	if out.LastIterProcessed != 3 {
		t.Errorf("Load mutated destination on absent file: %+v", out)
	}
}

// A corrupt watermark surfaces a parse error rather than silently resetting.
func TestLoadCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.watermark.yaml")
	if err := os.WriteFile(path, []byte("not: [valid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out demoWatermark
	if _, err := Load(path, &out); err == nil || !strings.Contains(err.Error(), "parse watermark") {
		t.Errorf("Load corrupt = %v, want parse error", err)
	}
}

// An unreadable path (a directory) surfaces a read error distinct from absent.
func TestLoadReadError(t *testing.T) {
	var out demoWatermark
	if _, err := Load(t.TempDir(), &out); err == nil || !strings.Contains(err.Error(), "read watermark") {
		t.Errorf("Load dir = %v, want read error", err)
	}
}

// A blocked parent (a path component is a regular file) reads as absent,
// matching Windows' not-exist mapping for the same shape; Save is the layer
// that surfaces the broken hierarchy.
func TestLoadBlockedParentIsAbsent(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "service-state")
	if err := os.WriteFile(blocker, []byte("file, not dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out demoWatermark
	found, err := Load(filepath.Join(blocker, "demo.watermark.yaml"), &out)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if found {
		t.Error("Load found = true under blocked parent, want false")
	}
}

// failingMarshaler forces yaml.Marshal to return an error.
type failingMarshaler struct{}

func (failingMarshaler) MarshalYAML() (any, error) {
	return nil, errors.New("refuses to marshal")
}

// A value YAML cannot marshal is reported as a marshal error.
func TestSaveMarshalError(t *testing.T) {
	err := Save(Path(t.TempDir(), "demo"), failingMarshaler{})
	if err == nil || !strings.Contains(err.Error(), "marshal watermark") {
		t.Errorf("Save = %v, want marshal error", err)
	}
}

// A state-dir path blocked by an existing file surfaces the mkdir error.
func TestSaveMkdirError(t *testing.T) {
	repo := t.TempDir()
	blocker := filepath.Join(repo, ".agents")
	if err := os.WriteFile(blocker, []byte("file, not dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Save(Path(repo, "demo"), demoWatermark{})
	if err == nil || !strings.Contains(err.Error(), "create state dir") {
		t.Errorf("Save blocked = %v, want mkdir error", err)
	}
}

// A write failure in the atomic writer is wrapped with the watermark path.
func TestSaveWriteError(t *testing.T) {
	dir := t.TempDir()
	// The final rename target is a directory, so WriteFileAtomic's rename fails.
	path := filepath.Join(dir, "demo.watermark.yaml")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	err := Save(path, demoWatermark{})
	if err == nil || !strings.Contains(err.Error(), "write watermark") {
		t.Errorf("Save onto dir = %v, want write error", err)
	}
}
