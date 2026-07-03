package goverifier

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
)

// TestHelperProcess is the subprocess used by integration tests that need a
// deterministic exit code without depending on /usr/bin/true or /usr/bin/false
// (which are unavailable on Windows). When GO_VERIFIER_TEST_HELPER is set to
// "1" the process reads GO_VERIFIER_SLEEP_MS (optional sleep before exit) and
// GO_VERIFIER_EXIT, then exits with that code, and never runs any actual tests.
// All other processes ignore this block.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_VERIFIER_TEST_HELPER") != "1" {
		return
	}
	if ms, _ := strconv.Atoi(os.Getenv("GO_VERIFIER_SLEEP_MS")); ms > 0 {
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
	code, _ := strconv.Atoi(os.Getenv("GO_VERIFIER_EXIT"))
	os.Exit(code)
}

// helperCmd returns the argv + env for a subprocess that exits with code.
// The subprocess is this test binary re-invoked with TestHelperProcess as the
// sole matching test, which is a standard Go pattern (os/exec tests, etc.).
func helperCmd(code int) (cmd []string, env []string) {
	cmd = []string{os.Args[0], "-test.run=TestHelperProcess", "-test.v=false"}
	env = []string{
		"GO_VERIFIER_TEST_HELPER=1",
		fmt.Sprintf("GO_VERIFIER_EXIT=%d", code),
	}
	return
}

// helperSleepCmd returns the argv + env for a subprocess that sleeps sleepMs
// milliseconds before exiting with code. Built on helperCmd so it stays
// portable across all CI platforms (no dependency on /bin/sleep or cmd.exe).
func helperSleepCmd(sleepMs, code int) (cmd []string, env []string) {
	cmd, env = helperCmd(code)
	env = append(env, fmt.Sprintf("GO_VERIFIER_SLEEP_MS=%d", sleepMs))
	return
}

// minimalSpec returns the smallest valid TaskSpec that exercises the test
// step (no build cmd so the build path is exercised separately).
func minimalSpec() *eval.TaskSpec {
	return &eval.TaskSpec{
		TaskSpecVersion: eval.CurrentTaskSpecVersion,
		TaskID:          "kg-go-impl-test-task",
		Language:        eval.LanguageGo,
		Difficulty:      eval.DifficultyEasy,
		GeneratedFrom:   eval.GeneratedFrom{Kind: eval.KindKGTemplate},
		Prompt:          "implement the function",
		Verification:    eval.Verification{TestCmd: []string{"go", "test", "./..."}},
	}
}

// fakeRunCmd returns a runCmd seam that returns the given values on every
// call, after a configurable delay.
func fakeRunCmd(stdout, stderr string, code int, dur time.Duration, err error) func(context.Context, string, []string, []string) (string, string, int, time.Duration, error) {
	return func(_ context.Context, _ string, _ []string, _ []string) (string, string, int, time.Duration, error) {
		return stdout, stderr, code, dur, err
	}
}

// ---- New / Language -----------------------------------------------------------

func TestNew_Language(t *testing.T) {
	v := New()
	if v == nil {
		t.Fatal("New returned nil")
	}
	if got := v.Language(); got != eval.LanguageGo {
		t.Errorf("Language() = %q, want %q", got, eval.LanguageGo)
	}
}

func TestNew_UsesRealRunner(t *testing.T) {
	v := New()
	if v.runCmd == nil {
		t.Fatal("New must set runCmd to a non-nil function")
	}
}

// ---- Verify: nil spec ---------------------------------------------------------

func TestVerify_NilSpec(t *testing.T) {
	v := New()
	_, err := v.Verify(context.Background(), nil, t.TempDir(), nil)
	if err == nil {
		t.Fatal("expected error for nil spec, got nil")
	}
}

// ---- Verify: no build cmd, test passes ----------------------------------------

func TestVerify_TestPassNoBuildCmd(t *testing.T) {
	v := New()
	v.runCmd = fakeRunCmd("ok\n", "", 0, 5*time.Millisecond, nil)

	spec := minimalSpec()
	res, err := v.Verify(context.Background(), spec, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Passed {
		t.Errorf("Passed = false, want true")
	}
	if res.Phase != PhaseTest {
		t.Errorf("Phase = %q, want %q", res.Phase, PhaseTest)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", res.ExitCode)
	}
	if res.Stdout != "ok\n" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, "ok\n")
	}
}

// ---- Verify: no build cmd, test fails (non-zero exit) -------------------------

