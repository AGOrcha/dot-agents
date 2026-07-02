package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
)

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

// recordingCmdFn returns a cmdFn that records each invocation and returns
// fixed values. The returned slice pointer accumulates calls for assertion.
type call struct {
	name string
	args []string
	dir  string
	env  []string
}

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

// --- claudeRunner ------------------------------------------------------------

func TestClaudeRunner_HappyPath(t *testing.T) {
	t.Parallel()
	const fakeOutput = `{"session_id":"sess-abc","model":"claude-test","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":80,"cache_creation_input_tokens":20}}`

	fn, calls := recordingCmdFn([]byte(fakeOutput), nil, 0, nil)
	r := &claudeRunner{run: fn}

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
	r := &claudeRunner{run: fixedCmdFn([]byte("error output"), nil, 1, nil)}

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
	r := &claudeRunner{run: fixedCmdFn(nil, nil, 0, nil)}
	_, err := r.Run(context.Background(), nil, minimalInstance(t))
	if err == nil {
		t.Error("Run(nil spec): want error, got nil")
	}
}

func TestClaudeRunner_NilInstance(t *testing.T) {
	t.Parallel()
	r := &claudeRunner{run: fixedCmdFn(nil, nil, 0, nil)}
	_, err := r.Run(context.Background(), minimalSpec(), nil)
	if err == nil {
		t.Error("Run(nil instance): want error, got nil")
	}
}

