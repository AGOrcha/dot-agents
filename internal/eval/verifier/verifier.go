package verifier

import (
	"context"
	"fmt"
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
	return fmt.Sprintf("verifier: %s: %v", e.Phase, e.Cause)
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
