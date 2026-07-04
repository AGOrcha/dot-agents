package store_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/harness"
	"github.com/AGOrcha/dot-agents/internal/eval/runner"
	"github.com/AGOrcha/dot-agents/internal/eval/scoringbridge"
	"github.com/AGOrcha/dot-agents/internal/eval/store"
	goverifier "github.com/AGOrcha/dot-agents/internal/eval/verifier/golang"
	"github.com/AGOrcha/dot-agents/internal/fsops"
	"github.com/AGOrcha/dot-agents/internal/scoring"
	"go.yaml.in/yaml/v3"
)

// ---- fixtures ---------------------------------------------------------------

const (
	fixedRunID    = "eval-store-test-01"
	fixedPrompt   = "Implement function Foo."
	fixedHarness  = "test-harness"
	fixedModel    = "test-model"
	fixedSession  = "sess-store-001"
	fixedBaseSHA  = "0000000000000000000000000000000000abcdef"
	agentStdout   = "agent produced this output"
	agentExitCode = 0
)

var errBoom = errors.New("boom")

// wantDigest computes the "sha256:<hex>" digest the store is expected to emit.
func wantDigest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// testSpec returns a minimal valid v1 TaskSpec fixture.
func testSpec() *eval.TaskSpec {
	return &eval.TaskSpec{
		TaskSpecVersion: eval.CurrentTaskSpecVersion,
		TaskID:          "kg-go-store-001",
		Language:        eval.LanguageGo,
		Difficulty:      eval.DifficultyMedium,
		GeneratedFrom: eval.GeneratedFrom{
			Kind:       eval.KindKGTemplate,
			TemplateID: "impl-pure-fn",
		},
		Prompt: fixedPrompt,
		Verification: eval.Verification{
			BuildCmd:       []string{"go", "build", "./..."},
			TestCmd:        []string{"go", "test", "./..."},
			TimeoutSeconds: 60,
		},
	}
}

// buildTestRun creates a harness.EvalRun whose score-stage artifacts
// (iteration-log/iter-1.yaml + iter-1.score.yaml) are written by
// scoringbridge.ScoreRun into the run dir under scoreRoot. Pointing the store at
// the SAME root exercises the merged-harness "adopt in place" wiring; pointing
// it at a DIFFERENT root exercises the scratch "copy in" wiring.
func buildTestRun(t *testing.T, scoreRoot string) harness.EvalRun {
	t.Helper()
	runDir := store.RunDir(scoreRoot, fixedRunID)

	sbRun := scoringbridge.EvalRun{
		RunID:      fixedRunID,
		RunDir:     runDir,
		BaseCommit: fixedBaseSHA,
		Spec:       testSpec(),
		Agent: scoringbridge.AgentTelemetry{
			SessionID: fixedSession,
			Harness:   fixedHarness,
			Model:     fixedModel,
			Retries:   0,
			Tokens: &scoring.TokenUsage{
				InputTokens:  1000,
				OutputTokens: 200,
				CacheHitRate: 0.75,
			},
		},
		Verify: scoringbridge.VerifyResult{
			BuildRan: true, BuildPassed: true,
			TestRan: true, TestPassed: true,
		},
		FinishedAt: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
	}
	sbRes, err := scoringbridge.ScoreRun(sbRun)
	if err != nil {
		t.Fatalf("scoringbridge.ScoreRun: %v", err)
	}

	return harness.EvalRun{
		Spec:       testSpec(),
		RunID:      fixedRunID,
		RunDir:     runDir,
		BaseCommit: fixedBaseSHA,
		Run: runner.Result{
			Stdout:   []byte(agentStdout),
			ExitCode: agentExitCode,
			Duration: 2 * time.Second,
			Telemetry: runner.AgentTelemetry{
				SessionID: fixedSession,
				Harness:   fixedHarness,
				Model:     fixedModel,
				Retries:   0,
			},
		},
		Verify: &goverifier.VerifyResult{
			Passed:   true,
			Phase:    goverifier.PhaseTest,
			ExitCode: 0,
			Duration: 300 * time.Millisecond,
		},
		Score: sbRes,
	}
}

