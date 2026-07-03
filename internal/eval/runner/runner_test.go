package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// sleepEnvVar makes the test binary re-enter as a subprocess that sleeps for
// the given number of milliseconds, giving realExec a portable long-running
// process to cancel/time-out under -race (the stdlib subprocess-helper
// pattern). TestMain intercepts it before running the suite.
const sleepEnvVar = "RUNNER_TEST_SLEEP_MS"

func TestMain(m *testing.M) {
	if ms := os.Getenv(sleepEnvVar); ms != "" {
		d, _ := strconv.Atoi(ms)
		time.Sleep(time.Duration(d) * time.Millisecond)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// --- fixtures ----------------------------------------------------------------

// minimalSpec returns a minimal valid TaskSpec for use in runner tests.
func minimalSpec() *eval.TaskSpec {
	return &eval.TaskSpec{
		TaskSpecVersion: eval.CurrentTaskSpecVersion,
		TaskID:          "test-task-001",
		Language:        eval.LanguageGo,
		Difficulty:      eval.DifficultyEasy,
		GeneratedFrom: eval.GeneratedFrom{
			Kind:       eval.KindKGTemplate,
			TemplateID: "impl-pure-fn",
		},
		Prompt: "Implement function Foo.",
		Verification: eval.Verification{
			TestCmd: []string{"go", "test", "./..."},
		},
	}
}

// minimalInstance returns a minimal sandbox Instance for use in runner tests.
func minimalInstance(t *testing.T) *sandbox.Instance {
	t.Helper()
	return &sandbox.Instance{
		RunID:      "run-001",
		RunDir:     t.TempDir(),
		Workdir:    t.TempDir(),
		BaseCommit: "abc123",
		Env:        []string{"HOME=/tmp/test-home", "USERPROFILE=/tmp/test-home"},
	}
}

// fixedCmdFn returns a cmdFn that immediately returns the given values
// without spawning any subprocess.
func fixedCmdFn(stdout []byte, stderr []byte, exitCode int, err error) cmdFn {
	return func(_ context.Context, _ string, _ []string, _ string, _ []string) ([]byte, []byte, int, error) {
		return stdout, stderr, exitCode, err
	}
}

// call records one exec-seam invocation for assertion.
type call struct {
	name string
	args []string
	dir  string
	env  []string
}

// recordingCmdFn returns a cmdFn that records each invocation and returns
// fixed values. The returned slice pointer accumulates calls.
func recordingCmdFn(
	stdout []byte,
	stderr []byte,
	exitCode int,
	err error,
) (cmdFn, *[]call) {
	calls := new([]call)
	fn := func(
		_ context.Context,
		name string,
		args []string,
		dir string,
		env []string,
	) ([]byte, []byte, int, error) {
		*calls = append(*calls, call{name: name, args: args, dir: dir, env: env})
		return stdout, stderr, exitCode, err
	}
	return fn, calls
}

// emptyScan is a token-scanner seam that always reports no telemetry.
func emptyScan(_, _, _, _ string) platform.SessionTokenMetrics {
	return platform.SessionTokenMetrics{}
}

// fakeScan returns a token-scanner seam that always reports m.
func fakeScan(m platform.SessionTokenMetrics) scanFn {
	return func(_, _, _, _ string) platform.SessionTokenMetrics { return m }
}

// --- New / factory -----------------------------------------------------------

func TestNew_KnownAdapters(t *testing.T) {
	t.Parallel()
	for _, adapter := range []Adapter{AdapterClaude, AdapterCodex, AdapterCopilot} {
		r, err := New(adapter)
		if err != nil {
			t.Errorf("New(%q): unexpected error: %v", adapter, err)
		}
		if r == nil {
			t.Errorf("New(%q): returned nil runner", adapter)
		}
	}
}

func TestNew_UnknownAdapter(t *testing.T) {
	t.Parallel()
	_, err := New(Adapter("mystery-platform"))
	if !errors.Is(err, ErrUnknownAdapter) {
		t.Errorf("New(unknown): want ErrUnknownAdapter, got %v", err)
	}
}

// --- FakeRunner --------------------------------------------------------------

func TestFakeRunner_ReturnsCannedResult(t *testing.T) {
	t.Parallel()
	canned := Result{
		Stdout:   []byte("scripted output"),
		ExitCode: 0,
		Telemetry: AgentTelemetry{
			Harness: "fake",
			Model:   "fake-model",
		},
	}
	f := &FakeRunner{Result: canned}

	spec := minimalSpec()
	inst := minimalInstance(t)
	got, err := f.Run(context.Background(), spec, inst)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if string(got.Stdout) != "scripted output" {
		t.Errorf("Stdout: want %q, got %q", "scripted output", string(got.Stdout))
	}
	if got.Telemetry.Harness != "fake" {
		t.Errorf("Harness: want %q, got %q", "fake", got.Telemetry.Harness)
	}
	// Call recording lets consumers assert the harness wired spec + instance.
	if f.Calls != 1 {
		t.Errorf("Calls: want 1, got %d", f.Calls)
	}
	if f.LastSpec != spec {
		t.Error("LastSpec was not recorded")
	}
	if f.LastInstance != inst {
		t.Error("LastInstance was not recorded")
	}
}

func TestFakeRunner_ReturnsCannedErr(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("scripted launch failure")
	f := &FakeRunner{Err: sentinel}

	_, err := f.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if !errors.Is(err, sentinel) {
		t.Errorf("Run: want scripted error, got %v", err)
	}
}

func TestFakeRunner_SatisfiesRunner(t *testing.T) {
	t.Parallel()
	// Compile-time guarantee is in fake.go; assert it holds at the value level
	// too so a downstream consumer can inject a FakeRunner as a Runner.
	var r Runner = &FakeRunner{}
	if _, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t)); err != nil {
		t.Errorf("Run via Runner interface: unexpected error: %v", err)
	}
}

