package scoring

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ScoreIteration on a real iter-log directory + git repo returns a Score
// with the requested iteration number, matching what the headless `da
// score iteration --recompute` flow would emit. Uses the existing
// testdata/iterlog fixture and the live repo as repoDir (same pattern
// TestBuildSignalSets uses).
func TestScoreIterationHappyPath(t *testing.T) {
	iterLogDir := filepath.Join("testdata", "iterlog")
	repoDir := filepath.Join("..", "..")
	if _, err := os.Stat(iterLogDir); err != nil {
		t.Skipf("iter-log fixture not present: %v", err)
	}
	score, rec, err := ScoreIteration(iterLogDir, repoDir, 1)
	if err != nil {
		t.Fatalf("ScoreIteration: %v", err)
	}
	if score.Iteration != 1 {
		t.Errorf("score.Iteration = %d, want 1", score.Iteration)
	}
	if rec.Iteration != 1 {
		t.Errorf("rec.Iteration = %d, want 1", rec.Iteration)
	}
	if score.RubricVersion != DefaultRubric().Version {
		t.Errorf("RubricVersion = %q, want %q", score.RubricVersion, DefaultRubric().Version)
	}
}

// A missing iteration surfaces a clear error rather than a zero Score.
// close-task uses the error to decide whether to surface "iter not in
// log" vs "rubric collision" — silent zero would confuse the operator.
func TestScoreIterationMissingIterErrors(t *testing.T) {
	iterLogDir := filepath.Join("testdata", "iterlog")
	repoDir := filepath.Join("..", "..")
	if _, err := os.Stat(iterLogDir); err != nil {
		t.Skipf("iter-log fixture not present: %v", err)
	}
	_, _, err := ScoreIteration(iterLogDir, repoDir, 999)
	if err == nil {
		t.Fatal("expected error for missing iter-999, got nil")
	}
	if !strings.Contains(err.Error(), "iter-999") {
		t.Errorf("error should name the missing iter: %v", err)
	}
}

// An iter-log dir that fails to load (e.g. malformed YAML) propagates
// the load error wrapped with "load iteration log:" context.
func TestScoreIterationSurfacesLoadError(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "iter-1.yaml"), []byte("not: [valid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := ScoreIteration(dir, t.TempDir(), 1)
	if err == nil {
		t.Fatal("expected load error, got nil")
	}
	if !strings.Contains(err.Error(), "load iteration log") {
		t.Errorf("error should mention load step: %v", err)
	}
}

// Non-git repoDir surfaces a BuildSignalSets error through the wrapper.
func TestScoreIterationSurfacesBuildSignalSetsError(t *testing.T) {
	iterLogDir := filepath.Join("testdata", "iterlog")
	if _, err := os.Stat(iterLogDir); err != nil {
		t.Skipf("iter-log fixture not present: %v", err)
	}
	nonGit := t.TempDir()
	_, _, err := ScoreIteration(iterLogDir, nonGit, 1)
	if err == nil {
		t.Fatal("expected build-signal-sets error, got nil")
	}
	if !strings.Contains(err.Error(), "build signal sets") {
		t.Errorf("error should mention build step: %v", err)
	}
}

// ScoreIterationWithSignals and ScoreIteration are one pipeline, not two:
// the wrapper must produce byte-identical Score + IterationRecord results,
// with the SignalSet as the only addition. This pins the R2 dashboard's
// recompute-on-miss consumer (t06) to the same scores close-task writes —
// any future divergence between the two calls is a bug, not evolution.
func TestScoreIterationWithSignalsMatchesScoreIteration(t *testing.T) {
	iterLogDir := filepath.Join("testdata", "iterlog")
	repoDir := filepath.Join("..", "..")
	if _, err := os.Stat(iterLogDir); err != nil {
		t.Skipf("iter-log fixture not present: %v", err)
	}
	score, rec, err := ScoreIteration(iterLogDir, repoDir, 1)
	if err != nil {
		t.Fatalf("ScoreIteration: %v", err)
	}
	scoreWS, set, recWS, err := ScoreIterationWithSignals(iterLogDir, repoDir, 1)
	if err != nil {
		t.Fatalf("ScoreIterationWithSignals: %v", err)
	}
	if !reflect.DeepEqual(score, scoreWS) {
		t.Errorf("scores diverge:\n ScoreIteration            = %+v\n ScoreIterationWithSignals = %+v", score, scoreWS)
	}
	if !reflect.DeepEqual(rec, recWS) {
		t.Errorf("records diverge:\n ScoreIteration            = %+v\n ScoreIterationWithSignals = %+v", rec, recWS)
	}
	if set.Iteration != 1 {
		t.Errorf("set.Iteration = %d, want 1", set.Iteration)
	}
}