// mustFileExist asserts path exists and returns its raw bytes.
func mustFileExist(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected file %s to exist: %v", path, err)
	}
	return data
}

// mustNotExist asserts path does not exist.
func mustNotExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected %s to be absent, stat err = %v", path, err)
	}
}

// assertAllFourPresent checks the canonical four-file layout is present and
// each store-owned / adopted file parses.
func assertAllFourPresent(t *testing.T, res store.Result) {
	t.Helper()
	for _, p := range []string{res.TaskspecPath, res.EvalRunPath, res.RecordPath, res.ScorePath} {
		mustFileExist(t, p)
	}
	if _, err := eval.ParseTaskSpec(mustFileExist(t, res.TaskspecPath)); err != nil {
		t.Errorf("ParseTaskSpec(taskspec.yaml) = %v, want nil", err)
	}
	records, err := scoring.LoadIterationLog(res.IterLogDir)
	if err != nil {
		t.Fatalf("LoadIterationLog() = %v, want nil", err)
	}
	if len(records) != 1 || records[0].TaskID != testSpec().TaskID {
		t.Errorf("iteration log = %+v, want 1 record for %q", records, testSpec().TaskID)
	}
}

// ---- (a) create case: run dir absent → store creates it + all four files ----

func TestWriteEvalRun_CreateWhenRunDirAbsent(t *testing.T) {
	scoreRoot := t.TempDir() // score stage wrote its artifacts here (a scratch dir)
	run := buildTestRun(t, scoreRoot)

	root := t.TempDir() // canonical root — the run dir does NOT exist yet
	mustNotExist(t, store.RunDir(root, fixedRunID))

	res, err := store.WriteEvalRun(run, root)
	if err != nil {
		t.Fatalf("WriteEvalRun() = %v, want nil", err)
	}
	if res.RunDir != store.RunDir(root, fixedRunID) {
		t.Errorf("Result.RunDir = %q, want canonical", res.RunDir)
	}
	assertAllFourPresent(t, res)

	// The iteration-log artifacts were copied in from the scratch score dir, so
	// the canonical copies match the score-stage originals byte-for-byte.
	if string(mustFileExist(t, res.RecordPath)) != string(mustFileExist(t, run.Score.RecordPath)) {
		t.Errorf("copied iter-1.yaml diverges from the score-stage original")
	}
	if string(mustFileExist(t, res.ScorePath)) != string(mustFileExist(t, run.Score.ScorePath)) {
		t.Errorf("copied iter-1.score.yaml diverges from the score-stage original")
	}
}

// ---- (b) adopt case: score stage wrote iteration-log INTO the canonical dir --

func TestWriteEvalRun_AdoptsInPlaceIterationLog(t *testing.T) {
	root := t.TempDir()
	// Score stage writes iteration-log INTO the canonical run dir (merged wiring).
	run := buildTestRun(t, root)

	// RecordPath/ScorePath already resolve inside the canonical run dir.
	canonicalIter := filepath.Join(store.RunDir(root, fixedRunID), "iteration-log")
	if filepath.Dir(run.Score.RecordPath) != canonicalIter {
		t.Fatalf("fixture RecordPath %q not under canonical %q", run.Score.RecordPath, canonicalIter)
	}
	origRecord := mustFileExist(t, run.Score.RecordPath)
	origScore := mustFileExist(t, run.Score.ScorePath)

	res, err := store.WriteEvalRun(run, root)
	if err != nil {
		t.Fatalf("WriteEvalRun() = %v, want nil", err)
	}
	assertAllFourPresent(t, res)

	// Adopt-in-place: the pre-existing iteration-log survives byte-for-byte and
	// its canonical path is exactly where the score stage wrote it.
	if res.RecordPath != run.Score.RecordPath {
		t.Errorf("Result.RecordPath = %q, want the in-place %q", res.RecordPath, run.Score.RecordPath)
	}
	if string(mustFileExist(t, res.RecordPath)) != string(origRecord) {
		t.Errorf("adopted iter-1.yaml changed — should survive byte-for-byte")
	}
	if string(mustFileExist(t, res.ScorePath)) != string(origScore) {
		t.Errorf("adopted iter-1.score.yaml changed — should survive byte-for-byte")
	}
}