// --- claudeRunner ------------------------------------------------------------

func TestClaudeRunner_HappyPath(t *testing.T) {
	t.Parallel()
	const fakeOutput = `{"session_id":"sess-abc","model":"claude-test","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":80,"cache_creation_input_tokens":20}}`

	fn, calls := recordingCmdFn([]byte(fakeOutput), nil, 0, nil)
	r := &claudeRunner{run: fn, scan: emptyScan}

	spec := minimalSpec()
	inst := minimalInstance(t)
	result, err := r.Run(context.Background(), spec, inst)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	// Verify exec was called once with the right binary and workdir.
	if len(*calls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(*calls))
	}
	c := (*calls)[0]
	if c.name != claudeBin {
		t.Errorf("exec name: want %q, got %q", claudeBin, c.name)
	}
	if c.dir != inst.Workdir {
		t.Errorf("exec dir: want %q, got %q", inst.Workdir, c.dir)
	}

	// Verify telemetry was extracted from JSON output.
	if result.Telemetry.Harness != "claude-code" {
		t.Errorf("Harness: want %q, got %q", "claude-code", result.Telemetry.Harness)
	}
	if result.Telemetry.SessionID != "sess-abc" {
		t.Errorf("SessionID: want %q, got %q", "sess-abc", result.Telemetry.SessionID)
	}
	if result.Telemetry.Model != "claude-test" {
		t.Errorf("Model: want %q, got %q", "claude-test", result.Telemetry.Model)
	}
	if result.Telemetry.Tokens == nil {
		t.Fatal("Tokens: want non-nil, got nil")
	}
	if result.Telemetry.Tokens.InputTokens != 100 {
		t.Errorf("InputTokens: want 100, got %d", result.Telemetry.Tokens.InputTokens)
	}
}

