package verifier

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

// BaseVerifier is the language-agnostic run engine shared by every per-language
// verifier. It runs build_cmd then test_cmd from a TaskSpec, short-circuiting on
// build failure. The TimeoutSeconds field is applied as a context deadline when
// non-zero; no extra deadline is applied when it is zero. Per-language adapters
// embed *BaseVerifier and contribute only their Language() identity via NewBase,
// so the run loop lives here once instead of being duplicated per language.
type BaseVerifier struct {
	// lang is the language this verifier reports via Language().
	lang eval.Language
	// runCmd is the seam for executing a single command. Tests inject a
	// deterministic fake; production code uses runProcess.
	runCmd func(ctx context.Context, workdir string, env []string, cmd []string) (stdout, stderr string, exitCode int, dur time.Duration, err error)
	// lookPath resolves toolchain binaries on PATH for the pre-flight. The seam
	// defaults to exec.LookPath and is injected by tests so the toolchain-missing
	// path can be exercised without mutating the process PATH.
	lookPath lookPathFn
}

// Compile-time assertion that the engine satisfies the shared contract.
var _ Verifier = (*BaseVerifier)(nil)

// NewBase returns a BaseVerifier for lang backed by the real process runner and
// exec.LookPath as the toolchain resolver.
func NewBase(lang eval.Language) *BaseVerifier {
	return &BaseVerifier{lang: lang, runCmd: runProcess, lookPath: exec.LookPath}
}

// Language implements Verifier.
func (b *BaseVerifier) Language() eval.Language { return b.lang }

// Verify implements Verifier. A nil spec returns an immediate error.
// An empty workdir is rejected with a VerifyError (PhaseValidate) before any
// command runs — an empty exec.Cmd.Dir would default to the current process
// directory, escaping the sandbox. A negative TimeoutSeconds is also rejected;
// 0 means no timeout (documented contract). TimeoutSeconds, when positive,
// becomes a context deadline spanning both build and test. A build failure
// short-circuits the test step.
func (b *BaseVerifier) Verify(ctx context.Context, spec *eval.TaskSpec, workdir string, env []string) (*VerifyResult, error) {
	if spec == nil {
		return nil, fmt.Errorf("verifier: spec is required")
	}
	if workdir == "" {
		return nil, &VerifyError{Phase: PhaseValidate, Cause: fmt.Errorf("workdir is required")}
	}
	if spec.Verification.TimeoutSeconds < 0 {
		return nil, &VerifyError{Phase: PhaseValidate, Cause: fmt.Errorf("TimeoutSeconds must be >= 0, got %d", spec.Verification.TimeoutSeconds)}
	}

	// Toolchain pre-flight: resolve the interpreter/compiler each command needs
	// BEFORE running anything. A missing toolchain fails fast with a distinct
	// *ToolchainError (not a VerifyError) carrying a clear, actionable message,
	// so "the python interpreter is absent" is never mistaken for "the agent's
	// code failed its tests". The resolved commands carry the actual binary
	// (e.g. python3 in place of python) into execution.
	buildCmd, testCmd, err := b.preflight(spec.Verification.BuildCmd, spec.Verification.TestCmd)
	if err != nil {
		return nil, err
	}

	ctx, cancel := applyTimeout(ctx, spec.Verification.TimeoutSeconds)
	defer cancel()

	var result VerifyResult

	if len(buildCmd) > 0 {
		stop, berr := b.runStep(ctx, workdir, env, buildCmd, PhaseBuild, &result)
		if berr != nil {
			return nil, berr
		}
		if stop {
			return &result, nil
		}
	}

	if _, terr := b.runStep(ctx, workdir, env, testCmd, PhaseTest, &result); terr != nil {
		return nil, terr
	}
	return &result, nil
}

// preflight resolves the toolchain for the build (when present) and test
// commands ahead of execution, returning the resolved argv for each with the
// leading token(s) rewritten to the first available candidate. It returns a
// *ToolchainError when a required interpreter/compiler is absent, before any
// command runs.
func (b *BaseVerifier) preflight(build, test []string) (resolvedBuild, resolvedTest []string, err error) {
	if len(build) > 0 {
		resolvedBuild, err = resolveToolchain(b.lang, build, b.lookPath)
		if err != nil {
			return nil, nil, err
		}
	}
	resolvedTest, err = resolveToolchain(b.lang, test, b.lookPath)
	if err != nil {
		return nil, nil, err
	}
	return resolvedBuild, resolvedTest, nil
}

// runStep executes one verification command, appends its output to result,
// and reports whether further steps should stop. It returns (true, err) when
// the step could not start (exec-level failure), (true, nil) when the step
// exited non-zero (short-circuit caller), or (false, nil) to continue.
func (b *BaseVerifier) runStep(
	ctx context.Context,
	workdir string,
	env []string,
	cmd []string,
	phase Phase,
	result *VerifyResult,
) (stop bool, err error) {
	out, errOut, code, dur, runErr := b.runCmd(ctx, workdir, env, cmd)
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
		return "", "", 0, 0, fmt.Errorf("verifier: empty command")
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