// TestWriteEvalRun_AdoptMissingArtifact covers the adopt validation path: the
// canonical RecordPath is claimed but the file is absent (a broken pipeline).
func TestWriteEvalRun_AdoptMissingArtifact(t *testing.T) {
	root := t.TempDir()
	run := buildTestRun(t, root)
	// Delete the in-place record so adoption must fail its existence check.
	if err := os.Remove(run.Score.RecordPath); err != nil {
		t.Fatal(err)
	}
	_, err := store.WriteEvalRun(run, root)
	if err == nil || !strings.Contains(err.Error(), "iter record") {
		t.Fatalf("WriteEvalRun() = %v, want an adopt-in-place iter-record error", err)
	}
}

// ---- (c) copy case: RecordPath outside canonical → copied in ----------------

func TestWriteEvalRun_CopiesExternalIterationLog(t *testing.T) {
	scoreRoot := t.TempDir()
	run := buildTestRun(t, scoreRoot)
	root := t.TempDir() // distinct canonical root

	// Pre-create the run dir so this exercises "dir exists, iteration-log copied".
	if err := os.MkdirAll(store.RunDir(root, fixedRunID), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := store.WriteEvalRun(run, root)
	if err != nil {
		t.Fatalf("WriteEvalRun() = %v, want nil", err)
	}
	// The canonical record path is inside the store root, NOT the scratch source.
	if res.RecordPath == run.Score.RecordPath {
		t.Errorf("Result.RecordPath equals the scratch source — expected a copy target")
	}
	if !strings.HasPrefix(res.RecordPath, root) {
		t.Errorf("Result.RecordPath = %q, want under canonical root %q", res.RecordPath, root)
	}
	if string(mustFileExist(t, res.RecordPath)) != string(mustFileExist(t, run.Score.RecordPath)) {
		t.Errorf("copied iter-1.yaml diverges from the scratch original")
	}
	assertAllFourPresent(t, res)
}

// TestWriteEvalRun_CopyExternalMissingSource covers the copy read-error path.
func TestWriteEvalRun_CopyExternalMissingSource(t *testing.T) {
	scoreRoot := t.TempDir()
	run := buildTestRun(t, scoreRoot)
	run.Score.RecordPath = filepath.Join(t.TempDir(), "does-not-exist.yaml")

	_, err := store.WriteEvalRun(run, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "iter record") {
		t.Fatalf("WriteEvalRun() = %v, want an iter-record copy error", err)
	}
}

// TestWriteEvalRun_CopyScoreMissingSource covers the score-sidecar copy error
// path (record copies fine; the score source is missing).
func TestWriteEvalRun_CopyScoreMissingSource(t *testing.T) {
	scoreRoot := t.TempDir()
	run := buildTestRun(t, scoreRoot)
	run.Score.ScorePath = filepath.Join(t.TempDir(), "no-score.yaml")

	_, err := store.WriteEvalRun(run, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "iter score") {
		t.Fatalf("WriteEvalRun() = %v, want an iter-score copy error", err)
	}
}

// ---- (d) per-file atomicity -------------------------------------------------

// TestWriteEvalRun_EvalRunWriteIsAtomic pins that a failed eval-run.yaml write
// leaves NO partial eval-run.yaml (the atomic write never renames a torn temp
// into place) and does not corrupt the already-written taskspec.yaml.
func TestWriteEvalRun_EvalRunWriteIsAtomic(t *testing.T) {
	scoreRoot := t.TempDir()
	run := buildTestRun(t, scoreRoot)
	root := t.TempDir()

	restore := store.SetWriteFileAtomic(failWriteOn("eval-run.yaml"))
	defer restore()

	_, err := store.WriteEvalRun(run, root)
	if err == nil || !strings.Contains(err.Error(), "eval-run") {
		t.Fatalf("WriteEvalRun() = %v, want an eval-run write error", err)
	}

	runDir := store.RunDir(root, fixedRunID)
	// No partial eval-run.yaml, and no leftover atomic temp file in the run dir.
	mustNotExist(t, filepath.Join(runDir, "eval-run.yaml"))
	entries, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatalf("read run dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".fsops-") {
			t.Errorf("leftover atomic temp file %q after failed write", e.Name())
		}
	}
	// taskspec.yaml (written before the failure) is intact.
	if _, err := eval.ParseTaskSpec(mustFileExist(t, filepath.Join(runDir, "taskspec.yaml"))); err != nil {
		t.Errorf("taskspec.yaml damaged by unrelated eval-run failure: %v", err)
	}
}

