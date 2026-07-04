package eval

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	evalcore "github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/harness"
	"github.com/AGOrcha/dot-agents/internal/eval/runner"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
	"github.com/AGOrcha/dot-agents/internal/eval/store"
	goverifier "github.com/AGOrcha/dot-agents/internal/eval/verifier/golang"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/spf13/cobra"
)

// ---- test-harness builders --------------------------------------------------

// buildTestHarness assembles a harness from a fixed-spec generator, the given
// sandbox + runner, and a scripted go verifier — no KG or go toolchain needed.
func buildTestHarness(t *testing.T, sb sandbox.Sandbox, run runner.Runner, ver *goverifier.VerifyResult) *harness.Harness {
	t.Helper()
	reg := evalcore.NewRegistry()
	if err := reg.Register(fixedGenerator{spec: validSpec()}); err != nil {
		t.Fatalf("register fixed generator: %v", err)
	}
	h, err := harness.New(harness.Config{
		Generators: reg,
		Sandbox:    sb,
		Runner:     run,
		Verifiers:  map[evalcore.Language]goverifier.Verifier{evalcore.LanguageGo: &fakeVerifier{res: ver}},
	})
	if err != nil {
		t.Fatalf("harness.New: %v", err)
	}
	return h
}

// writeSpecFile marshals spec to a temp YAML file and returns its path.
func writeSpecFile(t *testing.T, spec *evalcore.TaskSpec) string {
	t.Helper()
	data, err := yaml.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	path := filepath.Join(t.TempDir(), "task.yaml")
	writeFile(t, path, string(data))
	return path
}

// okRunner is a FakeRunner scripted to a clean, telemetry-bearing exit.
func okRunner() *runner.FakeRunner {
	return &runner.FakeRunner{Result: runner.Result{
		ExitCode:  0,
		Telemetry: runner.AgentTelemetry{Harness: "fake-harness", Model: "test-model"},
	}}
}

// ---- HARD TEST: da eval run --language go end-to-end ------------------------

// TestRunEvalCommandGoEndToEnd is the acceptance criterion: `da eval run
// --language go` driven through the real command core with a FakeRunner against
// a fixture KG produces an R1-scored outcome and persisted sidecars. It uses the
// real generator (fixture reader), real scoring bridge, real go verifier, and
// real store; only the sandbox (a fixture go module) and the agent runner are
// scripted.
func TestRunEvalCommandGoEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test shells out to the go toolchain")
	}
	requireGo(t)

	root := t.TempDir()
	runID := "eval-cli-go-int"
	// RunDir is the canonical location so the store adopts the score-stage
	// iteration-log sidecars in place (the default worktree wiring).
	inst := &sandbox.Instance{
		RunID:      runID,
		RunDir:     store.RunDir(root, runID),
		Workdir:    writeGoModule(t),
		BaseCommit: zeroCommit,
	}
	sb := &fakeSandbox{inst: inst}
	swapOpenReader(t, fixtureOpenReader)
	swapSandbox(t, func(sandbox.Config) (sandbox.Sandbox, error) { return sb, nil })
	swapRunner(t, func(runner.Adapter) (runner.Runner, error) { return okRunner(), nil })

	var buf bytes.Buffer
	err := runEvalCommand(context.Background(), &buf, runOptions{
		language: "go", repoDir: root, adapter: defaultAdapter,
	}, false)
	if err != nil {
		t.Fatalf("runEvalCommand: %v", err)
	}

	// OQ6: the run path swept stale sandboxes exactly once before provisioning.
	if sb.pruneCalls != 1 {
		t.Errorf("PruneStale called %d times on the run path, want 1", sb.pruneCalls)
	}

	// Sidecars persisted at the canonical run dir (adopt-in-place path).
	canonical := store.RunDir(root, runID)
	mustExist(t, filepath.Join(canonical, "eval-run.yaml"))
	mustExist(t, filepath.Join(canonical, "taskspec.yaml"))
	mustExist(t, filepath.Join(canonical, "iteration-log", "iter-1.yaml"))
	mustExist(t, filepath.Join(canonical, "iteration-log", "iter-1.score.yaml"))

	// The run is R1-scored: eval-run.yaml carries a scored value + band.
	rec, ok := readRunRecord(canonical)
	if !ok {
		t.Fatal("eval-run.yaml did not parse")
	}
	if !rec.Score.Scored || rec.Score.Band == "" {
		t.Fatalf("run not R1-scored: %+v", rec.Score)
	}
	if !rec.Verify.Passed {
		t.Errorf("fixture module should verify clean: %+v", rec.Verify)
	}

	out := buf.String()
	if !strings.Contains(out, runID) || !strings.Contains(out, "pass") {
		t.Errorf("run summary missing id/verify:\n%s", out)
	}
}

