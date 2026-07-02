package scoringbridge

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/review/labels"
	"github.com/AGOrcha/dot-agents/internal/scoring"
	"go.yaml.in/yaml/v3"
)

// testSpec returns a minimal valid v1 TaskSpec fixture.
func testSpec() *eval.TaskSpec {
	return &eval.TaskSpec{
		TaskSpecVersion:   eval.CurrentTaskSpecVersion,
		TaskID:            "kg-go-impl-001",
		Language:          eval.LanguageGo,
		Difficulty:        eval.DifficultyMedium,
		DifficultySignals: map[string]int{"edge_count": 12},
		GeneratedFrom: eval.GeneratedFrom{
			Kind:       eval.KindKGTemplate,
			TemplateID: "impl-pure-fn",
		},
		Prompt: "Implement the function Bar.",
		Verification: eval.Verification{
			BuildCmd:       []string{"go", "build", "./..."},
			TestCmd:        []string{"go", "test", "./..."},
			TimeoutSeconds: 120,
		},
	}
}

// testRun returns a fully-populated passing EvalRun rooted in a temp dir.
func testRun(t *testing.T) EvalRun {
	t.Helper()
	return EvalRun{
		RunID:      "kg-go-impl-001-x7",
		RunDir:     filepath.Join(t.TempDir(), "kg-go-impl-001-x7"),
		BaseCommit: "0123456789abcdef0123456789abcdef01234567",
		Spec:       testSpec(),
		Agent: AgentTelemetry{
			SessionID: "3e0e9c2a-9f6e-4a52-8c7a-000000000001",
			Harness:   "claude-code",
			Model:     "claude-sonnet-4-6",
			Retries:   0,
			Tokens: &scoring.TokenUsage{
				InputTokens:         1200,
				OutputTokens:        340,
				CacheReadTokens:     800,
				CacheCreationTokens: 200,
				CacheHitRate:        0.8,
			},
		},
		Verify: VerifyResult{
			BuildRan: true, BuildPassed: true,
			TestRan: true, TestPassed: true,
		},
		FinishedAt: time.Date(2026, 7, 2, 10, 30, 0, 0, time.UTC),
	}
}

// signalRow finds one signal's breakdown row in a Score.
func signalRow(t *testing.T, s scoring.Score, id scoring.SignalID) scoring.SignalContribution {
	t.Helper()
	for _, row := range s.Breakdown {
		if row.Signal == id {
			return row
		}
	}
	t.Fatalf("signal %q not in breakdown", id)
	return scoring.SignalContribution{}
}

// --- happy path -------------------------------------------------------------

func TestScoreRunHappyPath(t *testing.T) {
	run := testRun(t)
	res, err := ScoreRun(run)
	if err != nil {
		t.Fatalf("ScoreRun() = %v, want nil", err)
	}

	if res.IterLogDir != IterationLogDir(run.RunDir) {
		t.Errorf("IterLogDir = %q, want %q", res.IterLogDir, IterationLogDir(run.RunDir))
	}
	for _, p := range []string{res.RecordPath, res.ScorePath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected artifact %s: %v", p, err)
		}
	}

	if !res.Score.Scored {
		t.Fatalf("Score.Scored = false, want a scored run: %+v", res.Score)
	}
	if res.Score.RubricVersion != scoring.RubricVersion {
		t.Errorf("RubricVersion = %q, want the production rubric %q (D4.4: no eval rubric fork)",
			res.Score.RubricVersion, scoring.RubricVersion)
	}
	if res.Score.Iteration != evalIteration {
		t.Errorf("Score.Iteration = %d, want %d (OQ2 1-shot)", res.Score.Iteration, evalIteration)
	}
	if res.Score.Value <= 0 || res.Score.Value > 1 {
		t.Errorf("Score.Value = %v, want in (0, 1]", res.Score.Value)
	}
}