// ---- agent reproducibility block (R9/R10) -----------------------------------

func TestWriteEvalRun_AgentReproducibilityFields(t *testing.T) {
	scoreRoot := t.TempDir()
	run := buildTestRun(t, scoreRoot)
	root := t.TempDir()

	res, err := store.WriteEvalRun(run, root)
	if err != nil {
		t.Fatalf("WriteEvalRun() = %v, want nil", err)
	}

	var p store.PersistedEvalRun
	if err := yaml.Unmarshal(mustFileExist(t, res.EvalRunPath), &p); err != nil {
		t.Fatalf("unmarshal eval-run.yaml: %v", err)
	}

	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"agent.harness", p.Agent.Harness, fixedHarness},
		{"agent.model", p.Agent.Model, fixedModel},
		{"agent.session_id", p.Agent.SessionID, fixedSession},
		{"agent.retries", p.Agent.Retries, 0},
		{"agent.exit_code", p.Agent.ExitCode, agentExitCode},
		{"agent.duration", p.Agent.Duration, (2 * time.Second).String()},
		{"agent.prompt_digest", p.Agent.PromptDigest, wantDigest(fixedPrompt)},
		{"agent.output_digest", p.Agent.OutputDigest, wantDigest(agentStdout)},
		{"base_commit", p.BaseCommit, fixedBaseSHA},
		{"language", p.Language, "go"},
		{"difficulty", p.Difficulty, "medium"},
		{"verify.passed", p.Verify.Passed, true},
		{"verify.phase", p.Verify.Phase, "test"},
		{"score.scored", p.Score.Scored, true},
		{"score.rubric_version", p.Score.RubricVersion, scoring.RubricVersion},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}

	raw := string(mustFileExist(t, res.EvalRunPath))
	for _, frag := range []string{"prompt_digest:", "output_digest:", "session_id:", "harness:", "model:"} {
		if !strings.Contains(raw, frag) {
			t.Errorf("eval-run.yaml missing %q key", frag)
		}
	}
}

// TestWriteEvalRun_NilVerify confirms a nil Verify writes a zero-valued verify
// block rather than erroring.
func TestWriteEvalRun_NilVerify(t *testing.T) {
	scoreRoot := t.TempDir()
	run := buildTestRun(t, scoreRoot)
	run.Verify = nil

	res, err := store.WriteEvalRun(run, t.TempDir())
	if err != nil {
		t.Fatalf("WriteEvalRun(nil verify) = %v, want nil", err)
	}
	var p store.PersistedEvalRun
	if err := yaml.Unmarshal(mustFileExist(t, res.EvalRunPath), &p); err != nil {
		t.Fatalf("unmarshal eval-run.yaml: %v", err)
	}
	if p.Verify.Passed || p.Verify.Phase != "" {
		t.Errorf("verify = %+v, want zero value for nil Verify", p.Verify)
	}
}

// ---- validation errors -------------------------------------------------------

