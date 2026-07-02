package store

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/scoring"
	"go.yaml.in/yaml/v3"
)

// --- fixture helpers ---------------------------------------------------------

// repoRootDir is the dot-agents repo root, used as the scoring pipeline's
// repoDir (the same live-repo pattern internal/scoring's own tests use).
func repoRootDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	return root
}

func testRecomputeStore(t *testing.T, repoDir string, roots ...string) *RecomputeStore {
	t.Helper()
	rs := NewRecompute(testStore(t, roots...), repoDir)
	// Settle background sidecar writes before TempDir cleanup unlinks the root.
	t.Cleanup(rs.Flush)
	return rs
}

// writeIterWithMessageCount writes a v2 record whose session_tokens block
// carries message_count — the transcript_turn_count source the normalized
// scoring.IterationRecord drops.
func writeIterWithMessageCount(t *testing.T, dir string, n int, sid string, msgCount int) {
	t.Helper()
	content := fmt.Sprintf(`schema_version: 2
iteration: %d
date: "2026-05-01"
wave: "w1"
task_id: "task-%d"
commit: "commit%d"
agent:
  session_id: "%s"
  harness: "claude-code"
  model: "m"
session_tokens:
  input_tokens: 10
  output_tokens: 20
  cache_read_tokens: 90
  cache_creation_tokens: 10
  cache_hit_rate: 0.9
  message_count: %d
impl:
  summary: "impl %d"
  retries: 1
verifiers:
  - type: test
    status: pass
    gate_passed: true
    tests_added: 2
    retries: 0
`, n, n, n, sid, msgCount, n)
	writeFile(t, dir, fmt.Sprintf("iter-%d.yaml", n), content)
}

// backdate shifts a file's mtime, for staleness-window fixtures.
func backdate(t *testing.T, path string, by time.Duration) {
	t.Helper()
	past := time.Now().Add(-by)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatalf("backdate %s: %v", path, err)
	}
}

// --- fresh sidecar: raw t02 read is served untouched --------------------------

// A fresh sidecar must NOT trigger the pipeline: the response is exactly the
// t02 raw read, recompute-only fields left empty — the t02/t06 boundary from
// the other side (store_test.go pins the DiskStore half).
func TestRecomputeGetIterationFreshSidecarServesRawRead(t *testing.T) {
	rs := testRecomputeStore(t, repoRootDir(t), standardRoot(t))
	it, err := rs.GetIteration(context.Background(), "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !it.Scored || it.Score == nil || *it.Score != 0.8 || it.Band != "good" {
		t.Errorf("fresh sidecar should serve persisted score, got %+v", it)
	}
	if it.Integrity != nil || it.Objective != nil || it.IntegrityObservationCount != 0 || it.TranscriptTurnCount != nil {
		t.Errorf("fresh-sidecar path must not fill recompute-only fields: %+v", it)
	}
}

// --- recompute on miss ---------------------------------------------------------