func TestClaudeRunner_NonZeroExit(t *testing.T) {
	t.Parallel()
	r := &claudeRunner{run: fixedCmdFn([]byte("error output"), nil, 1, nil), scan: emptyScan}

	result, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if err != nil {
		t.Fatalf("non-zero exit should not produce a Go error, got: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("ExitCode: want 1, got %d", result.ExitCode)
	}
}

func TestClaudeRunner_NilSpec(t *testing.T) {
	t.Parallel()
	r := &claudeRunner{run: fixedCmdFn(nil, nil, 0, nil), scan: emptyScan}
	_, err := r.Run(context.Background(), nil, minimalInstance(t))
	if err == nil {
		t.Error("Run(nil spec): want error, got nil")
	}
}

func TestClaudeRunner_NilInstance(t *testing.T) {
	t.Parallel()
	r := &claudeRunner{run: fixedCmdFn(nil, nil, 0, nil), scan: emptyScan}
	_, err := r.Run(context.Background(), minimalSpec(), nil)
	if err == nil {
		t.Error("Run(nil instance): want error, got nil")
	}
}

func TestClaudeRunner_ExecError(t *testing.T) {
	t.Parallel()
	execErr := errors.New("binary not found")
	r := &claudeRunner{run: fixedCmdFn(nil, nil, 0, execErr), scan: emptyScan}

	_, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if err == nil {
		t.Fatal("Run: want error wrapping exec failure, got nil")
	}
	if !errors.Is(err, execErr) {
		t.Errorf("want error to wrap execErr, got: %v", err)
	}
}

func TestClaudeRunner_PromptPassedAsArg(t *testing.T) {
	t.Parallel()
	fn, calls := recordingCmdFn(nil, nil, 0, nil)
	r := &claudeRunner{run: fn, scan: emptyScan}

	spec := minimalSpec()
	spec.Prompt = "Write a function that reverses a string."
	_, err := r.Run(context.Background(), spec, minimalInstance(t))
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	c := (*calls)[0]
	if !argsContain(c.args, spec.Prompt) {
		t.Errorf("prompt not found in exec args: %v", c.args)
	}
	// Headless flag must be present so claude does not open an interactive TUI.
	if !argsContain(c.args, "--print") {
		t.Errorf("--print headless flag not found in exec args: %v", c.args)
	}
}

func TestClaudeRunner_SandboxEnvAppended(t *testing.T) {
	t.Parallel()
	fn, calls := recordingCmdFn(nil, nil, 0, nil)
	r := &claudeRunner{run: fn, scan: emptyScan}

	inst := minimalInstance(t)
	inst.Env = []string{"HOME=/sandbox-home", "MY_VAR=sentinel"}
	_, err := r.Run(context.Background(), minimalSpec(), inst)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	if !argsContain((*calls)[0].env, "MY_VAR=sentinel") {
		t.Error("sandbox env sentinel not found in exec env")
	}
}

func TestClaudeRunner_PartialJSON(t *testing.T) {
	t.Parallel()
	// Non-JSON output should not error; telemetry falls back to Harness-only.
	r := &claudeRunner{run: fixedCmdFn([]byte("not json at all"), nil, 0, nil), scan: emptyScan}

	result, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if result.Telemetry.Harness != "claude-code" {
		t.Errorf("Harness: want %q, got %q", "claude-code", result.Telemetry.Harness)
	}
	if result.Telemetry.Tokens != nil {
		t.Error("Tokens: want nil for non-JSON output, got non-nil")
	}
}

// TestClaudeRunner_ScannerBackfill exercises the fallback: the inline JSON
// carried a session id but no usage block, so the platform claude scanner is
// consulted and its metrics populate Telemetry.Tokens.
func TestClaudeRunner_ScannerBackfill(t *testing.T) {
	t.Parallel()
	const idOnly = `{"session_id":"sess-xyz","model":"claude-test"}`
	scan := fakeScan(platform.SessionTokenMetrics{
		InputTokens:  200,
		OutputTokens: 60,
	})
	r := &claudeRunner{run: fixedCmdFn([]byte(idOnly), nil, 0, nil), scan: scan}

	result, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if result.Telemetry.Tokens == nil {
		t.Fatal("Tokens: want non-nil from scanner backfill, got nil")
	}
	if result.Telemetry.Tokens.InputTokens != 200 {
		t.Errorf("InputTokens: want 200, got %d", result.Telemetry.Tokens.InputTokens)
	}
}

// TestClaudeRunner_NoBackfillWhenInlineTokens verifies the scanner is NOT
// consulted when the inline JSON already carried usage.
func TestClaudeRunner_NoBackfillWhenInlineTokens(t *testing.T) {
	t.Parallel()
	const withUsage = `{"session_id":"sess-1","usage":{"input_tokens":5,"output_tokens":1}}`
	called := false
	scan := func(_, _, _, _ string) platform.SessionTokenMetrics {
		called = true
		return platform.SessionTokenMetrics{InputTokens: 999}
	}
	r := &claudeRunner{run: fixedCmdFn([]byte(withUsage), nil, 0, nil), scan: scan}

	result, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if called {
		t.Error("scanner should not be consulted when inline usage is present")
	}
	if result.Telemetry.Tokens.InputTokens != 5 {
		t.Errorf("InputTokens: want inline 5, got %d", result.Telemetry.Tokens.InputTokens)
	}
}

func TestClaudeRunner_DurationMeasured(t *testing.T) {
	t.Parallel()
	delay := 10 * time.Millisecond
	r := &claudeRunner{run: func(_ context.Context, _ string, _ []string, _ string, _ []string) ([]byte, []byte, int, error) {
		time.Sleep(delay)
		return nil, nil, 0, nil
	}, scan: emptyScan}

	result, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if result.Duration < delay {
		t.Errorf("Duration: want >= %v, got %v", delay, result.Duration)
	}
}

// --- codexRunner -------------------------------------------------------------

func TestCodexRunner_HappyPath(t *testing.T) {
	t.Parallel()
	fn, calls := recordingCmdFn([]byte("codex output"), nil, 0, nil)
	r := &codexRunner{run: fn}

	result, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	if len(*calls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(*calls))
	}
	c := (*calls)[0]
	if c.name != codexBin {
		t.Errorf("exec name: want %q, got %q", codexBin, c.name)
	}
	// FIX 1: the canonical headless invocation is `codex exec <prompt>`.
	if len(c.args) < 2 || c.args[0] != codexExecSub {
		t.Errorf("codex args: want [exec <prompt>], got %v", c.args)
	}
	if !argsContain(c.args, minimalSpec().Prompt) {
		t.Errorf("prompt not found in codex args: %v", c.args)
	}
	if result.Telemetry.Harness != "codex" {
		t.Errorf("Harness: want %q, got %q", "codex", result.Telemetry.Harness)
	}
	// Documented gap: codex token telemetry stays nil in v1.
	if result.Telemetry.Tokens != nil {
		t.Error("Tokens: want nil for codex adapter (documented gap), got non-nil")
	}
}

