package crgbehavior

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// realHistoryTasks bounds the in-suite run: the full pinned corpus is a
// decommission-evidence run (tools/crgbehaviorgate), not a unit test, and each
// task costs one live Python query.
const realHistoryTasks = 3

// TestBehaviorGate_RealHistoryCorpus is the §11.4 criterion-2 gate wired into
// the Go suite: it replays the pinned REAL review tasks against the legacy
// Python bridge and the kg-native adapter, and fails on a gating divergence.
// It SKIPS with a clear message where the legacy side cannot be driven (no
// code-review-graph install, or no built .code-review-graph/graph.db) — a
// missing legacy bridge is an environment fact, not a behavior divergence.
func TestBehaviorGate_RealHistoryCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("behavior-preservation gate drives a live Python subprocess per task")
	}
	repo := repoRoot(t)
	manifest, err := LoadManifest(filepath.Join(repo, DefaultManifestPath))
	if err != nil {
		t.Fatalf("the pinned corpus must load: %v", err)
	}
	report, err := RunLive(Config{RepoRoot: repo, Manifest: manifest, MaxTasks: realHistoryTasks}, repo)
	if errors.Is(err, ErrBridgeUnavailable) {
		t.Skipf("SKIP: legacy CRG bridge unavailable, dual-read not executed (%v); "+
			"see testdata/crg-behavior/BEHAVIOR.md to run it locally", err)
	}
	if err != nil {
		t.Fatalf("gate run: %v", err)
	}
	if !report.Pass() {
		var buf bytes.Buffer
		report.Render(&buf)
		t.Fatalf("CRG behavior-preservation gate FAILED:\n%s", buf.String())
	}
}

// repoRoot resolves the repository root from this package's directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("working dir: %v", err)
	}
	return filepath.Dir(filepath.Dir(wd))
}
