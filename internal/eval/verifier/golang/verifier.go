package goverifier

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
)

// Phase identifies which verification step produced the current result.
type Phase string

const (
	// PhaseBuild is the build_cmd step; set only when build fails and the
	// test step is short-circuited.
	PhaseBuild Phase = "build"
	// PhaseTest is the test_cmd step; set on both test pass and test failure.
	PhaseTest Phase = "test"
	// PhaseValidate is the pre-flight validation step; it is set on VerifyErrors
	// returned before any command runs (empty workdir, invalid spec fields).
	PhaseValidate Phase = "validate"
)

// VerifyResult is the outcome of running a TaskSpec's verification commands
// inside a sandbox working directory. It is the canonical shape that the
// sibling verifier-python and verifier-typescript packages mirror (the R4
// TASKS.yaml note: "sequenced after verifier-go so the VerifyResult shape
// stabilizes first").
type VerifyResult struct {
	// Passed is true only when all verification steps exit with code 0.
	Passed bool
	// Phase is the last step that ran: PhaseBuild on build short-circuit or
	// PhaseTest on full pass or test failure.
	Phase Phase
	// ExitCode is the exit code of the last step that ran.
	ExitCode int
	// Stdout is the combined standard output of all steps that ran.
	Stdout string
	// Stderr is the combined standard error of all steps that ran.
	Stderr string
	// Duration is the total elapsed wall time of all verification steps.
	Duration time.Duration
}

// VerifyError wraps a non-exit-code failure from a verification step —
// context cancellation, command not found, or other OS-level start failure.
// Callers should use errors.As to distinguish this from a clean non-zero
// exit (which is encoded in [VerifyResult] without an error return).
type VerifyError struct {
	// Phase is the step that could not start.
	Phase Phase
	// Cause is the underlying exec or context error.
	Cause error
}

// Error implements the error interface.
func (e *VerifyError) Error() string {
	return fmt.Sprintf("goverifier: %s: %v", e.Phase, e.Cause)
}

// Unwrap returns the underlying cause so errors.Is / errors.As traversal
// passes through VerifyError transparently.
func (e *VerifyError) Unwrap() error { return e.Cause }

// Verifier runs the verification commands from an eval.TaskSpec inside a
// sandbox working directory and returns a VerifyResult. It is the seam the
// R4 harness driver uses; per-language adapters sit behind this interface so
// the harness is language-agnostic.
type Verifier interface {
	// Language reports the language this verifier handles.
	Language() eval.Language
	// Verify runs the TaskSpec's verification commands in workdir with env
	// appended to the host environment. A non-zero exit populates the
	// returned VerifyResult (Passed=false); a VerifyError is returned only
	// when a step could not start.
	Verify(ctx context.Context, spec *eval.TaskSpec, workdir string, env []string) (*VerifyResult, error)
}

// GoVerifier implements [Verifier] for Go tasks. It runs build_cmd then
// test_cmd from the TaskSpec, short-circuiting on build failure. The
// TimeoutSeconds field is applied as a context deadline when non-zero; no
// extra deadline is applied when it is zero.
type GoVerifier struct {
	// runCmd is the seam for executing a single command. Tests inject a
	// deterministic fake; production code uses runProcess.
	runCmd func(ctx context.Context, workdir string, env []string, cmd []string) (stdout, stderr string, exitCode int, dur time.Duration, err error)
}

// Compile-time assertion.
var _ Verifier = (*GoVerifier)(nil)

// New returns a GoVerifier backed by the real process runner.
func New() *GoVerifier {
	v := &GoVerifier{}
	v.runCmd = runProcess
	return v
}

// Language implements Verifier.
func (v *GoVerifier) Language() eval.Language { return eval.LanguageGo }

// Verify implements Verifier. A nil spec returns an immediate error. An empty
// workdir is rejected with a VerifyError (PhaseValidate) before any command
// runs — an empty exec.Cmd.Dir would default to the current process directory,
// escaping the sandbox. A negative TimeoutSeconds is also rejected; 0 means no
// timeout (documented contract). TimeoutSeconds, when positive, becomes a
// context deadline spanning both build and test. A build failure short-circuits
// the test step.
func (v *GoVerifier) Verify(ctx context.Context, spec *eval.TaskSpec, workdir string, env []string) (*VerifyResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("goverifier: spec is required")
	}
	if workdir == "" {
		return nil, &VerifyError{Phase: PhaseValidate, Cause: fmt.Errorf("workdir is required")}
	}
	if spec.Verification.TimeoutSeconds < 0 {
		return nil, &VerifyError{Phase: PhaseValidate, Cause: fmt.Errorf("TimeoutSeconds must be >= 0, got %d", spec.Verification.TimeoutSeconds)}
	}
	ctx, cancel := applyTimeout(ctx, spec.Verification.TimeoutSeconds)
	defer cancel()

	var result VerifyResult

	if len(spec.Verification.BuildCmd) > 0 {
		stop, err := v.runStep(ctx, workdir, env, spec.Verification.BuildCmd, PhaseBuild, &result)
		if err != nil {
			return nil, err
		}
		if stop {
			return &result, nil
		}
	}

	if _, err := v.runStep(ctx, workdir, env, spec.Verification.TestCmd, PhaseTest, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// runStep executes one verification command, appends its output to result,
// and reports whether further steps should stop. It returns (true, err) when
// the step could not start (exec-level failure), (true, nil) when the step
// exited non-zero (short-circuit caller), or (false, nil) to continue.
func (v *GoVerifier) runStep(
	ctx context.Context,
	workdir string,
	env []string,
	cmd []string,
	phase Phase,
	result *VerifyResult,
) (stop bool, err error) {
	out, errOut, code, dur, runErr := v.runCmd(ctx, workdir, env, cmd)
	result.Stdout += out
	result.Stderr += errOut
	result.Duration += dur
	result.Phase = phase
	if runErr != nil {
		return true, &VerifyError{Phase: phase, Cause: runErr}
	}
	result.ExitCode = code
	result.Passed = code == 0
	return code != 0, nil
}

// applyTimeout wraps ctx with a deadline of sec seconds when sec > 0.
// The returned cancel must always be called (the caller is responsible).
func applyTimeout(ctx context.Context, sec int) (context.Context, context.CancelFunc) {
	if sec <= 0 {
		return ctx, func() { /* no deadline set: nothing to cancel */ }
	}
	return context.WithTimeout(ctx, time.Duration(sec)*time.Second)
}

// runProcess executes cmd in workdir with env appended to the host
// environment, capturing stdout and stderr separately. A non-zero exit is
// returned as an exitCode (not as an error); only exec-level failures
// — context cancellation before start, binary not found — return a non-nil
// error. The caller interprets exit codes.
func runProcess(ctx context.Context, workdir string, env []string, cmd []string) (stdout, stderr string, exitCode int, dur time.Duration, err error) {
	if len(cmd) == 0 {
		return "", "", 0, 0, fmt.Errorf("goverifier: empty command")
	}
	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...) //nolint:gosec // cmd comes from a TaskSpec authored by the harness
	c.Dir = workdir
	c.Env = append(os.Environ(), env...)
	var outBuf, errBuf bytes.Buffer
	c.Stdout = &outBuf
	c.Stderr = &errBuf
	start := time.Now()
	runErr := c.Run()
	dur = time.Since(start)
	stdout = outBuf.String()
	stderr = errBuf.String()
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			return stdout, stderr, exitErr.ExitCode(), dur, nil
		}
		return stdout, stderr, 0, dur, runErr
	}
	return stdout, stderr, 0, dur, nil
}