func TestCodexRunner_NilSpec(t *testing.T) {
	t.Parallel()
	r := &codexRunner{run: fixedCmdFn(nil, nil, 0, nil)}
	_, err := r.Run(context.Background(), nil, minimalInstance(t))
	if err == nil {
		t.Error("Run(nil spec): want error, got nil")
	}
}

func TestCodexRunner_NilInstance(t *testing.T) {
	t.Parallel()
	r := &codexRunner{run: fixedCmdFn(nil, nil, 0, nil)}
	_, err := r.Run(context.Background(), minimalSpec(), nil)
	if err == nil {
		t.Error("Run(nil instance): want error, got nil")
	}
}

func TestCodexRunner_NonZeroExit(t *testing.T) {
	t.Parallel()
	r := &codexRunner{run: fixedCmdFn(nil, []byte("err"), 2, nil)}

	result, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if err != nil {
		t.Fatalf("non-zero exit should not produce a Go error, got: %v", err)
	}
	if result.ExitCode != 2 {
		t.Errorf("ExitCode: want 2, got %d", result.ExitCode)
	}
}

func TestCodexRunner_ExecError(t *testing.T) {
	t.Parallel()
	execErr := errors.New("codex not installed")
	r := &codexRunner{run: fixedCmdFn(nil, nil, 0, execErr)}

	_, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if !errors.Is(err, execErr) {
		t.Errorf("want error wrapping execErr, got: %v", err)
	}
}

// --- copilotRunner -----------------------------------------------------------

