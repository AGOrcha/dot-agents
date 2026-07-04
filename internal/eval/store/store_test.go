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

// buildTestRun creates a harness.EvalRun whose scoring artifacts are written by
// scoringbridge.ScoreRun into a private SCRATCH dir — the scoring stage's own
// working location, deliberately distinct from the store's canonical root. That
// mirrors the write-once contract: the store owns the canonical run dir and
// reads run.Score.RecordPath as an input from elsewhere, so every test targets a
// canonical root that does not already contain the run.
func buildTestRun(t *testing.T) harness.EvalRun {
	t.Helper()
	scratch := t.TempDir()
	runDir := store.RunDir(scratch, fixedRunID)

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

// assertNoCanonicalDir asserts the canonical run dir is absent and the runs
// root holds no leftover (staging) entries — the transactional guarantee.
func assertNoCanonicalDir(t *testing.T, root string) {
	t.Helper()
	finalDir := store.RunDir(root, fixedRunID)
	if _, err := os.Stat(finalDir); !os.IsNotExist(err) {
		t.Errorf("canonical run dir exists after failure: stat err = %v, want IsNotExist", err)
	}
	runsRoot := filepath.Join(root, ".agents", "eval", "runs")
	entries, err := os.ReadDir(runsRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatalf("read runs root: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("runs root not empty after failure, leftover entries: %v", names)
	}
}

// ---- happy-path: full layout ------------------------------------------------

// TestWriteEvalRun_FullLayout pins the four-file layout and round-trips
// iter-1.yaml through the production loader (the R2 stable contract).
func TestWriteEvalRun_FullLayout(t *testing.T) {
	root := t.TempDir()
	run := buildTestRun(t)

	res, err := store.WriteEvalRun(run, root)
	if err != nil {
		t.Fatalf("WriteEvalRun() = %v, want nil", err)
	}

	assertResultPaths(t, res, root)
	assertTaskspecFile(t, res)
	assertEvalRunFile(t, res)
	assertIterLogRoundTrip(t, res)
	assertScoreSidecar(t, res)
}

func assertResultPaths(t *testing.T, res store.Result, root string) {
	t.Helper()
	wantRunDir := store.RunDir(root, fixedRunID)
	if res.RunDir != wantRunDir {
		t.Errorf("Result.RunDir = %q, want %q", res.RunDir, wantRunDir)
	}
	if res.IterLogDir != filepath.Join(wantRunDir, "iteration-log") {
		t.Errorf("Result.IterLogDir = %q, want <run>/iteration-log", res.IterLogDir)
	}
	for _, p := range []string{res.TaskspecPath, res.EvalRunPath, res.RecordPath, res.ScorePath} {
		mustFileExist(t, p)
	}
}

func assertTaskspecFile(t *testing.T, res store.Result) {
	t.Helper()
	spec, err := eval.ParseTaskSpec(mustFileExist(t, res.TaskspecPath))
	if err != nil {
		t.Fatalf("ParseTaskSpec(taskspec.yaml) = %v, want nil", err)
	}
	if spec.TaskID != testSpec().TaskID {
		t.Errorf("taskspec task_id = %q, want %q", spec.TaskID, testSpec().TaskID)
	}
	if spec.Language != eval.LanguageGo {
		t.Errorf("taskspec language = %q, want go", spec.Language)
	}
}

func assertEvalRunFile(t *testing.T, res store.Result) {
	t.Helper()
	var p store.PersistedEvalRun
	if err := yaml.Unmarshal(mustFileExist(t, res.EvalRunPath), &p); err != nil {
		t.Fatalf("unmarshal eval-run.yaml: %v", err)
	}
	if p.RunID != fixedRunID {
		t.Errorf("eval-run run_id = %q, want %q", p.RunID, fixedRunID)
	}
	if p.Language != "go" || p.Difficulty != "medium" {
		t.Errorf("eval-run language/difficulty = %q/%q, want go/medium", p.Language, p.Difficulty)
	}
	if !p.Verify.Passed || p.Verify.Phase != "test" {
		t.Errorf("eval-run verify = %+v, want passed test", p.Verify)
	}
	if !p.Score.Scored {
		t.Errorf("eval-run score.scored = false, want true")
	}
}

func assertIterLogRoundTrip(t *testing.T, res store.Result) {
	t.Helper()
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
}

func assertScoreSidecar(t *testing.T, res store.Result) {
	t.Helper()
	var ps scoring.PersistedScore
	if err := yaml.Unmarshal(mustFileExist(t, res.ScorePath), &ps); err != nil {
		t.Fatalf("unmarshal iter-1.score.yaml: %v", err)
	}
	if ps.Iteration != 1 || !ps.Scored {
		t.Errorf("score = {iter:%d scored:%t}, want {1 true}", ps.Iteration, ps.Scored)
	}
	if ps.RubricVersion != scoring.RubricVersion {
		t.Errorf("score rubric_version = %q, want %q", ps.RubricVersion, scoring.RubricVersion)
	}
}

// TestWriteEvalRun_AgentReproducibilityFields pins spec R9 / R10: eval-run.yaml
// must carry the agent platform/harness, model, session id, the prompt-overlay
// identity (prompt digest), and the agent-output digest — not just exit code +
// duration.
func TestWriteEvalRun_AgentReproducibilityFields(t *testing.T) {
	root := t.TempDir()
	run := buildTestRun(t)

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
		{"score.rubric_version", p.Score.RubricVersion, scoring.RubricVersion},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.field, c.got, c.want)
		}
	}

	// The digests must be raw-string present in the file bytes so a downstream
	// text-diff tool (not just the typed struct) can read them.
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
	root := t.TempDir()
	run := buildTestRun(t)
	run.Verify = nil

	res, err := store.WriteEvalRun(run, root)
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