func TestWriteEvalRun_Validation(t *testing.T) {
	scoreRoot := t.TempDir()
	cases := []struct {
		name    string
		mutate  func(*harness.EvalRun, *string)
		wantErr error
	}{
		{"empty run id", func(r *harness.EvalRun, _ *string) { r.RunID = "   " }, store.ErrEmptyRunID},
		{"nil spec", func(r *harness.EvalRun, _ *string) { r.Spec = nil }, store.ErrNilSpec},
		{"empty root", func(_ *harness.EvalRun, rt *string) { *rt = "" }, store.ErrEmptyRoot},
		{"empty record path", func(r *harness.EvalRun, _ *string) { r.Score.RecordPath = "  " }, store.ErrEmptyRecordPath},
		{"empty score path", func(r *harness.EvalRun, _ *string) { r.Score.ScorePath = "  " }, store.ErrEmptyScorePath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := buildTestRun(t, scoreRoot)
			rt := t.TempDir()
			tc.mutate(&run, &rt)
			if _, err := store.WriteEvalRun(run, rt); !errors.Is(err, tc.wantErr) {
				t.Errorf("WriteEvalRun() = %v, want errors.Is(_, %v)", err, tc.wantErr)
			}
		})
	}
}

// ---- (e) run_id path-safety retained ----------------------------------------

func TestWriteEvalRun_UnsafeRunID(t *testing.T) {
	malicious := []string{"../../etc", "a/b", `a\b`, "..", "foo/../../bar", "."}
	for _, id := range malicious {
		t.Run(id, func(t *testing.T) {
			scoreRoot := t.TempDir()
			run := buildTestRun(t, scoreRoot)
			run.RunID = id

			targetRoot := t.TempDir()
			_, err := store.WriteEvalRun(run, targetRoot)
			if !errors.Is(err, store.ErrUnsafeRunID) {
				t.Fatalf("WriteEvalRun(%q) = %v, want ErrUnsafeRunID", id, err)
			}
			// Nothing must have been created under the target root at all.
			if _, statErr := os.Stat(filepath.Join(targetRoot, ".agents")); !os.IsNotExist(statErr) {
				t.Errorf("unsafe run id %q created files under target root (stat err = %v)", id, statErr)
			}
		})
	}
}

// ---- seam-driven mkdir / write failure paths --------------------------------

// failWriteOn returns a write seam that fails on a path suffix and otherwise
// delegates to the real fsops.WriteFileAtomic.
func failWriteOn(suffix string) func(string, []byte) error {
	return func(path string, data []byte) error {
		if strings.HasSuffix(path, suffix) {
			return errBoom
		}
		return fsops.WriteFileAtomic(path, data)
	}
}

func TestWriteEvalRun_EnsureRunDirFails(t *testing.T) {
	scoreRoot := t.TempDir()
	run := buildTestRun(t, scoreRoot)

	restore := store.SetMkdirAll(func(string, os.FileMode) error { return errBoom })
	defer restore()

	_, err := store.WriteEvalRun(run, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "ensure run dir") {
		t.Fatalf("WriteEvalRun() = %v, want an ensure-run-dir error", err)
	}
}

func TestWriteEvalRun_TaskspecWriteFails(t *testing.T) {
	scoreRoot := t.TempDir()
	run := buildTestRun(t, scoreRoot)

	restore := store.SetWriteFileAtomic(failWriteOn("taskspec.yaml"))
	defer restore()

	_, err := store.WriteEvalRun(run, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "taskspec") {
		t.Fatalf("WriteEvalRun() = %v, want a taskspec write error", err)
	}
}

// TestWriteEvalRun_AdoptStatSeamError covers the adopt existence-check error
// branch via the stat seam (a stat failure that is not "not-exist").
func TestWriteEvalRun_AdoptStatSeamError(t *testing.T) {
	root := t.TempDir()
	run := buildTestRun(t, root) // adopt wiring: RecordPath inside canonical

	restore := store.SetStatPath(func(string) (os.FileInfo, error) { return nil, errBoom })
	defer restore()

	_, err := store.WriteEvalRun(run, root)
	if err == nil || !strings.Contains(err.Error(), "adopt in place") {
		t.Fatalf("WriteEvalRun() = %v, want an adopt-in-place stat error", err)
	}
}

// ---- RunDir helper ----------------------------------------------------------

func TestRunDir(t *testing.T) {
	got := store.RunDir("/repo", "eval-run-01")
	want := filepath.Join("/repo", ".agents", "eval", "runs", "eval-run-01")
	if got != want {
		t.Errorf("RunDir() = %q, want %q", got, want)
	}
}