func TestCopilotRunner_HappyPath(t *testing.T) {
	t.Parallel()
	fn, calls := recordingCmdFn([]byte("copilot suggestion"), nil, 0, nil)
	r := &copilotRunner{run: fn, scan: emptyScan}

	result, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	if len(*calls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(*calls))
	}
	c := (*calls)[0]
	// Assert the FULL argv is the canonical `copilot -p <prompt>` code-gen
	// form — NOT the `gh copilot suggest` shell-suggestion extension. A bare
	// arg[0] check would pass for any wrong subcommand.
	if c.name != copilotBin {
		t.Errorf("exec name: want %q, got %q", copilotBin, c.name)
	}
	wantArgs := []string{"-p", minimalSpec().Prompt}
	if !reflect.DeepEqual(c.args, wantArgs) {
		t.Errorf("copilot argv: want %v, got %v", wantArgs, c.args)
	}
	if result.Telemetry.Harness != "gh-copilot" {
		t.Errorf("Harness: want %q, got %q", "gh-copilot", result.Telemetry.Harness)
	}
	// Empty scan → no telemetry recovered → nil (first-class absent).
	if result.Telemetry.Tokens != nil {
		t.Error("Tokens: want nil when scan finds nothing, got non-nil")
	}
}

// TestCopilotRunner_TokenScan wires a non-empty scanner and asserts its
// metrics are mapped onto Telemetry.Tokens (FIX 3).
func TestCopilotRunner_TokenScan(t *testing.T) {
	t.Parallel()
	scan := fakeScan(platform.SessionTokenMetrics{
		InputTokens:         300,
		OutputTokens:        90,
		CacheReadTokens:     150,
		CacheCreationTokens: 50,
	})
	r := &copilotRunner{run: fixedCmdFn([]byte("ok"), nil, 0, nil), scan: scan}

	result, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}
	if result.Telemetry.Tokens == nil {
		t.Fatal("Tokens: want non-nil from scanner, got nil")
	}
	if result.Telemetry.Tokens.InputTokens != 300 {
		t.Errorf("InputTokens: want 300, got %d", result.Telemetry.Tokens.InputTokens)
	}
	// CacheHitRate derived from 150 / (150 + 50) = 0.75.
	if hit := result.Telemetry.Tokens.CacheHitRate; hit < 0.749 || hit > 0.751 {
		t.Errorf("CacheHitRate: want ~0.75, got %.3f", hit)
	}
}

func TestCopilotRunner_NilSpec(t *testing.T) {
	t.Parallel()
	r := &copilotRunner{run: fixedCmdFn(nil, nil, 0, nil), scan: emptyScan}
	_, err := r.Run(context.Background(), nil, minimalInstance(t))
	if err == nil {
		t.Error("Run(nil spec): want error, got nil")
	}
}

func TestCopilotRunner_NilInstance(t *testing.T) {
	t.Parallel()
	r := &copilotRunner{run: fixedCmdFn(nil, nil, 0, nil), scan: emptyScan}
	_, err := r.Run(context.Background(), minimalSpec(), nil)
	if err == nil {
		t.Error("Run(nil instance): want error, got nil")
	}
}

func TestCopilotRunner_ExecError(t *testing.T) {
	t.Parallel()
	execErr := errors.New("gh not found")
	r := &copilotRunner{run: fixedCmdFn(nil, nil, 0, execErr), scan: emptyScan}

	_, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if !errors.Is(err, execErr) {
		t.Errorf("want error wrapping execErr, got: %v", err)
	}
}

// --- cancel / timeout (race-safe) -------------------------------------------

// TestClaudeRunner_Cancellation verifies that a cancelled context propagates
// to the exec seam. Each adapter instance owns its seam field, so this is
// -race safe with no shared package-level state.
func TestClaudeRunner_Cancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before Run is called

	cancelled := false
	r := &claudeRunner{run: func(
		c context.Context,
		_ string,
		_ []string,
		_ string,
		_ []string,
	) ([]byte, []byte, int, error) {
		if c.Err() != nil {
			cancelled = true
			return nil, nil, 0, c.Err()
		}
		return nil, nil, 0, nil
	}, scan: emptyScan}

	_, err := r.Run(ctx, minimalSpec(), minimalInstance(t))
	if !cancelled {
		t.Error("cancelled context was not propagated to exec seam")
	}
	if err == nil {
		t.Error("Run with cancelled context: want error, got nil")
	}
}