// TestWriteEvalRun_RefusesOverwrite pins the write-once contract and the fix for
// the erase hazard: a second write to a run id whose canonical dir already
// exists returns ErrRunExists, the persisted run is left byte-for-byte intact,
// and no staging dir is leaked. The commit never deletes the existing run, so a
// failure between "delete" and "rename" can never erase it — there is no delete.
func TestWriteEvalRun_RefusesOverwrite(t *testing.T) {
	root := t.TempDir()
	run := buildTestRun(t)

	res, err := store.WriteEvalRun(run, root)
	if err != nil {
		t.Fatalf("WriteEvalRun (first) = %v, want nil", err)
	}
	orig := mustFileExist(t, res.EvalRunPath)

	_, err = store.WriteEvalRun(run, root)
	if !errors.Is(err, store.ErrRunExists) {
		t.Fatalf("WriteEvalRun (second) = %v, want ErrRunExists", err)
	}

	// The persisted run must survive the refused overwrite unchanged.
	if got := mustFileExist(t, res.EvalRunPath); string(got) != string(orig) {
		t.Errorf("existing run content changed after refused overwrite")
	}
	// The runs root holds exactly the one committed run dir — no leaked staging.
	entries, err := os.ReadDir(filepath.Join(root, ".agents", "eval", "runs"))
	if err != nil {
		t.Fatalf("read runs root: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != fixedRunID {
		t.Errorf("runs root = %v, want exactly [%s]", entries, fixedRunID)
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
		{"empty run id", func(r *harness.EvalRun, _ *string) { r.RunID = "   " }, store.ErrEmptyRunID},
		{"nil spec", func(r *harness.EvalRun, _ *string) { r.Spec = nil }, store.ErrNilSpec},
		{"empty root", func(_ *harness.EvalRun, rt *string) { *rt = "" }, store.ErrEmptyRoot},
		{"empty record path", func(r *harness.EvalRun, _ *string) { r.Score.RecordPath = "  " }, store.ErrEmptyRecordPath},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := buildTestRun(t)
			rt := root
			tc.mutate(&run, &rt)
			if _, err := store.WriteEvalRun(run, rt); !errors.Is(err, tc.wantErr) {
				t.Errorf("WriteEvalRun() = %v, want errors.Is(_, %v)", err, tc.wantErr)
			}
		})
	}
}