func TestVerify_TestFail(t *testing.T) {
	v := New()
	v.runCmd = fakeRunCmd("", "FAIL\n", 1, 3*time.Millisecond, nil)

	spec := minimalSpec()
	res, err := v.Verify(context.Background(), spec, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Passed {
		t.Errorf("Passed = true, want false")
	}
	if res.Phase != PhaseTest {
		t.Errorf("Phase = %q, want %q", res.Phase, PhaseTest)
	}
	if res.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", res.ExitCode)
	}
	if res.Stderr != "FAIL\n" {
		t.Errorf("Stderr = %q, want %q", res.Stderr, "FAIL\n")
	}
}

// ---- Verify: test exec error (context cancelled, binary not found) ------------

func TestVerify_TestExecError(t *testing.T) {
	execErr := errors.New("binary not found")
	v := New()
	v.runCmd = fakeRunCmd("", "", 0, 0, execErr)

	spec := minimalSpec()
	_, err := v.Verify(context.Background(), spec, t.TempDir(), nil)
	var ve *VerifyError
	if !errors.As(err, &ve) {
		t.Fatalf("expected VerifyError, got %T: %v", err, err)
	}
	if ve.Phase != PhaseTest {
		t.Errorf("VerifyError.Phase = %q, want %q", ve.Phase, PhaseTest)
	}
	if !errors.Is(err, execErr) {
		t.Errorf("errors.Is(err, execErr) = false, want true")
	}
}

// ---- Verify: build passes, test passes ----------------------------------------

func TestVerify_BuildPassTestPass(t *testing.T) {
	calls := 0
	v := New()
	v.runCmd = func(_ context.Context, _ string, _ []string, cmd []string) (string, string, int, time.Duration, error) {
		calls++
		return fmt.Sprintf("step%d\n", calls), "", 0, time.Millisecond, nil
	}

	spec := minimalSpec()
	spec.Verification.BuildCmd = []string{"go", "build", "./..."}
	res, err := v.Verify(context.Background(), spec, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("runCmd called %d times, want 2 (build + test)", calls)
	}
	if !res.Passed {
		t.Errorf("Passed = false, want true")
	}
	if res.Phase != PhaseTest {
		t.Errorf("Phase = %q, want %q", res.Phase, PhaseTest)
	}
	// Output from both steps is concatenated.
	if res.Stdout != "step1\nstep2\n" {
		t.Errorf("Stdout = %q, want %q", res.Stdout, "step1\nstep2\n")
	}
}

// ---- Verify: build fails (non-zero exit), test step skipped -------------------

func TestVerify_BuildFailShortCircuits(t *testing.T) {
	calls := 0
	v := New()
	v.runCmd = func(_ context.Context, _ string, _ []string, _ []string) (string, string, int, time.Duration, error) {
		calls++
		return "", "build error\n", 2, time.Millisecond, nil
	}

	spec := minimalSpec()
	spec.Verification.BuildCmd = []string{"go", "build", "./..."}
	res, err := v.Verify(context.Background(), spec, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("runCmd called %d times after build failure, want 1 (only build)", calls)
	}
	if res.Passed {
		t.Errorf("Passed = true, want false")
	}
	if res.Phase != PhaseBuild {
		t.Errorf("Phase = %q, want %q", res.Phase, PhaseBuild)
	}
	if res.ExitCode != 2 {
		t.Errorf("ExitCode = %d, want 2", res.ExitCode)
	}
}

// ---- Verify: build exec error -------------------------------------------------

func TestVerify_BuildExecError(t *testing.T) {
	buildErr := errors.New("toolchain not found")
	v := New()
	v.runCmd = fakeRunCmd("", "", 0, 0, buildErr)

	spec := minimalSpec()
	spec.Verification.BuildCmd = []string{"go", "build", "./..."}
	_, err := v.Verify(context.Background(), spec, t.TempDir(), nil)
	var ve *VerifyError
	if !errors.As(err, &ve) {
		t.Fatalf("expected VerifyError, got %T: %v", err, err)
	}
	if ve.Phase != PhaseBuild {
		t.Errorf("VerifyError.Phase = %q, want %q", ve.Phase, PhaseBuild)
	}
	if !errors.Is(err, buildErr) {
		t.Errorf("errors.Is(err, buildErr) = false, want true")
	}
}

// ---- Verify: timeout applied --------------------------------------------------