func TestScoreRunSignalBreakdown(t *testing.T) {
	res, err := ScoreRun(testRun(t))
	if err != nil {
		t.Fatalf("ScoreRun() = %v, want nil", err)
	}

	present := map[scoring.SignalID]float64{
		scoring.SignalVerifier:           1.0, // build + test both passed
		scoring.SignalTests:              1.0, // tests_total_pass true
		scoring.SignalCorrectionPressure: 1.0, // zero retries, zero corrections
		scoring.SignalTokenEfficiency:    0.8, // native cache_hit_rate
	}
	for id, want := range present {
		row := signalRow(t, res.Score, id)
		if !row.Present || row.SubScore != want {
			t.Errorf("%s = (present=%t, sub=%v), want (true, %v)", id, row.Present, row.SubScore, want)
		}
	}

	absent := []scoring.SignalID{
		scoring.SignalLanded, // sandbox commits never land on trunk
		scoring.SignalScope,  // eval tasks declare no write_scope
		scoring.SignalHookOutcomes,
		scoring.SignalHumanLabel, // unlabeled run — absent-safe under rubric 3.0.0
	}
	for _, id := range absent {
		if row := signalRow(t, res.Score, id); row.Present {
			t.Errorf("%s present = true (sub=%v), want absent for an eval run", id, row.SubScore)
		}
	}
}

// TestScoreRunRoundTripsThroughProductionLoader pins the R5 contract: the
// record the bridge scored is byte-equivalent to what the production iter-log
// loader (the one `da score iteration` uses) reads back from the eval space.
func TestScoreRunRoundTripsThroughProductionLoader(t *testing.T) {
	res, err := ScoreRun(testRun(t))
	if err != nil {
		t.Fatalf("ScoreRun() = %v, want nil", err)
	}

	records, err := scoring.LoadIterationLog(res.IterLogDir)
	if err != nil {
		t.Fatalf("LoadIterationLog() = %v, want nil", err)
	}
	if len(records) != 1 {
		t.Fatalf("LoadIterationLog() returned %d records, want exactly 1 (OQ2 1-shot)", len(records))
	}
	if !reflect.DeepEqual(records[0], res.Record) {
		t.Errorf("re-loaded record diverges from scored record:\n got %+v\nwant %+v", records[0], res.Record)
	}
}

// TestScoreRunScoreSidecarIsPersistedScore asserts the sidecar has the
// durable PersistedScore shape R1's CLI and the R2 dashboard consume.
func TestScoreRunScoreSidecarIsPersistedScore(t *testing.T) {
	res, err := ScoreRun(testRun(t))
	if err != nil {
		t.Fatalf("ScoreRun() = %v, want nil", err)
	}

	data, err := os.ReadFile(res.ScorePath)
	if err != nil {
		t.Fatalf("read score sidecar: %v", err)
	}
	var ps scoring.PersistedScore
	if err := yaml.Unmarshal(data, &ps); err != nil {
		t.Fatalf("parse score sidecar: %v", err)
	}
	if ps.Iteration != evalIteration || !ps.Scored || ps.RubricVersion != scoring.RubricVersion {
		t.Errorf("sidecar = {iter:%d scored:%t rubric:%q}, want {iter:%d scored:true rubric:%q}",
			ps.Iteration, ps.Scored, ps.RubricVersion, evalIteration, scoring.RubricVersion)
	}
	if len(ps.Breakdown) == 0 {
		t.Error("sidecar breakdown is empty, want one row per rubric signal")
	}
}

// --- failure encoding (spec done criterion 8) -------------------------------