// TestRunCommandExecute drives the assembled cobra command through the real
// RunEval entry point to cover RunEval + runOptionsFrom + flag wiring
// end-to-end.
func TestRunCommandExecute(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test shells out to the go toolchain")
	}
	requireGo(t)
	root := t.TempDir()
	runID := "eval-cli-exec"
	inst := &sandbox.Instance{RunID: runID, RunDir: store.RunDir(root, runID), Workdir: writeGoModule(t), BaseCommit: zeroCommit}
	swapOpenReader(t, fixtureOpenReader)
	swapSandbox(t, func(sandbox.Config) (sandbox.Sandbox, error) { return &fakeSandbox{inst: inst}, nil })
	swapRunner(t, func(runner.Adapter) (runner.Runner, error) { return okRunner(), nil })

	cmd := newRunCmd(func(c *cobra.Command, _ []string) error { return RunEval(c, false) })
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"--language", "go", "--repo-dir", root})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("run Execute: %v", err)
	}
	if !strings.Contains(buf.String(), runID) {
		t.Errorf("run Execute output missing run id:\n%s", buf.String())
	}
}

// RunEval + runOptionsFrom without a go toolchain: an empty --language fails in
// buildHarness before any sandbox/graph wiring, so the entry point is covered
// on the error path.
func TestRunEvalEntryInvalidLanguage(t *testing.T) {
	cmd := newRunCmd(func(c *cobra.Command, _ []string) error { return RunEval(c, false) })
	cmd.SetArgs([]string{}) // no --language, no --task
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("run with no language should fail before wiring seams")
	}
}

// ---- runEval core: happy + error branches (no go toolchain) -----------------

// runEval with scripted seams persists an R1 score without the go toolchain.
func TestRunEvalPersistsWithFakeVerifier(t *testing.T) {
	root := t.TempDir()
	runID := "eval-fakever-1"
	inst := &sandbox.Instance{RunID: runID, RunDir: store.RunDir(root, runID), Workdir: t.TempDir(), BaseCommit: zeroCommit}
	h := buildTestHarness(t, &fakeSandbox{inst: inst}, okRunner(), passingVerify())

	var buf bytes.Buffer
	err := runEval(context.Background(), &buf, runContext{harness: h, root: root, language: evalcore.LanguageGo})
	if err != nil {
		t.Fatalf("runEval: %v", err)
	}
	mustExist(t, filepath.Join(store.RunDir(root, runID), "eval-run.yaml"))
	rec, ok := readRunRecord(store.RunDir(root, runID))
	if !ok || !rec.Score.Scored {
		t.Fatalf("expected a scored run, got ok=%v rec=%+v", ok, rec)
	}
}

func TestRunEvalHarnessError(t *testing.T) {
	h := buildTestHarness(t, &fakeSandbox{err: errFixture}, okRunner(), passingVerify())
	err := runEval(context.Background(), &bytes.Buffer{}, runContext{harness: h, root: t.TempDir(), language: evalcore.LanguageGo})
	if err == nil {
		t.Fatal("provision failure should surface as a run error")
	}
}

func TestRunEvalPersistError(t *testing.T) {
	inst := &sandbox.Instance{RunID: "eval-noroot", RunDir: t.TempDir(), Workdir: t.TempDir(), BaseCommit: zeroCommit}
	h := buildTestHarness(t, &fakeSandbox{inst: inst}, okRunner(), passingVerify())
	// Empty root makes store.WriteEvalRun reject the run (ErrEmptyRoot).
	err := runEval(context.Background(), &bytes.Buffer{}, runContext{harness: h, root: "", language: evalcore.LanguageGo})
	if err == nil {
		t.Fatal("empty root should fail persistence")
	}
}