// --- realExec integration (requires go on PATH) -----------------------------

func TestRealExec_SuccessPath(t *testing.T) {
	t.Parallel()
	stdout, _, code, err := realExec(
		context.Background(), "go", []string{"version"},
		t.TempDir(), os.Environ(),
	)
	if err != nil {
		t.Fatalf("realExec: unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code: want 0, got %d", code)
	}
	if len(stdout) == 0 {
		t.Error("stdout: want non-empty for go version")
	}
}

// TestRealExec_NonZeroExit verifies a non-zero exit is reported in the return
// value, not as a Go error. `go build` in an empty dir exits non-zero.
func TestRealExec_NonZeroExit(t *testing.T) {
	t.Parallel()
	_, _, code, err := realExec(
		context.Background(), "go", []string{"build", "./..."},
		t.TempDir(), os.Environ(),
	)
	if err != nil {
		t.Fatalf("realExec: non-zero exit must not produce a Go error, got: %v", err)
	}
	if code == 0 {
		t.Error("exit code: want non-zero for go build in empty dir, got 0")
	}
}

// TestRealExec_BinaryNotFound verifies a missing binary produces a Go error
// (not a non-zero exit code), which callers treat as a launch failure.
func TestRealExec_BinaryNotFound(t *testing.T) {
	t.Parallel()
	_, _, _, err := realExec(
		context.Background(),
		"no-such-binary-runner-xyz-abc-42",
		nil,
		t.TempDir(),
		os.Environ(),
	)
	if err == nil {
		t.Error("want error for missing binary, got nil")
	}
}

// TestRealExec_CancelledContext verifies that a context already cancelled
// before exec returns an error that unwraps to context.Canceled — an infra
// failure, NOT a normal non-zero-exit Result (FIX 2).
func TestRealExec_CancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, code, err := realExec(ctx, "go", []string{"version"}, t.TempDir(), os.Environ())
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want error unwrapping to context.Canceled, got: %v", err)
	}
	if code != -1 {
		t.Errorf("exit code: want -1 (infra), got %d", code)
	}
}

// TestRealExec_TimeoutKilledMidRun is the -race regression for FIX 2: a
// subprocess that outlives a short context deadline is killed mid-run, and
// realExec must surface context.DeadlineExceeded rather than a normal
// non-zero-exit Result. Uses the TestMain sleep-helper subprocess so the test
// is portable across the macos/windows/ubuntu matrix.
func TestRealExec_TimeoutKilledMidRun(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	env := append(os.Environ(), sleepEnvVar+"=5000")
	_, _, code, err := realExec(ctx, os.Args[0], nil, t.TempDir(), env)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("want error unwrapping to context.DeadlineExceeded, got: %v", err)
	}
	if code != -1 {
		t.Errorf("exit code: want -1 (infra kill), got %d", code)
	}
}

// TestConcurrentRunners verifies that four runner instances with distinct
// seams do not share state under -race.
func TestConcurrentRunners(t *testing.T) {
	t.Parallel()

	const workers = 4
	var wg sync.WaitGroup
	wg.Add(workers)

	errs := make([]error, workers)
	for i := 0; i < workers; i++ {
		idx := i
		go func() {
			defer wg.Done()
			tag := fmt.Sprintf("worker-%d", idx)
			r := &claudeRunner{run: fixedCmdFn([]byte(tag), nil, 0, nil), scan: emptyScan}
			result, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
			if err != nil {
				errs[idx] = err
				return
			}
			if string(result.Stdout) != tag {
				errs[idx] = fmt.Errorf("worker %d: stdout mismatch: want %q, got %q", idx, tag, string(result.Stdout))
			}
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("worker %d: %v", i, err)
		}
	}
}

// --- parseClaudeTelemetry ----------------------------------------------------