// TestWriteEvalRun_UnsafeRunID pins the path-traversal guard: a run ID with a
// separator or ".." is rejected with ErrUnsafeRunID and NOTHING is written
// anywhere under the root.
func TestWriteEvalRun_UnsafeRunID(t *testing.T) {
	malicious := []string{
		"../../etc",
		"a/b",
		`a\b`,
		"..",
		"foo/../../bar",
		".",
	}
	for _, id := range malicious {
		t.Run(id, func(t *testing.T) {
			run := buildTestRun(t)
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

// ---- transactional failure paths --------------------------------------------

// TestWriteEvalRun_PartialFailureNoCanonicalDir is the core fix-3 guarantee: a
// mid-write step failure (here, an unreadable iteration record source) leaves NO
// partial run directory at the canonical path and no leftover staging dir.
func TestWriteEvalRun_PartialFailureNoCanonicalDir(t *testing.T) {
	run := buildTestRun(t)
	run.Score.RecordPath = filepath.Join(t.TempDir(), "does-not-exist.yaml")

	targetRoot := t.TempDir()
	_, err := store.WriteEvalRun(run, targetRoot)
	if err == nil {
		t.Fatal("WriteEvalRun() = nil, want mid-write error")
	}
	assertNoCanonicalDir(t, targetRoot)
}

// TestWriteEvalRun_RunsRootBlocked covers the "create runs root" failure: a
// regular file where the runs root must go.
func TestWriteEvalRun_RunsRootBlocked(t *testing.T) {
	run := buildTestRun(t)

	targetRoot := t.TempDir()
	evalDir := filepath.Join(targetRoot, ".agents", "eval")
	if err := os.MkdirAll(evalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(evalDir, "runs"), []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := store.WriteEvalRun(run, targetRoot)
	if err == nil || !strings.Contains(err.Error(), "create runs root") {
		t.Fatalf("WriteEvalRun() = %v, want create-runs-root error", err)
	}
}

// failMkdirOn returns a mkdir seam that fails when the FINAL path element
// contains match (inspecting only the base avoids matching the test-name that
// t.TempDir bakes into the temp path) and otherwise delegates to fsops.MkdirAll.
func failMkdirOn(match string) func(string, os.FileMode) error {
	return func(path string, perm os.FileMode) error {
		if strings.Contains(filepath.Base(path), match) {
			return errBoom
		}
		return fsops.MkdirAll(path, perm)
	}
}

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

func TestWriteEvalRun_SeamFailures(t *testing.T) {
	cases := []struct {
		name    string
		install func() (restore func())
		wantSub string
	}{
		{
			name:    "staging dir create",
			install: func() func() { return store.SetMkdirAll(failMkdirOn(".staging-")) },
			wantSub: "create staging dir",
		},
		{
			name:    "iteration-log dir create",
			install: func() func() { return store.SetMkdirAll(failMkdirOn("iteration-log")) },
			wantSub: "create iteration-log dir",
		},
		{
			name:    "taskspec write",
			install: func() func() { return store.SetWriteFileAtomic(failWriteOn("taskspec.yaml")) },
			wantSub: "taskspec",
		},
		{
			name:    "eval-run write",
			install: func() func() { return store.SetWriteFileAtomic(failWriteOn("eval-run.yaml")) },
			wantSub: "eval-run",
		},
		{
			name: "score persist",
			install: func() func() {
				return store.SetWriteIterationScore(func(string, scoring.Score) (string, error) { return "", errBoom })
			},
			wantSub: "persist score",
		},
		{
			name: "stat run dir",
			install: func() func() {
				return store.SetStatPath(func(string) (os.FileInfo, error) { return nil, errBoom })
			},
			wantSub: "stat run dir",
		},
		{
			name:    "commit rename",
			install: func() func() { return store.SetRenameDir(func(string, string) error { return errBoom }) },
			wantSub: "commit run dir",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := buildTestRun(t)
			targetRoot := t.TempDir()

			restore := tc.install()
			defer restore()

			_, err := store.WriteEvalRun(run, targetRoot)
			if err == nil || !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("WriteEvalRun() = %v, want error containing %q", err, tc.wantSub)
			}
			// Every failure path leaves NO partial directory at the canonical
			// path (the staging dir is removed on error).
			assertNoCanonicalDir(t, targetRoot)
		})
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