// ---- buildHarness wiring branches -------------------------------------------

func TestBuildHarnessSuccess(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	sb := &fakeSandbox{}
	swapSandbox(t, func(sandbox.Config) (sandbox.Sandbox, error) { return sb, nil })
	swapRunner(t, func(runner.Adapter) (runner.Runner, error) { return &runner.FakeRunner{}, nil })
	h, lang, closeFn, err := buildHarness(t.TempDir(), runOptions{language: "go"})
	if err != nil {
		t.Fatalf("buildHarness: %v", err)
	}
	defer closeReader(closeFn)
	if h == nil || lang != evalcore.LanguageGo {
		t.Fatalf("buildHarness returned h=%v lang=%q", h, lang)
	}
	// OQ6: a successful sandbox wiring sweeps stale worktrees exactly once.
	if sb.pruneCalls != 1 {
		t.Errorf("PruneStale called %d times, want 1", sb.pruneCalls)
	}
}

// A prune FAILURE is best-effort: it is surfaced on warnOut but never aborts
// the run (buildHarness still succeeds).
func TestBuildHarnessPruneFailureDoesNotAbort(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	sb := &fakeSandbox{pruneErr: errFixture}
	swapSandbox(t, func(sandbox.Config) (sandbox.Sandbox, error) { return sb, nil })
	swapRunner(t, func(runner.Adapter) (runner.Runner, error) { return &runner.FakeRunner{}, nil })
	var warn bytes.Buffer
	swapWarnOut(t, &warn)

	h, _, closeFn, err := buildHarness(t.TempDir(), runOptions{language: "go"})
	if err != nil {
		t.Fatalf("prune failure must not abort buildHarness: %v", err)
	}
	defer closeReader(closeFn)
	if h == nil {
		t.Fatal("buildHarness returned a nil harness despite only a prune failure")
	}
	if sb.pruneCalls != 1 {
		t.Errorf("PruneStale called %d times, want 1", sb.pruneCalls)
	}
	if !strings.Contains(warn.String(), "prune failed") {
		t.Errorf("prune failure not surfaced as a warning: %q", warn.String())
	}
}

// A non-empty prune reports the pruned count on warnOut.
func TestBuildHarnessPruneReportsCount(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	sb := &fakeSandbox{pruned: []string{"old-run-1", "old-run-2"}}
	swapSandbox(t, func(sandbox.Config) (sandbox.Sandbox, error) { return sb, nil })
	swapRunner(t, func(runner.Adapter) (runner.Runner, error) { return &runner.FakeRunner{}, nil })
	var warn bytes.Buffer
	swapWarnOut(t, &warn)

	_, _, closeFn, err := buildHarness(t.TempDir(), runOptions{language: "go"})
	if err != nil {
		t.Fatalf("buildHarness: %v", err)
	}
	defer closeReader(closeFn)
	if !strings.Contains(warn.String(), "pruned 2") {
		t.Errorf("pruned-count not reported: %q", warn.String())
	}
}

func TestBuildHarnessInvalidLanguage(t *testing.T) {
	if _, _, _, err := buildHarness(t.TempDir(), runOptions{language: ""}); err == nil {
		t.Fatal("empty language should error before wiring seams")
	}
}

func TestBuildHarnessSandboxErrorClosesReader(t *testing.T) {
	closed := false
	swapOpenReader(t, func() (graphstore.CodeGraphReader, func() error, error) {
		return fixtureReader(), func() error { closed = true; return nil }, nil
	})
	swapSandbox(t, func(sandbox.Config) (sandbox.Sandbox, error) { return nil, errFixture })
	if _, _, _, err := buildHarness(t.TempDir(), runOptions{language: "go"}); err == nil {
		t.Fatal("sandbox error should surface")
	}
	if !closed {
		t.Error("reader not released on sandbox error")
	}
}