func TestParseClaudeTelemetry_ValidJSON(t *testing.T) {
	t.Parallel()
	input := `{"session_id":"sid-1","model":"m1","usage":{"input_tokens":10,"output_tokens":5,"cache_read_input_tokens":8,"cache_creation_input_tokens":2}}`
	tel := parseClaudeTelemetry([]byte(input))

	if tel.SessionID != "sid-1" {
		t.Errorf("SessionID: want %q, got %q", "sid-1", tel.SessionID)
	}
	if tel.Model != "m1" {
		t.Errorf("Model: want %q, got %q", "m1", tel.Model)
	}
	if tel.Tokens == nil {
		t.Fatal("Tokens: want non-nil")
	}
	if tel.Tokens.InputTokens != 10 {
		t.Errorf("InputTokens: want 10, got %d", tel.Tokens.InputTokens)
	}
	// cache hit rate = cache_read / (cache_read + cache_creation) = 8 / (8 + 2)
	// = 0.8. input_tokens (10) is NOT in the denominator.
	want := 0.8
	if tel.Tokens.CacheHitRate < want-0.001 || tel.Tokens.CacheHitRate > want+0.001 {
		t.Errorf("CacheHitRate: want ~%.3f, got %.3f", want, tel.Tokens.CacheHitRate)
	}
}

func TestParseClaudeTelemetry_LastUsageWins(t *testing.T) {
	t.Parallel()
	input := `{"session_id":"first"}
{"session_id":"second","usage":{"input_tokens":5,"output_tokens":1}}`
	tel := parseClaudeTelemetry([]byte(input))
	if tel.SessionID != "second" {
		t.Errorf("SessionID: want %q, got %q", "second", tel.SessionID)
	}
	if tel.Tokens == nil {
		t.Error("Tokens: want non-nil for second line")
	}
}

func TestParseClaudeTelemetry_NonJSON(t *testing.T) {
	t.Parallel()
	tel := parseClaudeTelemetry([]byte("plain text output\nno json here"))
	if tel.Harness != "claude-code" {
		t.Errorf("Harness: want %q, got %q", "claude-code", tel.Harness)
	}
	if tel.Tokens != nil {
		t.Error("Tokens: want nil for non-JSON output")
	}
}

func TestParseClaudeTelemetry_InvalidJSONLine(t *testing.T) {
	t.Parallel()
	tel := parseClaudeTelemetry([]byte("{not-valid-json!!!}"))
	if tel.Harness != "claude-code" {
		t.Errorf("Harness: want %q, got %q", "claude-code", tel.Harness)
	}
	if tel.Tokens != nil {
		t.Error("Tokens: want nil for malformed JSON line")
	}
}

func TestParseClaudeTelemetry_ExplicitHitRate(t *testing.T) {
	t.Parallel()
	input := `{"usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"cache_hit_rate":0.75}}`
	tel := parseClaudeTelemetry([]byte(input))
	if tel.Tokens == nil {
		t.Fatal("Tokens: want non-nil")
	}
	if tel.Tokens.CacheHitRate < 0.749 || tel.Tokens.CacheHitRate > 0.751 {
		t.Errorf("CacheHitRate: want 0.75, got %.3f", tel.Tokens.CacheHitRate)
	}
}

// --- buildEnv ----------------------------------------------------------------

func TestBuildEnv_SandboxEnvAppendedLast(t *testing.T) {
	t.Parallel()
	sandboxEnv := []string{"HOME=/sandbox", "CUSTOM=yes"}
	env := buildEnv(sandboxEnv)

	lastHome := ""
	for _, kv := range env {
		if len(kv) >= 5 && kv[:5] == "HOME=" {
			lastHome = kv
		}
	}
	if lastHome != "HOME=/sandbox" {
		t.Errorf("last HOME entry: want HOME=/sandbox, got %q", lastHome)
	}
}

// --- computeHitRate ----------------------------------------------------------

func TestComputeHitRate_ExplicitRate(t *testing.T) {
	t.Parallel()
	u := &claudeTokenUsage{CacheHitRate: 0.5}
	if got := computeHitRate(u); got != 0.5 {
		t.Errorf("want 0.5, got %f", got)
	}
}

