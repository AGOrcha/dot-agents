package workflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A brand-new project with no iter-log directory yields N=1 — the schema's
// enforced minimum. close-task can call this on the very first close
// without first MkdirAll'ing.
func TestNextIterationNumberMissingDirYields1(t *testing.T) {
	got, err := NextIterationNumber(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("NextIterationNumber(absent): %v", err)
	}
	if got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

// An empty iter-log directory also yields N=1.
func TestNextIterationNumberEmptyDirYields1(t *testing.T) {
	dir := t.TempDir()
	got, err := NextIterationNumber(dir)
	if err != nil {
		t.Fatalf("NextIterationNumber(empty): %v", err)
	}
	if got != 1 {
		t.Errorf("got %d, want 1", got)
	}
}

// With existing iter-N.yaml entries, the next number is max+1 so each
// close opens a fresh iteration entry.
func TestNextIterationNumberReturnsMaxPlus1(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"iter-1.yaml", "iter-3.yaml", "iter-2.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NextIterationNumber(dir)
	if err != nil {
		t.Fatalf("NextIterationNumber: %v", err)
	}
	if got != 4 {
		t.Errorf("got %d, want 4", got)
	}
}

// Non-canonical names — including the R1 per-iteration score sidecars
// (iter-N.score.yaml) — are ignored so they cannot inflate the max-scan
// and make close-task skip iteration numbers.
func TestNextIterationNumberIgnoresNonCanonical(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"iter-1.yaml",
		"iter-1.score.yaml",          // R1 sidecar
		"iter-99.score.yaml",         // R1 sidecar with high N
		"historical.yaml",            // legacy aggregate
		"iter-foo.yaml",              // non-numeric
		"session-abc-123.score.yaml", // R1 session sidecar
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x: 1\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := NextIterationNumber(dir)
	if err != nil {
		t.Fatalf("NextIterationNumber: %v", err)
	}
	if got != 2 {
		t.Errorf("got %d, want 2 (only iter-1.yaml counts)", got)
	}
}

// A subdirectory in iter-log dir is ignored (some workflows nest fixtures).
func TestNextIterationNumberIgnoresSubdirs(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "iter-5.yaml"), 0o755); err != nil {
		// directory accidentally named like an iter file
		t.Fatal(err)
	}
	got, err := NextIterationNumber(dir)
	if err != nil {
		t.Fatalf("NextIterationNumber: %v", err)
	}
	if got != 1 {
		t.Errorf("got %d, want 1 (subdir should not count)", got)
	}
}

// Any non-IsNotExist error from os.ReadDir propagates wrapped with the
// "scoring next iteration: read %s" context so log triage can find the
// source. Drives the branch via the nextIterReadDir seam — fixturing a
// real ReadDir error portably across POSIX (chmod-0 / ENOTDIR) and
// Windows (which ignores chmod and treats files-as-dirs leniently) is
// the case the seam exists for.
func TestNextIterationNumberReportsReadError(t *testing.T) {
	prior := nextIterReadDir
	nextIterReadDir = func(string) ([]os.DirEntry, error) {
		return nil, errors.New("read boom")
	}
	t.Cleanup(func() { nextIterReadDir = prior })
	_, err := NextIterationNumber("/anywhere")
	if err == nil {
		t.Fatal("expected wrapped ReadDir error, got nil")
	}
	if !strings.Contains(err.Error(), "scoring next iteration: read") {
		t.Errorf("error not wrapped with the read context: %v", err)
	}
}

// DefaultIterationRole returns "impl" — the role close-task assumes when
// no stronger context tells us otherwise. Pin the value so renames or
// stealth changes become a test failure.
func TestDefaultIterationRoleImpl(t *testing.T) {
	if got := DefaultIterationRole(); got != "impl" {
		t.Errorf("DefaultIterationRole = %q, want \"impl\"", got)
	}
}

// IterationLogDir is the one canonical computation for the iter-log path —
// every caller in the package must use it so close-task, auto-derivers,
// and the existing checkpoint flow stay in sync.
func TestIterationLogDirShape(t *testing.T) {
	got := IterationLogDir("/some/project")
	want := filepath.Join("/some/project", ".agents", "active", "iteration-log")
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
