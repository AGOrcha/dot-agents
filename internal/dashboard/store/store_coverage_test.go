package store

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/scoring"
)

func TestWithSubscriberCounterOption(t *testing.T) {
	s := New([]string{t.TempDir()}, WithSubscriberCounter(func() int { return 7 }))
	h, err := s.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.SubscriberCount != 7 {
		t.Fatalf("WithSubscriberCounter not wired: %d", h.SubscriberCount)
	}
}

func TestUpdateSortValBranches(t *testing.T) {
	if v := updateSortVal(Run{LastUpdate: nil}); v != 0 {
		t.Errorf("nil last_update ranks oldest (0), got %v", v)
	}
	bad := "not-a-timestamp"
	if v := updateSortVal(Run{LastUpdate: &bad}); v != 0 {
		t.Errorf("unparseable last_update ranks oldest (0), got %v", v)
	}
	good := "2026-05-28T14:03:11Z"
	if v := updateSortVal(Run{LastUpdate: &good}); v <= 0 {
		t.Errorf("valid last_update should parse to a positive unix, got %v", v)
	}
}

func TestSessionLastUpdateNoMtimes(t *testing.T) {
	s := testStore(t)
	// Craft a session whose snapshot carries no file mtimes -> nil last_update.
	ctx := sessionCtx{
		root: "r",
		snap: rootSnapshot{fileMtime: map[string]time.Time{}},
		records: []scoring.IterationRecord{
			{Iteration: 1, Agent: scoring.AgentInfo{SessionID: "s"}},
		},
	}
	if got := s.sessionLastUpdate(ctx); got != nil {
		t.Fatalf("no resolvable mtime should yield nil, got %v", *got)
	}
}

func TestMapBreakdownAndVerifiersEmpty(t *testing.T) {
	if mapBreakdown(nil) != nil {
		t.Error("empty breakdown should map to nil")
	}
	if mapVerifiers(nil) != nil {
		t.Error("empty verifiers should map to nil")
	}
}

func TestPerIterationRefUnscoredInSessionSidecar(t *testing.T) {
	dir := t.TempDir()
	writeV2Iter(t, dir, 1, "sess-x", "h", 0, false, false)
	writeV2Iter(t, dir, 2, "sess-x", "h", 0, false, false)
	// Session sidecar present with one scored + one UNSCORED per-iteration ref.
	writeSessionScore(t, dir, "sess-x", []int{1, 2}, 0.7, "good", []scoring.SessionIterRef{
		{Iteration: 1, Scored: true, Value: 0.7, Band: "good"},
		{Iteration: 2, Scored: false},
	})
	s := testStore(t, dir)
	run, err := s.GetRun(context.Background(), "sess-x")
	if err != nil {
		t.Fatal(err)
	}
	if len(run.PerIteration) != 2 {
		t.Fatalf("want 2 refs, got %d", len(run.PerIteration))
	}
	if run.PerIteration[1].Scored || run.PerIteration[1].Score != nil || run.PerIteration[1].Band != bandUnscored {
		t.Errorf("unscored sidecar ref should surface unscored: %+v", run.PerIteration[1])
	}
}

func TestReadDirMtimeSkipsSubdirs(t *testing.T) {
	dir := t.TempDir()
	writeV2Iter(t, dir, 1, "sess-a", "h", 0, false, false)
	if err := os.Mkdir(filepath.Join(dir, "iter-2.yaml"), 0o755); err != nil {
		t.Fatal(err) // a directory that name-matches must be ignored
	}
	s := testStore(t, dir)
	its, err := s.ListIterations(context.Background(), "sess-a")
	if err != nil || len(its) != 1 {
		t.Fatalf("subdir must be skipped, got %d iters err=%v", len(its), err)
	}
}

// TestReadErrorBranches covers the os.ReadFile failure paths in decodeYAML and
// resilientRecords using dangling symlinks (stat succeeds, read fails). Skipped
// where symlinks are unavailable (e.g. unprivileged Windows).
func TestReadErrorBranches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is privileged on Windows")
	}
	dir := t.TempDir()
	writeV2Iter(t, dir, 1, "sess-a", "h", 0, false, false)
	// Dangling symlink sidecar -> decodeYAML ReadFile error branch.
	if err := os.Symlink(filepath.Join(dir, "nope-score"), filepath.Join(dir, "iter-1.score.yaml")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	// Dangling symlink iter file -> LoadIterationLog errors, resilientRecords
	// then hits its ReadFile error branch on the same link.
	if err := os.Symlink(filepath.Join(dir, "nope-iter"), filepath.Join(dir, "iter-2.yaml")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	s := testStore(t, dir)
	its, err := s.ListIterations(context.Background(), "sess-a")
	if err != nil {
		t.Fatalf("unreadable files must be skipped, not fail: %v", err)
	}
	if len(its) != 1 || its[0].Iteration != 1 || its[0].Scored {
		t.Fatalf("only the good unscored iter-1 should survive: %+v", its)
	}
}