// A missing sidecar triggers the synchronous pipeline: the response carries a
// real score plus the recompute-sourced integrity / objective /
// transcript_turn_count fields, and the sidecar is persisted in the
// background so the next read is a plain t02 hit.
func TestRecomputeGetIterationMissingSidecarRecomputes(t *testing.T) {
	dir := t.TempDir()
	writeIterWithMessageCount(t, dir, 1, "sess-r", 42)
	rs := testRecomputeStore(t, repoRootDir(t), dir)

	it, err := rs.GetIteration(context.Background(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !it.Scored || it.Score == nil || *it.Score <= 0 || *it.Score > 1 {
		t.Fatalf("recompute should produce a scored result, got %+v", it)
	}
	if it.Band == bandUnscored || it.RubricVersion != scoring.DefaultRubric().Version {
		t.Errorf("band/rubric_version not from fresh score: band=%q rv=%q", it.Band, it.RubricVersion)
	}
	if got, want := len(it.Breakdown), len(scoring.DefaultRubric().Signals); got != want {
		t.Errorf("breakdown rows = %d, want one per rubric signal (%d)", got, want)
	}
	assertRecomputeFieldsFilled(t, it, 42)

	rs.Flush()
	assertSidecarPersisted(t, dir, 1, *it.Score)
	assertNextReadIsRawHit(t, rs, *it.Score)
}

// assertRecomputeFieldsFilled checks the four t06-populated DTO fields.
func assertRecomputeFieldsFilled(t *testing.T, it Iteration, wantTurns int) {
	t.Helper()
	if it.Objective == nil {
		t.Fatal("objective must be populated by the recompute path")
	}
	// No transcript dirs were configured, so every objective check is a
	// first-class absent — present:false with the SignalSide shape intact.
	if it.Objective.RanCliCommand.Present || it.Objective.CommittedAfterTests.Present || it.Objective.ReadLoopState.Present {
		t.Errorf("objective checks without transcripts must be absent: %+v", it.Objective)
	}
	if len(it.Integrity) == 0 {
		t.Error("integrity observations must be populated (verifier row has an observed side)")
	}
	comparable := 0
	for _, row := range it.Integrity {
		if row.Comparable {
			comparable++
			if row.Delta == nil {
				t.Errorf("comparable row %q must carry a delta", row.Signal)
			}
		} else if row.Delta != nil {
			t.Errorf("non-comparable row %q must have a null delta", row.Signal)
		}
	}
	if it.IntegrityObservationCount != comparable {
		t.Errorf("integrity_observation_count = %d, want comparable rows = %d", it.IntegrityObservationCount, comparable)
	}
	if it.TranscriptTurnCount == nil || *it.TranscriptTurnCount != wantTurns {
		t.Errorf("transcript_turn_count = %v, want %d (session_tokens.message_count)", it.TranscriptTurnCount, wantTurns)
	}
}

// assertSidecarPersisted verifies the background best-effort write landed and
// round-trips the same value the response carried.
func assertSidecarPersisted(t *testing.T, dir string, n int, wantValue float64) {
	t.Helper()
	data, err := os.ReadFile(scoring.IterationScorePath(dir, n))
	if err != nil {
		t.Fatalf("sidecar not persisted after Flush: %v", err)
	}
	var ps scoring.PersistedScore
	if err := yaml.Unmarshal(data, &ps); err != nil {
		t.Fatalf("persisted sidecar unparseable: %v", err)
	}
	if !ps.Scored || ps.Value != wantValue || ps.Iteration != n {
		t.Errorf("persisted sidecar mismatch: %+v (want value %v)", ps, wantValue)
	}
}

// assertNextReadIsRawHit verifies the follow-up read is served by the raw t02
// path (recompute-only fields empty) with the persisted score.
func assertNextReadIsRawHit(t *testing.T, rs *RecomputeStore, wantValue float64) {
	t.Helper()
	it, err := rs.GetIteration(context.Background(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !it.Scored || it.Score == nil || *it.Score != wantValue {
		t.Errorf("post-persist read should serve the sidecar score %v, got %+v", wantValue, it)
	}
	if it.Integrity != nil || it.Objective != nil {
		t.Error("post-persist read is the raw path; recompute-only fields must be empty again")
	}
}

// A sidecar older than its iter-N.yaml record is stale: the checkpoint
// rewrote the record after scoring, so the pipeline reruns.
func TestRecomputeGetIterationStaleSidecarRecomputes(t *testing.T) {
	dir := t.TempDir()
	writeIterWithMessageCount(t, dir, 3, "sess-s", 7)
	writeIterScore(t, dir, 3, 0.9, "excellent")
	backdate(t, scoring.IterationScorePath(dir, 3), time.Hour)

	rs := testRecomputeStore(t, repoRootDir(t), dir)
	it, err := rs.GetIteration(context.Background(), "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if it.Objective == nil || it.TranscriptTurnCount == nil {
		t.Errorf("stale sidecar must take the recompute path, got %+v", it)
	}
}

// A corrupt sidecar never parses into the snapshot, so it counts as missing —
// "the dashboard never says no score when the scorer could just answer."
func TestRecomputeGetIterationCorruptSidecarRecomputes(t *testing.T) {
	dir := t.TempDir()
	writeIterWithMessageCount(t, dir, 4, "sess-c", 5)
	writeFile(t, dir, "iter-4.score.yaml", "not: [valid yaml")

	rs := testRecomputeStore(t, repoRootDir(t), dir)
	it, err := rs.GetIteration(context.Background(), "", 4)
	if err != nil {
		t.Fatal(err)
	}
	if it.Objective == nil || !it.Scored {
		t.Errorf("corrupt sidecar must recompute, got %+v", it)
	}
}

// A sidecar whose filename disagrees with its iteration field parses into the
// snapshot under the content's number but has no mtime under the conventional
// name — the filename convention wins and the iteration recomputes.
func TestRecomputeGetIterationMismatchedSidecarNameRecomputes(t *testing.T) {
	dir := t.TempDir()
	writeIterWithMessageCount(t, dir, 5, "sess-m", 9)
	mismatched := scoring.Score{Iteration: 5, RubricVersion: scoring.RubricVersion, Scored: true, Value: 0.5, Band: "fair"}
	data, err := yaml.Marshal(scoring.BuildPersistedScore(mismatched, scoring.IterationRecord{}))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "iter-9.score.yaml", string(data))

	rs := testRecomputeStore(t, repoRootDir(t), dir)
	it, err := rs.GetIteration(context.Background(), "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if it.Objective == nil {
		t.Errorf("mismatched sidecar name must recompute, got %+v", it)
	}
}

// --- degradation ----------------------------------------------------------------

// A pipeline failure (here: repoDir is not a git repository) degrades to the
// raw t02 read instead of erroring — spec R10: never trade a valid raw answer
// for a recompute error.
func TestRecomputeGetIterationPipelineFailureFallsBackToRaw(t *testing.T) {
	dir := t.TempDir()
	writeIterWithMessageCount(t, dir, 1, "sess-f", 3)
	var buf bytes.Buffer
	rs := NewRecompute(New([]string{dir}, WithLogger(slog.New(slog.NewTextHandler(&buf, nil)))), t.TempDir())

	it, err := rs.GetIteration(context.Background(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if it.Scored || it.Band != bandUnscored || it.Integrity != nil || it.Objective != nil {
		t.Errorf("pipeline failure must serve the raw read, got %+v", it)
	}
	if !strings.Contains(buf.String(), "recompute-on-miss pipeline failed") {
		t.Errorf("pipeline failure should be logged, got: %s", buf.String())
	}
}

// buildSignalSet's no-matching-set branch is unreachable through GetIteration
// (the snapshot and pipeline read the same log), so it is pinned directly.
func TestRecomputeBuildSignalSetNoMatchingIteration(t *testing.T) {
	dir := t.TempDir()
	writeIterWithMessageCount(t, dir, 1, "sess-x", 1)
	rs := testRecomputeStore(t, repoRootDir(t), dir)
	if _, ok := rs.buildSignalSet(dir, 999); ok {
		t.Error("no signal set should match iteration 999")
	}
}

// Error contracts pass through the decorator unchanged.
func TestRecomputeGetIterationErrorPassthrough(t *testing.T) {
	rs := testRecomputeStore(t, repoRootDir(t), standardRoot(t))
	if _, err := rs.GetIteration(context.Background(), t.TempDir(), 1); err != ErrRootNotAllowed {
		t.Errorf("unlisted root: got %v, want ErrRootNotAllowed", err)
	}
	if _, err := rs.GetIteration(context.Background(), "", 404); err != ErrNotFound {
		t.Errorf("missing iteration: got %v, want ErrNotFound", err)
	}
}

// The background sidecar write is best-effort: an unwritable root is logged
// and the response is unaffected.
func TestRecomputePersistAsyncWriteFailureIsLogged(t *testing.T) {
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("permission-bit fixture needs a non-root POSIX environment")
	}
	dir := t.TempDir()
	writeIterWithMessageCount(t, dir, 1, "sess-w", 2)
	var buf bytes.Buffer
	rs := NewRecompute(New([]string{dir}, WithLogger(slog.New(slog.NewTextHandler(&buf, nil)))), repoRootDir(t))

	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	it, err := rs.GetIteration(context.Background(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !it.Scored {
		t.Errorf("write failure must not affect the response: %+v", it)
	}
	rs.Flush()
	if !strings.Contains(buf.String(), "best-effort sidecar write failed") {
		t.Errorf("failed write should be logged, got: %s", buf.String())
	}
	if _, err := os.Stat(scoring.IterationScorePath(dir, 1)); !os.IsNotExist(err) {
		t.Errorf("sidecar must not exist after failed write, stat err = %v", err)
	}
}

// --- small helpers ---------------------------------------------------------------

func TestReadMessageCount(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "no-tokens.yaml", "iteration: 1\n")
	writeFile(t, dir, "no-count.yaml", "session_tokens:\n  input_tokens: 5\n")
	writeFile(t, dir, "bad.yaml", "not: [valid")
	writeFile(t, dir, "counted.yaml", "session_tokens:\n  message_count: 17\n")

	cases := []struct {
		name string
		file string
		want *int
	}{
		{"missing file", "absent.yaml", nil},
		{"no session_tokens block", "no-tokens.yaml", nil},
		{"no message_count field", "no-count.yaml", nil},
		{"unparseable yaml", "bad.yaml", nil},
		{"captured", "counted.yaml", intPtr(17)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := readMessageCount(filepath.Join(dir, tc.file))
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("got %d, want nil", *got)
			case tc.want != nil && (got == nil || *got != *tc.want):
				t.Errorf("got %v, want %d", got, *tc.want)
			}
		})
	}
}

// mapIntegrity projects deltas only for comparable pairs.
func TestMapIntegrityDeltaSemantics(t *testing.T) {
	obs := []scoring.IntegrityObservation{
		{
			Signal:   scoring.SignalTests,
			Role:     scoring.RoleImpl,
			Claimed:  scoring.PresentSignal(1.0, "claimed pass"),
			Observed: scoring.PresentSignal(0.5, "observed half"),
		},
		{
			Signal:  scoring.SignalLanded,
			Role:    scoring.RoleImpl,
			Claimed: scoring.PresentSignal(1.0, "claimed only"),
		},
	}
	rows := mapIntegrity(obs)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if !rows[0].Comparable || rows[0].Delta == nil || *rows[0].Delta != -0.5 {
		t.Errorf("comparable over-claim should carry delta -0.5: %+v", rows[0])
	}
	if rows[1].Comparable || rows[1].Delta != nil {
		t.Errorf("one-sided observation must not be comparable: %+v", rows[1])
	}
	if mapIntegrity(nil) != nil {
		t.Error("no observations must map to nil (omitted from JSON)")
	}
	if got := comparableCount(obs); got != 1 {
		t.Errorf("comparableCount = %d, want 1", got)
	}
}