func TestVerify_TimeoutApplied(t *testing.T) {
	// The fake runCmd returns immediately; we just verify that a spec with
	// TimeoutSeconds=1 does not cause an error by itself (no cancellation).
	v := New()
	v.runCmd = fakeRunCmd("ok\n", "", 0, time.Millisecond, nil)

	spec := minimalSpec()
	spec.Verification.TimeoutSeconds = 1
	res, err := v.Verify(context.Background(), spec, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Passed {
		t.Errorf("Passed = false, want true")
	}
}

// TestVerify_TimeoutExpires proves that applyTimeout + exec.CommandContext
// actually cancels a running subprocess when TimeoutSeconds elapses. It uses
// the portable helperSleepCmd pattern so the test passes on Windows too.
//
// Production contract (ExitError path in runProcess): when the process is
// killed by SIGKILL/TerminateProcess the error from cmd.Run() is an
// *exec.ExitError, so runProcess returns (code!=0, nil). Verify therefore
// returns a non-nil result with Passed=false and a non-zero ExitCode — not a
// VerifyError.
func TestVerify_TimeoutExpires(t *testing.T) {
	// Subprocess sleeps 3s; the 1s timeout must fire first and kill it.
	cmd, env := helperSleepCmd(3000, 0)

	v := New()
	spec := minimalSpec()
	spec.Verification.TestCmd = cmd
	spec.Verification.TimeoutSeconds = 1

	start := time.Now()
	result, err := v.Verify(context.Background(), spec, t.TempDir(), env)

	// Must complete well before the 3-second subprocess finishes.
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Errorf("Verify took %v; 1s timeout should have cancelled the 3s subprocess far sooner", elapsed)
	}
	// Contract: killed process travels the ExitError path → Verify returns no error.
	if err != nil {
		t.Fatalf("unexpected error from Verify: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result, got nil")
	}
	if result.Passed {
		t.Error("Passed = true; want false (subprocess was killed by timeout)")
	}
	// Unix: SIGKILL → ExitCode=-1. Windows: TerminateProcess → ExitCode=1.
	// Either way, exit 0 is impossible for a killed process.
	if result.ExitCode == 0 {
		t.Errorf("ExitCode = 0; want non-zero (subprocess was killed by context timeout)")
	}
	if result.Phase != PhaseTest {
		t.Errorf("Phase = %q, want %q", result.Phase, PhaseTest)
	}
}

func TestVerify_TimeoutZeroNoDeadline(t *testing.T) {
	// TimeoutSeconds=0 must not add a deadline; the pre-cancelled context
	// is the only deadline.
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled

	v := New()
	v.runCmd = func(ctx context.Context, _ string, _ []string, _ []string) (string, string, int, time.Duration, error) {
		// The context was pre-cancelled; runCmd must propagate the error.
		if err := ctx.Err(); err != nil {
			return "", "", 0, 0, err
		}
		return "ok\n", "", 0, time.Millisecond, nil
	}

	spec := minimalSpec() // TimeoutSeconds defaults to 0
	_, err := v.Verify(ctx, spec, t.TempDir(), nil)
	// Expect a VerifyError wrapping context.Canceled.
	var ve *VerifyError
	if !errors.As(err, &ve) {
		t.Fatalf("expected VerifyError for cancelled context, got %T: %v", err, err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected errors.Is(err, context.Canceled), got %v", err)
	}
}

// ---- VerifyError: Error() and Unwrap() ----------------------------------------

func TestVerifyError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("underlying cause")
	ve := &VerifyError{Phase: PhaseBuild, Cause: cause}
	if ve.Error() == "" {
		t.Fatal("VerifyError.Error() returned empty string")
	}
	if !errors.Is(ve, cause) {
		t.Errorf("errors.Is through VerifyError should reach cause")
	}
}

func TestVerifyError_PhaseInMessage(t *testing.T) {
	for _, phase := range []Phase{PhaseBuild, PhaseTest} {
		ve := &VerifyError{Phase: phase, Cause: errors.New("x")}
		msg := ve.Error()
		if msg == "" {
			t.Errorf("VerifyError.Error() for phase %q is empty", phase)
		}
	}
}

// ---- applyTimeout -------------------------------------------------------------

func TestApplyTimeout_ZeroSec(t *testing.T) {
	ctx, cancel := applyTimeout(context.Background(), 0)
	defer cancel()
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		t.Error("expected no deadline for sec=0")
	}
}

func TestApplyTimeout_NegativeSec(t *testing.T) {
	ctx, cancel := applyTimeout(context.Background(), -1)
	defer cancel()
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		t.Error("expected no deadline for sec=-1")
	}
}

