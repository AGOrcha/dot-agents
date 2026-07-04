package store_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/harness"
	"github.com/AGOrcha/dot-agents/internal/eval/runner"
	"github.com/AGOrcha/dot-agents/internal/eval/scoringbridge"
	"github.com/AGOrcha/dot-agents/internal/eval/store"
	goverifier "github.com/AGOrcha/dot-agents/internal/eval/verifier/golang"
	"github.com/AGOrcha/dot-agents/internal/scoring"
	"go.yaml.in/yaml/v3"
)

// ---- fixtures ---------------------------------------------------------------

// fixedRunID is the run ID used across test helpers.
const fixedRunID = "eval-store-test-01"

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
		Prompt: "Implement function Foo.",
		Verification: eval.Verification{
			BuildCmd:       []string{"go", "build", "./..."},
			TestCmd:        []string{"go", "test", "./..."},
			TimeoutSeconds: 60,
		},
	}
}

// buildTestRun creates a harness.EvalRun rooted at <root>/.agents/eval/runs/<fixedRunID>
// with scoringbridge-written iteration-log files already present. It is the
// standard fully-populated fixture for happy-path tests.
func buildTestRun(t *testing.T, root string) harness.EvalRun {
	t.Helper()
	runDir := store.RunDir(root, fixedRunID)

	sbRun := scoringbridge.EvalRun{
		RunID:      fixedRunID,
		RunDir:     runDir,
		BaseCommit: "0000000000000000000000000000000000abcdef",
		Spec:       testSpec(),
		Agent: scoringbridge.AgentTelemetry{
			SessionID: "sess-store-001",
			Harness:   "test-harness",
			Model:     "test-model",
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
		BaseCommit: "0000000000000000000000000000000000abcdef",
		Run: runner.Result{
			ExitCode: 0,
			Duration: 2 * time.Second,
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

// ---- happy-path: full layout ------------------------------------------------

// TestWriteEvalRun_FullLayout is the primary layout-pinning test. It verifies
// that after WriteEvalRun:
//   - all four sidecar files exist at their canonical paths
//   - taskspec.yaml round-trips through eval.ParseTaskSpec
//   - eval-run.yaml carries the correct run_id and verify block
//   - iter-1.yaml is readable by scoring.LoadIterationLog (R2 stable contract)
//   - iter-1.score.yaml parses as a scored PersistedScore
func TestWriteEvalRun_FullLayout(t *testing.T) {
	root := t.TempDir()
	run := buildTestRun(t, root)

	res, err := store.WriteEvalRun(run, root)
	if err != nil {
		t.Fatalf("WriteEvalRun() = %v, want nil", err)
	}

	// --- path assertions ------------------------------------------------------
	wantRunDir := store.RunDir(root, fixedRunID)
	if res.RunDir != wantRunDir {
		t.Errorf("Result.RunDir = %q, want %q", res.RunDir, wantRunDir)
	}
	wantIterDir := filepath.Join(wantRunDir, "iteration-log")
	if res.IterLogDir != wantIterDir {
		t.Errorf("Result.IterLogDir = %q, want %q", res.IterLogDir, wantIterDir)
	}

	for _, p := range []string{res.TaskspecPath, res.EvalRunPath, res.RecordPath, res.ScorePath} {
		mustFileExist(t, p)
	}

	// --- taskspec.yaml --------------------------------------------------------
	tsData := mustFileExist(t, res.TaskspecPath)
	spec, err := eval.ParseTaskSpec(tsData)
	if err != nil {
		t.Fatalf("ParseTaskSpec(taskspec.yaml) = %v, want nil", err)
	}
	if spec.TaskID != testSpec().TaskID {
		t.Errorf("taskspec task_id = %q, want %q", spec.TaskID, testSpec().TaskID)
	}
	if spec.Language != eval.LanguageGo {
		t.Errorf("taskspec language = %q, want go", spec.Language)
	}

	// --- eval-run.yaml --------------------------------------------------------
	erData := mustFileExist(t, res.EvalRunPath)
	var persisted store.PersistedEvalRun
	if err := yaml.Unmarshal(erData, &persisted); err != nil {
		t.Fatalf("unmarshal eval-run.yaml: %v", err)
	}
	if persisted.RunID != fixedRunID {
		t.Errorf("eval-run run_id = %q, want %q", persisted.RunID, fixedRunID)
	}
	if persisted.Language != "go" {
		t.Errorf("eval-run language = %q, want go", persisted.Language)
	}
	if persisted.Difficulty != "medium" {
		t.Errorf("eval-run difficulty = %q, want medium", persisted.Difficulty)
	}
	if !persisted.Verify.Passed {
		t.Errorf("eval-run verify.passed = false, want true")
	}
	if persisted.Verify.Phase != "test" {
		t.Errorf("eval-run verify.phase = %q, want test", persisted.Verify.Phase)
	}
	if !persisted.Score.Scored {
		t.Errorf("eval-run score.scored = false, want true")
	}

	// --- iter-1.yaml (R2 stable contract: production loader round-trip) ------
	records, err := scoring.LoadIterationLog(res.IterLogDir)
	if err != nil {
		t.Fatalf("LoadIterationLog() = %v, want nil", err)
	}
	if len(records) != 1 {
		t.Fatalf("LoadIterationLog() returned %d records, want 1", len(records))
	}
	if records[0].TaskID != testSpec().TaskID {
		t.Errorf("iter-1 task_id = %q, want %q", records[0].TaskID, testSpec().TaskID)
	}
	if records[0].Wave != fixedRunID {
		t.Errorf("iter-1 wave = %q, want %q (run ID)", records[0].Wave, fixedRunID)
	}

	// --- iter-1.score.yaml ----------------------------------------------------
	scoreData := mustFileExist(t, res.ScorePath)
	var ps scoring.PersistedScore
	if err := yaml.Unmarshal(scoreData, &ps); err != nil {
		t.Fatalf("unmarshal iter-1.score.yaml: %v", err)
	}
	if ps.Iteration != 1 {
		t.Errorf("score iteration = %d, want 1", ps.Iteration)
	}
	if !ps.Scored {
		t.Errorf("score.scored = false, want true")
	}
	if ps.RubricVersion != scoring.RubricVersion {
		t.Errorf("score rubric_version = %q, want %q", ps.RubricVersion, scoring.RubricVersion)
	}
}

// TestWriteEvalRun_NilVerify confirms a nil Verify in harness.EvalRun writes a
// zero-valued verify block in eval-run.yaml (not a marshal error).
func TestWriteEvalRun_NilVerify(t *testing.T) {
	root := t.TempDir()
	run := buildTestRun(t, root)
	run.Verify = nil

	res, err := store.WriteEvalRun(run, root)
	if err != nil {
		t.Fatalf("WriteEvalRun(nil verify) = %v, want nil", err)
	}
	data := mustFileExist(t, res.EvalRunPath)
	var persisted store.PersistedEvalRun
	if err := yaml.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("unmarshal eval-run.yaml: %v", err)
	}
	if persisted.Verify.Passed {
		t.Errorf("verify.passed = true, want false (zero value for nil Verify)")
	}
	if persisted.Verify.Phase != "" {
		t.Errorf("verify.phase = %q, want empty (zero value)", persisted.Verify.Phase)
	}
}

// TestWriteEvalRun_Idempotent confirms that calling WriteEvalRun twice on the
// same (root, runID) pair succeeds and yields identical file content.
func TestWriteEvalRun_Idempotent(t *testing.T) {
	root := t.TempDir()
	run := buildTestRun(t, root)

	res1, err := store.WriteEvalRun(run, root)
	if err != nil {
		t.Fatalf("WriteEvalRun (first) = %v, want nil", err)
	}
	res2, err := store.WriteEvalRun(run, root)
	if err != nil {
		t.Fatalf("WriteEvalRun (second) = %v, want nil", err)
	}

	for _, pair := range [][2]string{
		{res1.TaskspecPath, res2.TaskspecPath},
		{res1.EvalRunPath, res2.EvalRunPath},
		{res1.RecordPath, res2.RecordPath},
	} {
		d1 := mustFileExist(t, pair[0])
		d2 := mustFileExist(t, pair[1])
		if string(d1) != string(d2) {
			t.Errorf("idempotent: %s content changed between calls", filepath.Base(pair[0]))
		}
	}
}

// ---- validation errors -------------------------------------------------------

func TestWriteEvalRun_Validation(t *testing.T) {
	root := t.TempDir()

	cases := []struct {
		name    string
		mutate  func(*harness.EvalRun, *string)
		wantErr error
	}{
		{
			name:    "empty run id",
			mutate:  func(r *harness.EvalRun, _ *string) { r.RunID = "   " },
			wantErr: store.ErrEmptyRunID,
		},
		{
			name:    "nil spec",
			mutate:  func(r *harness.EvalRun, _ *string) { r.Spec = nil },
			wantErr: store.ErrNilSpec,
		},
		{
			name:    "empty root",
			mutate:  func(_ *harness.EvalRun, rt *string) { *rt = "" },
			wantErr: store.ErrEmptyRoot,
		},
		{
			name:    "empty record path",
			mutate:  func(r *harness.EvalRun, _ *string) { r.Score.RecordPath = "  " },
			wantErr: store.ErrEmptyRecordPath,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := buildTestRun(t, root)
			rt := root
			tc.mutate(&run, &rt)
			_, err := store.WriteEvalRun(run, rt)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("WriteEvalRun() = %v, want errors.Is(_, %v)", err, tc.wantErr)
			}
		})
	}
}

// ---- filesystem failure paths -----------------------------------------------

// TestWriteEvalRun_DirCreateFail verifies the "create sidecar dirs" error path:
// a regular file squatting at the iteration-log directory prevents MkdirAll.
// buildTestRun uses sbRoot so the canonical structure there does not interfere
// with the blocker planted at targetRoot.
func TestWriteEvalRun_DirCreateFail(t *testing.T) {
	sbRoot := t.TempDir()
	run := buildTestRun(t, sbRoot) // iter-log files written under sbRoot

	// Plant the blocker at a fresh targetRoot that has no pre-existing dirs.
	targetRoot := t.TempDir()
	targetRunDir := store.RunDir(targetRoot, fixedRunID)
	if err := os.MkdirAll(targetRunDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A regular file at iteration-log makes MkdirAll fail.
	if err := os.WriteFile(filepath.Join(targetRunDir, "iteration-log"), []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := store.WriteEvalRun(run, targetRoot)
	if err == nil {
		t.Fatal("WriteEvalRun() = nil, want dir-create error")
	}
}

// TestWriteEvalRun_TaskspecWriteFail verifies the "write taskspec" error path:
// a directory squatting at taskspec.yaml prevents the atomic rename.
func TestWriteEvalRun_TaskspecWriteFail(t *testing.T) {
	root := t.TempDir()
	run := buildTestRun(t, root)

	runDir := store.RunDir(root, fixedRunID)
	if err := os.MkdirAll(filepath.Join(runDir, "iteration-log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(runDir, "taskspec.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := store.WriteEvalRun(run, root)
	if err == nil {
		t.Fatal("WriteEvalRun() = nil, want taskspec write error")
	}
}

// TestWriteEvalRun_EvalRunWriteFail verifies the "write eval-run" error path:
// a directory squatting at eval-run.yaml prevents the atomic rename.
func TestWriteEvalRun_EvalRunWriteFail(t *testing.T) {
	root := t.TempDir()
	run := buildTestRun(t, root)

	runDir := store.RunDir(root, fixedRunID)
	if err := os.MkdirAll(filepath.Join(runDir, "iteration-log"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(runDir, "eval-run.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := store.WriteEvalRun(run, root)
	if err == nil {
		t.Fatal("WriteEvalRun() = nil, want eval-run write error")
	}
}

// TestWriteEvalRun_IterRecordReadFail verifies the "write iter record" error
// path when run.Score.RecordPath points to a non-existent file.
func TestWriteEvalRun_IterRecordReadFail(t *testing.T) {
	root := t.TempDir()
	run := buildTestRun(t, root)
	run.Score.RecordPath = filepath.Join(t.TempDir(), "does-not-exist.yaml")

	_, err := store.WriteEvalRun(run, root)
	if err == nil {
		t.Fatal("WriteEvalRun() = nil, want iter-record read error")
	}
}

// TestWriteEvalRun_ScoreWriteFail verifies the "persist score" error path:
// a directory at iter-1.score.yaml prevents the atomic rename.
// buildTestRun uses sbRoot so its iter-1.score.yaml file does not occupy the
// path we need to turn into a directory blocker at targetRoot.
func TestWriteEvalRun_ScoreWriteFail(t *testing.T) {
	sbRoot := t.TempDir()
	run := buildTestRun(t, sbRoot) // iter-log files written under sbRoot

	// At targetRoot, create the iter-log dir and block the score sidecar path.
	targetRoot := t.TempDir()
	targetIterDir := filepath.Join(store.RunDir(targetRoot, fixedRunID), "iteration-log")
	if err := os.MkdirAll(targetIterDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A directory at iter-1.score.yaml makes scoring.WriteIterationScore fail.
	if err := os.MkdirAll(filepath.Join(targetIterDir, "iter-1.score.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	// run.Score.RecordPath still points to the valid file under sbRoot, so
	// copyFileAtomic succeeds; only the score sidecar rename hits the blocker.

	_, err := store.WriteEvalRun(run, targetRoot)
	if err == nil {
		t.Fatal("WriteEvalRun() = nil, want score-persist error")
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