func TestComputeHitRate_Derived(t *testing.T) {
	t.Parallel()
	u := &claudeTokenUsage{
		InputTokens:         10,
		CacheReadTokens:     8,
		CacheCreationTokens: 2,
	}
	// 8 / (8 + 2) = 0.8; input_tokens excluded from the denominator.
	got := computeHitRate(u)
	if got < 0.799 || got > 0.801 {
		t.Errorf("want ~0.8, got %f", got)
	}
}

// TestComputeHitRate_ExcludesInputTokens is the FIX A regression: with a
// nonzero input_tokens, the derived rate must equal
// cache_read / (cache_read + cache_creation) — proving input_tokens is NOT in
// the denominator (the shipped contract in signal_backfill.go / session.go).
func TestComputeHitRate_ExcludesInputTokens(t *testing.T) {
	t.Parallel()
	const (
		cacheRead     = 30
		cacheCreation = 10
	)
	u := &claudeTokenUsage{
		InputTokens:         5000, // large; must not affect the rate
		CacheReadTokens:     cacheRead,
		CacheCreationTokens: cacheCreation,
	}
	want := float64(cacheRead) / float64(cacheRead+cacheCreation) // 0.75
	got := computeHitRate(u)
	if got < want-0.0001 || got > want+0.0001 {
		t.Errorf("CacheHitRate: want %.4f (cache_read/(cache_read+cache_creation)), got %.4f", want, got)
	}
}

func TestComputeHitRate_ZeroTotal(t *testing.T) {
	t.Parallel()
	u := &claudeTokenUsage{}
	if got := computeHitRate(u); got != 0 {
		t.Errorf("want 0, got %f", got)
	}
}

// --- tokens helpers ----------------------------------------------------------

func TestScratchHomeFromEnv(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		env  []string
		want string
	}{
		{"home wins", []string{"HOME=/a", "USERPROFILE=/b"}, "/a"},
		{"userprofile fallback", []string{"USERPROFILE=/only"}, "/only"},
		{"last home wins", []string{"HOME=/first", "HOME=/second"}, "/second"},
		{"none", []string{"PATH=/usr/bin"}, ""},
		{"empty", nil, ""},
	}
	for _, tc := range cases {
		if got := scratchHomeFromEnv(tc.env); got != tc.want {
			t.Errorf("%s: want %q, got %q", tc.name, tc.want, got)
		}
	}
}

func TestTokensFromMetrics_Empty(t *testing.T) {
	t.Parallel()
	if got := tokensFromMetrics(platform.SessionTokenMetrics{}); got != nil {
		t.Errorf("want nil for empty metrics, got %+v", got)
	}
}

func TestTokensFromMetrics_Mapped(t *testing.T) {
	t.Parallel()
	m := platform.SessionTokenMetrics{
		InputTokens:         100,
		OutputTokens:        40,
		CacheReadTokens:     30,
		CacheCreationTokens: 10,
	}
	got := tokensFromMetrics(m)
	if got == nil {
		t.Fatal("want non-nil, got nil")
	}
	if got.InputTokens != 100 || got.OutputTokens != 40 {
		t.Errorf("token mapping mismatch: %+v", got)
	}
	// derived hit rate = 30 / (30 + 10) = 0.75
	if got.CacheHitRate < 0.749 || got.CacheHitRate > 0.751 {
		t.Errorf("CacheHitRate: want ~0.75, got %.3f", got.CacheHitRate)
	}
}

func TestTokensFromMetrics_ExplicitHitRate(t *testing.T) {
	t.Parallel()
	m := platform.SessionTokenMetrics{OutputTokens: 5, CacheHitRate: 0.9}
	got := tokensFromMetrics(m)
	if got == nil {
		t.Fatal("want non-nil, got nil")
	}
	if got.CacheHitRate != 0.9 {
		t.Errorf("CacheHitRate: want 0.9, got %.3f", got.CacheHitRate)
	}
}

// --- helpers -----------------------------------------------------------------

func argsContain(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