func TestApplyTimeout_PositiveSec(t *testing.T) {
	ctx, cancel := applyTimeout(context.Background(), 60)
	defer cancel()
	deadline, hasDeadline := ctx.Deadline()
	if !hasDeadline {
		t.Fatal("expected a deadline for sec=60")
	}
	remaining := time.Until(deadline)
	if remaining < 50*time.Second || remaining > 65*time.Second {
		t.Errorf("deadline remaining = %v, want ~60s", remaining)
	}
}

// ---- runProcess integration tests --------------------------------------------
//
// These tests exercise the real exec path. They use the subprocess helper
// pattern (TestHelperProcess) for a portable, toolchain-free non-zero exit.

func TestRunProcess_EmptyCmd(t *testing.T) {
	_, _, _, _, err := runProcess(context.Background(), t.TempDir(), nil, nil)
	if err == nil {
		t.Fatal("expected error for empty cmd, got nil")
	}
}

func TestRunProcess_Success(t *testing.T) {
	// "go version" is always available in the CI environment.
	stdout, stderr, code, dur, err := runProcess(context.Background(), t.TempDir(), nil, []string{"go", "version"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0; stderr=%q", code, stderr)
	}
	if stdout == "" {
		t.Error("expected non-empty stdout from go version")
	}
	if dur <= 0 {
		t.Error("expected positive duration")
	}
}

func TestRunProcess_NonZeroExit(t *testing.T) {
	cmd, env := helperCmd(42)
	_, _, code, _, err := runProcess(context.Background(), t.TempDir(), env, cmd)
	if err != nil {
		t.Fatalf("unexpected exec error: %v", err)
	}
	if code != 42 {
		t.Errorf("exit code = %d, want 42", code)
	}
}

func TestRunProcess_CommandNotFound(t *testing.T) {
	_, _, _, _, err := runProcess(context.Background(), t.TempDir(), nil, []string{"/this-binary-absolutely-does-not-exist-goverifier-test"})
	if err == nil {
		t.Fatal("expected error for non-existent binary, got nil")
	}
	// Must NOT be wrapped as a VerifyError — runProcess returns raw errors.
	var ve *VerifyError
	if errors.As(err, &ve) {
		t.Errorf("runProcess should not wrap errors in VerifyError; got %T", err)
	}
}

func TestRunProcess_ContextCancelled(t *testing.T) {
	// Launch a helper subprocess that sleeps 3s; the 100ms context deadline
	// fires first, guaranteeing cancellation well before the sleep ends.
	cmd, env := helperSleepCmd(3000, 0)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _, code, _, err := runProcess(ctx, t.TempDir(), env, cmd)

	// Must return well before the 3-second subprocess finishes.
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("runProcess took %v; 100ms context should have cancelled the 3s subprocess far sooner", elapsed)
	}
	// A killed subprocess never produces a clean success.
	// On Unix: *exec.ExitError with code -1 → runProcess returns (code=-1, nil).
	// On Windows: *exec.ExitError with code 1  → runProcess returns (code=1, nil).
	// (err==nil, code==0) is therefore impossible for a killed process.
	if err == nil && code == 0 {
		t.Error("context deadline fired but runProcess returned (code=0, err=nil) — cancellation not enforced")
	}
	if err != nil {
		// runProcess must return raw errors, never wrapped in VerifyError.
		var ve *VerifyError
		if errors.As(err, &ve) {
			t.Errorf("runProcess must not wrap errors in VerifyError; got %T", err)
		}
	}
}

// ---- Verify: Duration accumulates across steps --------------------------------

func TestVerify_DurationAccumulates(t *testing.T) {
	stepDur := 10 * time.Millisecond
	v := New()
	v.runCmd = fakeRunCmd("", "", 0, stepDur, nil)

	spec := minimalSpec()
	spec.Verification.BuildCmd = []string{"go", "build", "./..."}
	res, err := v.Verify(context.Background(), spec, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Duration < 2*stepDur {
		t.Errorf("Duration = %v, want >= %v (two steps)", res.Duration, 2*stepDur)
	}
}

// ---- Verify: Stderr accumulates across steps ----------------------------------

func TestVerify_StderrAccumulates(t *testing.T) {
	calls := 0
	v := New()
	v.runCmd = func(_ context.Context, _ string, _ []string, _ []string) (string, string, int, time.Duration, error) {
		calls++
		return "", fmt.Sprintf("warn%d\n", calls), 0, time.Millisecond, nil
	}

	spec := minimalSpec()
	spec.Verification.BuildCmd = []string{"go", "build", "./..."}
	res, err := v.Verify(context.Background(), spec, t.TempDir(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Stderr != "warn1\nwarn2\n" {
		t.Errorf("Stderr = %q, want %q", res.Stderr, "warn1\nwarn2\n")
	}
}
