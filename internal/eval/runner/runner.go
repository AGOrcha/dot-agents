package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
	"github.com/AGOrcha/dot-agents/internal/scoring"
)

// Adapter names one of the supported agent platforms.
type Adapter string

// v1 adapter set (OQ1 ruling: claude, codex, and copilot ship; others deferred).
const (
	AdapterClaude  Adapter = "claude"
	AdapterCodex   Adapter = "codex"
	AdapterCopilot Adapter = "copilot"
)

// ErrUnknownAdapter is returned by New when the adapter name is not
// recognised.
var ErrUnknownAdapter = errors.New("runner: unknown adapter")

// AgentTelemetry is the canonical agent-runner identity and usage telemetry
// for one eval run (R4 spec requirement R9). All fields are optional; what is
// present flows into the scoringbridge's iteration record so the
// platform-tuner persona can diff platforms.
//
// Scoringbridge defines its own interim AgentTelemetry shape; a follow-up
// should switch it to import this type. Until then the harness driver maps
// between them at the bridge call site (the shapes are structurally
// identical).
type AgentTelemetry struct {
	// SessionID is the platform session UUID when the runner captured one.
	SessionID string
	// Harness is the agent platform identifier (claude-code, codex, ...).
	Harness string
	// Model is the model ID the run used; populated when the CLI reports it.
	Model string
	// Retries is how many times the runner had to re-invoke the agent.
	Retries int
	// Tokens is the run's token usage; nil when the runner captured none.
	// Absent token data is first-class — the rubric renormalizes.
	Tokens *scoring.TokenUsage
}

// Result is the complete output of one agent run.
type Result struct {
	// Stdout is the captured standard output of the agent process.
	Stdout []byte
	// Stderr is the captured standard error of the agent process.
	Stderr []byte
	// ExitCode is the process exit code (0 = success).
	ExitCode int
	// Duration is wall-clock time from exec start to process exit.
	Duration time.Duration
	// Telemetry holds the identity and usage data the adapter extracted.
	Telemetry AgentTelemetry
}

// Runner runs an agent against a TaskSpec inside a provisioned sandbox and
// returns the captured output and telemetry.
type Runner interface {
	// Run invokes the agent with the task prompt inside instance.Workdir,
	// appending instance.Env to the subprocess environment. The returned
	// Result carries stdout, stderr, exit code, wall time, and any telemetry
	// the adapter could extract. A non-zero exit code is NOT itself a Go
	// error — it is encoded in Result.ExitCode so the harness can
	// distinguish "the agent ran but produced wrong output" from "we could
	// not launch the agent at all".
	Run(ctx context.Context, spec *eval.TaskSpec, instance *sandbox.Instance) (Result, error)
}

// New returns a Runner for the named adapter. Unrecognised adapters return
// ErrUnknownAdapter.
func New(adapter Adapter) (Runner, error) {
	switch adapter {
	case AdapterClaude:
		return newClaudeRunner(), nil
	case AdapterCodex:
		return newCodexRunner(), nil
	case AdapterCopilot:
		return newCopilotRunner(), nil
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownAdapter, adapter)
	}
}

// cmdFn is the exec seam used by every adapter. The production implementation
// is realExec; tests replace it with a deterministic fake via the adapter
// struct's run field, avoiding shared state and keeping swaps race-free.
type cmdFn func(
	ctx context.Context,
	name string,
	args []string,
	dir string,
	env []string,
) (stdout []byte, stderr []byte, exitCode int, err error)

// realExec is the production cmdFn. It builds an exec.Cmd, sets Dir and Env,
// runs it, and collects stdout/stderr.
//
// Two exit conditions are deliberately kept distinct:
//
//   - Context cancel/timeout (infrastructure failure). If ctx ends during the
//     run, the process is killed and cmd.Run reports a "signal: killed"
//     *exec.ExitError that is indistinguishable from a genuine agent
//     non-zero exit. realExec checks ctx.Err() FIRST and returns it wrapped,
//     so callers see context.Canceled / context.DeadlineExceeded via
//     errors.Is and treat it as infra failure, not an agent result.
//   - Agent non-zero exit (a completed run). Reported in exitCode with a nil
//     error, so the harness scores it as "the agent ran but the output was
//     wrong".
func realExec(
	ctx context.Context,
	name string,
	args []string,
	dir string,
	env []string,
) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	// Drive stdin from an empty reader so no adapter can hang waiting on
	// interactive TTY input (codex exec in particular reads stdin).
	cmd.Stdin = bytes.NewReader(nil)
	// WaitDelay unblocks pipe-copy goroutines after context cancellation,
	// matching the cliprobe.go pattern for bounded subprocess execution.
	cmd.WaitDelay = 5 * time.Second

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	if ctxErr := ctx.Err(); ctxErr != nil {
		// The context cancelled/timed out and killed the process mid-run: an
		// infrastructure failure, NOT a normal agent non-zero exit.
		return outBuf.Bytes(), errBuf.Bytes(), -1,
			fmt.Errorf("runner: context ended during exec: %w", ctxErr)
	}

	code := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			code = exitErr.ExitCode()
			runErr = nil // non-zero exit is a Result, not a Go error
		}
	}
	return outBuf.Bytes(), errBuf.Bytes(), code, runErr
}