func TestScoreRunFailedVerificationStillEmitsSidecar(t *testing.T) {
	run := testRun(t)
	run.Verify = VerifyResult{BuildRan: true, BuildPassed: false, TestRan: true, TestPassed: false}

	res, err := ScoreRun(run)
	if err != nil {
		t.Fatalf("ScoreRun() = %v, want nil (a failed run is scoreable, not an error)", err)
	}
	if _, err := os.Stat(res.ScorePath); err != nil {
		t.Fatalf("score sidecar missing for failed run: %v (done criterion 8)", err)
	}
	if !res.Score.Scored {
		t.Fatal("Score.Scored = false, want failure encoded as a signal, not an unscored run")
	}
	for _, id := range []scoring.SignalID{scoring.SignalVerifier, scoring.SignalTests} {
		row := signalRow(t, res.Score, id)
		if !row.Present || row.SubScore != 0.0 {
			t.Errorf("%s = (present=%t, sub=%v), want (true, 0.0) for a failed run", id, row.Present, row.SubScore)
		}
	}
}

func TestScoreRunVerificationNeverRan(t *testing.T) {
	run := testRun(t)
	run.Verify = VerifyResult{} // harness never reached verification

	res, err := ScoreRun(run)
	if err != nil {
		t.Fatalf("ScoreRun() = %v, want nil", err)
	}
	// "never checked" is unknown, excluded from the verifier mean — not a fail.
	for _, id := range []scoring.SignalID{scoring.SignalVerifier, scoring.SignalTests} {
		if row := signalRow(t, res.Score, id); row.Present {
			t.Errorf("%s present = true, want absent when verification never ran", id)
		}
	}
	if !res.Score.Scored {
		t.Error("Score.Scored = false, want scored (correction pressure is always present)")
	}
}

// --- shared-rubric parity: sidecar extractors read the eval space -----------