func TestBuildHarnessRunnerError(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	swapSandbox(t, func(sandbox.Config) (sandbox.Sandbox, error) { return &fakeSandbox{}, nil })
	swapRunner(t, func(runner.Adapter) (runner.Runner, error) { return nil, errFixture })
	if _, _, _, err := buildHarness(t.TempDir(), runOptions{language: "go"}); err == nil {
		t.Fatal("runner error should surface")
	}
}

// newRunner returning a nil runner with no error makes harness.New reject the
// wiring — covering buildHarness's harness.New error branch. (A nil sandbox is
// not usable here: the OQ6 prune runs on the sandbox before harness.New, and the
// real newSandbox never returns a nil sandbox with a nil error.)
func TestBuildHarnessNewError(t *testing.T) {
	swapOpenReader(t, fixtureOpenReader)
	swapSandbox(t, func(sandbox.Config) (sandbox.Sandbox, error) { return &fakeSandbox{}, nil })
	swapRunner(t, func(runner.Adapter) (runner.Runner, error) { return nil, nil })
	if _, _, _, err := buildHarness(t.TempDir(), runOptions{language: "go"}); err == nil {
		t.Fatal("nil runner should make harness.New fail")
	}
}

// ---- resolveGenerators / fixed-spec (--task) --------------------------------

func TestResolveGeneratorsTaskMode(t *testing.T) {
	path := writeSpecFile(t, validSpec())
	reg, lang, closeFn, err := resolveGenerators(runOptions{task: path})
	if err != nil {
		t.Fatalf("resolveGenerators: %v", err)
	}
	if closeFn != nil {
		t.Error("--task opens no reader; closer should be nil")
	}
	if lang != evalcore.LanguageGo {
		t.Fatalf("lang = %q, want go", lang)
	}
	g, ok := reg.Lookup(evalcore.LanguageGo)
	if !ok {
		t.Fatal("fixed generator not registered")
	}
	got, err := g.Generate(context.Background(), evalcore.GenerateOptions{})
	if err != nil || got.TaskID != validSpec().TaskID {
		t.Fatalf("fixed generator replay = %+v, err %v", got, err)
	}
}

func TestFixedRegistryLanguageConflict(t *testing.T) {
	path := writeSpecFile(t, validSpec()) // go spec
	if _, _, err := fixedRegistry(runOptions{task: path, language: "python"}); err == nil {
		t.Fatal("mismatched --language should conflict with the spec")
	}
}

func TestLoadTaskSpecReadError(t *testing.T) {
	if _, err := loadTaskSpec(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("missing task file should error")
	}
}

func TestLoadTaskSpecParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(path, []byte("task_spec_version: not-an-int"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadTaskSpec(path); err == nil {
		t.Fatal("malformed task file should error")
	}
}

func TestVerifiersHasGo(t *testing.T) {
	if _, ok := verifiers()[evalcore.LanguageGo]; !ok {
		t.Fatal("verifiers() must include the go verifier")
	}
}

func TestFixedRegistryLoadError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.yaml")
	if _, _, err := fixedRegistry(runOptions{task: missing}); err == nil {
		t.Fatal("fixedRegistry should surface the task load error")
	}
}

// runEvalCommand surfaces a buildHarness failure (invalid language) before any
// sandbox/runner/graph wiring runs.
func TestRunEvalCommandBuildError(t *testing.T) {
	err := runEvalCommand(context.Background(), &bytes.Buffer{}, runOptions{language: ""}, false)
	if err == nil {
		t.Fatal("empty language should fail the run before wiring")
	}
}

// The default (un-swapped) seams execute their production bodies: NewWorktreeSandbox
// rejects an empty repo path, and runner.New rejects an unknown adapter.
func TestDefaultSeams(t *testing.T) {
	if _, err := newSandbox(sandbox.Config{}); err == nil {
		t.Error("default newSandbox with empty config should error")
	}
	if _, err := newRunner(runner.Adapter("bogus-adapter")); err == nil {
		t.Error("default newRunner with an unknown adapter should error")
	}
}