func TestClaudeRunner_ExecError(t *testing.T) {
	t.Parallel()
	execErr := errors.New("binary not found")
	r := &claudeRunner{run: fixedCmdFn(nil, nil, 0, execErr)}

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
	r := &claudeRunner{run: fn}

	spec := minimalSpec()
	spec.Prompt = "Write a function that reverses a string."
	_, err := r.Run(context.Background(), spec, minimalInstance(t))
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	c := (*calls)[0]
	found := false
	for _, arg := range c.args {
		if arg == spec.Prompt {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("prompt not found in exec args: %v", c.args)
	}
}

func TestClaudeRunner_SandboxEnvAppended(t *testing.T) {
	t.Parallel()
	fn, calls := recordingCmdFn(nil, nil, 0, nil)
	r := &claudeRunner{run: fn}

	inst := minimalInstance(t)
	inst.Env = []string{"HOME=/sandbox-home", "MY_VAR=sentinel"}
	_, err := r.Run(context.Background(), minimalSpec(), inst)
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	c := (*calls)[0]
	hasSentinel := false
	for _, kv := range c.env {
		if kv == "MY_VAR=sentinel" {
			hasSentinel = true
		}
	}
	if !hasSentinel {
		t.Error("sandbox env sentinel not found in exec env")
	}
}

func TestClaudeRunner_PartialJSON(t *testing.T) {
	t.Parallel()
	// Partial/non-JSON output should not cause errors; telemetry falls back
	// to the Harness-only baseline.
	r := &claudeRunner{run: fixedCmdFn([]byte("not json at all"), nil, 0, nil)}

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

func TestClaudeRunner_DurationMeasured(t *testing.T) {
	t.Parallel()
	delay := 10 * time.Millisecond
	r := &claudeRunner{run: func(_ context.Context, _ string, _ []string, _ string, _ []string) ([]byte, []byte, int, error) {
		time.Sleep(delay)
		return nil, nil, 0, nil
	}}

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
	if (*calls)[0].name != codexBin {
		t.Errorf("exec name: want %q, got %q", codexBin, (*calls)[0].name)
	}
	if result.Telemetry.Harness != "codex" {
		t.Errorf("Harness: want %q, got %q", "codex", result.Telemetry.Harness)
	}
	if result.Telemetry.Tokens != nil {
		t.Error("Tokens: want nil for codex adapter, got non-nil")
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
	r := &copilotRunner{run: fn}

	result, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if err != nil {
		t.Fatalf("Run: unexpected error: %v", err)
	}

	if len(*calls) != 1 {
		t.Fatalf("expected 1 exec call, got %d", len(*calls))
	}
	c := (*calls)[0]
	if c.name != copilotBin {
		t.Errorf("exec name: want %q, got %q", copilotBin, c.name)
	}
	if result.Telemetry.Harness != "gh-copilot" {
		t.Errorf("Harness: want %q, got %q", "gh-copilot", result.Telemetry.Harness)
	}
	if result.Telemetry.Tokens != nil {
		t.Error("Tokens: want nil for copilot adapter, got non-nil")
	}
}

func TestCopilotRunner_NilSpec(t *testing.T) {
	t.Parallel()
	r := &copilotRunner{run: fixedCmdFn(nil, nil, 0, nil)}
	_, err := r.Run(context.Background(), nil, minimalInstance(t))
	if err == nil {
		t.Error("Run(nil spec): want error, got nil")
	}
}

func TestCopilotRunner_NilInstance(t *testing.T) {
	t.Parallel()
	r := &copilotRunner{run: fixedCmdFn(nil, nil, 0, nil)}
	_, err := r.Run(context.Background(), minimalSpec(), nil)
	if err == nil {
		t.Error("Run(nil instance): want error, got nil")
	}
}

func TestCopilotRunner_ExecError(t *testing.T) {
	t.Parallel()
	execErr := errors.New("gh not found")
	r := &copilotRunner{run: fixedCmdFn(nil, nil, 0, execErr)}

	_, err := r.Run(context.Background(), minimalSpec(), minimalInstance(t))
	if !errors.Is(err, execErr) {
		t.Errorf("want error wrapping execErr, got: %v", err)
	}
}

// --- cancel / timeout (race-safe) -------------------------------------------

// TestClaudeRunner_Cancellation verifies that a cancelled context propagates
// to the exec seam. The test is -race safe because each adapter instance owns
// its seam field; there is no shared package-level state.
func TestClaudeRunner_Cancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled before Run is called

	cancelled := false
	r := &claudeRunner{run: func(
		c context.Context,
		name string,
		args []string,
		dir string,
		env []string,
	) ([]byte, []byte, int, error) {
		if c.Err() != nil {
			cancelled = true
			return nil, nil, 0, c.Err()
		}
		return nil, nil, 0, nil
	}}

	_, err := r.Run(ctx, minimalSpec(), minimalInstance(t))
	if !cancelled {
		t.Error("cancelled context was not propagated to exec seam")
	}
	if err == nil {
		t.Error("Run with cancelled context: want error, got nil")
	}
}

// TestClaudeRunner_Timeout verifies that a timed-out context propagates to
// the exec seam and returns an error.
func TestClaudeRunner_Timeout(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	// Ensure the deadline has passed before Run is called.
	time.Sleep(time.Millisecond)

	r := &claudeRunner{run: func(
		c context.Context,
		_ string,
		_ []string,
		_ string,
		_ []string,
	) ([]byte, []byte, int, error) {
		return nil, nil, 0, c.Err()
	}}

	_, err := r.Run(ctx, minimalSpec(), minimalInstance(t))
	if err == nil {
		t.Error("Run with expired timeout: want error, got nil")
	}
}

// TestConcurrentRunners verifies that two runner instances with distinct
// seams do not share state under -race. Each goroutine operates on its own
// adapter instance; the test fails under the race detector if any shared
// variable is written concurrently.
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
			r := &claudeRunner{
				run: fixedCmdFn([]byte(tag), nil, 0, nil),
			}
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
	// cache hit rate = 8 / (10 + 8 + 2) = 0.4
	want := 0.4
	if tel.Tokens.CacheHitRate < want-0.001 || tel.Tokens.CacheHitRate > want+0.001 {
		t.Errorf("CacheHitRate: want ~%.3f, got %.3f", want, tel.Tokens.CacheHitRate)
	}
}

func TestParseClaudeTelemetry_LastUsageWins(t *testing.T) {
	t.Parallel()
	// Multiple JSON lines; the last one with a usage block should win.
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
	// A line that starts with '{' but is malformed JSON; the parser must
	// skip it gracefully and still produce a valid baseline AgentTelemetry.
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
	// Sandbox overrides must appear after os.Environ() entries.
	sandbox := []string{"HOME=/sandbox", "CUSTOM=yes"}
	env := buildEnv(sandbox)

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
	// 8 / (10 + 8 + 2) = 0.4
	got := computeHitRate(u)
	if got < 0.399 || got > 0.401 {
		t.Errorf("want ~0.4, got %f", got)
	}
}

func TestComputeHitRate_ZeroTotal(t *testing.T) {
	t.Parallel()
	u := &claudeTokenUsage{}
	if got := computeHitRate(u); got != 0 {
		t.Errorf("want 0, got %f", got)
	}
}

// --- realExec (integration tests; requires go on PATH) ----------------------

// TestRealExec_SuccessPath verifies the happy path of realExec using the go
// binary, which is guaranteed to be present in any Go test environment.
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

// TestRealExec_NonZeroExit verifies that a non-zero exit code is reported in
// the return value, not as a Go error. `go build` in an empty directory exits
// non-zero because there are no Go files.
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

// TestRealExec_BinaryNotFound verifies that a missing binary produces a Go
// error (not a non-zero exit code), which callers treat as a launch failure.
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
// before exec returns an error from realExec.
func TestRealExec_CancelledContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, _, err := realExec(ctx, "go", []string{"version"}, t.TempDir(), os.Environ())
	if err == nil {
		t.Error("want error for cancelled context, got nil")
	}
}