func TestScoreRunScoresHumanLabelSidecar(t *testing.T) {
	run := testRun(t)
	iterDir := IterationLogDir(run.RunDir)
	if err := os.MkdirAll(iterDir, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := labels.Add(iterDir, evalIteration, labels.AddInput{
		Actor: "reviewer@example.com",
		Role:  labels.RoleReviewer,
		Structured: labels.Structured{
			Correctness:    labels.CorrectnessMax,
			ScopeJudgement: labels.ScopeOnTarget,
			Hallucination:  labels.HallucinationNone,
		},
		Now: time.Date(2026, 7, 2, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("labels.Add() = %v, want nil", err)
	}

	res, err := ScoreRun(run)
	if err != nil {
		t.Fatalf("ScoreRun() = %v, want nil", err)
	}
	row := signalRow(t, res.Score, scoring.SignalHumanLabel)
	if !row.Present || row.SubScore != 1.0 {
		t.Errorf("human_label = (present=%t, sub=%v), want (true, 1.0): eval runs are labelable under the same rubric",
			row.Present, row.SubScore)
	}
}

func TestScoreRunScoresHookOutcomeSidecar(t *testing.T) {
	run := testRun(t)
	iterDir := IterationLogDir(run.RunDir)
	if err := os.MkdirAll(iterDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sidecar := []byte("schema_version: 1\nrecords:\n" +
		"  - intervention_class: prevent_before_action\n" +
		"    result: allow\n    rule_id: r1\n    correlation_id: c1\n")
	if err := os.WriteFile(filepath.Join(iterDir, "iter-1.hook-outcomes.yaml"), sidecar, 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := ScoreRun(run)
	if err != nil {
		t.Fatalf("ScoreRun() = %v, want nil", err)
	}
	row := signalRow(t, res.Score, scoring.SignalHookOutcomes)
	if !row.Present || row.SubScore != 1.0 {
		t.Errorf("hook_outcomes = (present=%t, sub=%v), want (true, 1.0)", row.Present, row.SubScore)
	}
}

// --- validation --------------------------------------------------------------

func TestScoreRunValidation(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*EvalRun)
		wantErr error
	}{
		{"empty run id", func(r *EvalRun) { r.RunID = "  " }, ErrEmptyRunID},
		{"empty run dir", func(r *EvalRun) { r.RunDir = "" }, ErrEmptyRunDir},
		{"nil spec", func(r *EvalRun) { r.Spec = nil }, ErrNilTaskSpec},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := testRun(t)
			tc.mutate(&run)
			if _, err := ScoreRun(run); !errors.Is(err, tc.wantErr) {
				t.Errorf("ScoreRun() = %v, want errors.Is(err, %v)", err, tc.wantErr)
			}
		})
	}
}

func TestScoreRunInvalidSpec(t *testing.T) {
	run := testRun(t)
	run.Spec.Prompt = "" // fails TaskSpec.Validate
	_, err := ScoreRun(run)
	if err == nil || !strings.Contains(err.Error(), "invalid task spec") {
		t.Errorf("ScoreRun() = %v, want the invalid-task-spec wrap", err)
	}
}

// --- filesystem failure branches ---------------------------------------------

func TestScoreRunIterLogDirCreateFailure(t *testing.T) {
	run := testRun(t)
	if err := os.MkdirAll(run.RunDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A regular file where the iteration-log dir must go makes MkdirAll fail.
	if err := os.WriteFile(IterationLogDir(run.RunDir), []byte("in the way"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ScoreRun(run)
	if err == nil || !strings.Contains(err.Error(), "create eval iter-log dir") {
		t.Errorf("ScoreRun() = %v, want the iter-log-dir create wrap", err)
	}
}

func TestScoreRunRecordWriteFailure(t *testing.T) {
	run := testRun(t)
	// A directory squatting on iter-1.yaml makes the atomic rename fail.
	if err := os.MkdirAll(filepath.Join(IterationLogDir(run.RunDir), "iter-1.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := ScoreRun(run)
	if err == nil || !strings.Contains(err.Error(), "write iteration record") {
		t.Errorf("ScoreRun() = %v, want the record write wrap", err)
	}
}

func TestScoreRunScoreWriteFailure(t *testing.T) {
	run := testRun(t)
	// The record write succeeds; the score sidecar rename hits a directory.
	if err := os.MkdirAll(filepath.Join(IterationLogDir(run.RunDir), "iter-1.score.yaml"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := ScoreRun(run)
	if err == nil || !strings.Contains(err.Error(), "persist score") {
		t.Errorf("ScoreRun() = %v, want the persist-score wrap", err)
	}
}

// --- small helpers ------------------------------------------------------------

func TestIterationLogDir(t *testing.T) {
	got := IterationLogDir(filepath.Join(".agents", "eval", "runs", "r1"))
	want := filepath.Join(".agents", "eval", "runs", "r1", "iteration-log")
	if got != want {
		t.Errorf("IterationLogDir() = %q, want %q", got, want)
	}
}

func TestScoreRunZeroFinishedAtDefaultsToNow(t *testing.T) {
	run := testRun(t)
	run.FinishedAt = time.Time{}
	before := time.Now().UTC().Add(-time.Minute)

	res, err := ScoreRun(run)
	if err != nil {
		t.Fatalf("ScoreRun() = %v, want nil", err)
	}
	at, err := time.Parse(checkpointLayout, res.Record.CheckpointAt)
	if err != nil {
		t.Fatalf("checkpoint_at %q does not parse: %v", res.Record.CheckpointAt, err)
	}
	if at.Before(before) || at.After(time.Now().UTC().Add(time.Minute)) {
		t.Errorf("checkpoint_at = %v, want ~now for a zero FinishedAt", at)
	}
}

func TestScoreRunNoTokenTelemetry(t *testing.T) {
	run := testRun(t)
	run.Agent.Tokens = nil

	res, err := ScoreRun(run)
	if err != nil {
		t.Fatalf("ScoreRun() = %v, want nil", err)
	}
	row := signalRow(t, res.Score, scoring.SignalTokenEfficiency)
	if row.Present {
		t.Errorf("token_efficiency present = true, want absent without telemetry: %+v", row)
	}
	if res.Record.SessionTokens != nil {
		t.Errorf("Record.SessionTokens = %+v, want nil", res.Record.SessionTokens)
	}
}
