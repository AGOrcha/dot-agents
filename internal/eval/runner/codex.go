package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
)

// codexBin is the Codex CLI binary name resolved via PATH.
const codexBin = "codex"

// codexRunner invokes the OpenAI Codex CLI against a sandbox workdir.
// The Codex CLI does not expose machine-readable token counts in its v1 CLI
// surface, so Telemetry.Tokens is always nil; the rubric renormalises over
// the absent signal.
type codexRunner struct {
	// run is the exec seam; production code uses realExec, tests inject a fake.
	run cmdFn
}

func newCodexRunner() *codexRunner {
	return &codexRunner{run: realExec}
}

var _ Runner = (*codexRunner)(nil)

// Run implements Runner. It invokes `codex <prompt>` in instance.Workdir with
// instance.Env appended to the process environment. stdout/stderr are
// captured verbatim; token telemetry is absent in v1 (Codex CLI does not
// emit machine-readable usage data).
func (r *codexRunner) Run(
	ctx context.Context,
	spec *eval.TaskSpec,
	instance *sandbox.Instance,
) (Result, error) {
	if spec == nil {
		return Result{}, fmt.Errorf("runner/codex: task spec is nil")
	}
	if instance == nil {
		return Result{}, fmt.Errorf("runner/codex: sandbox instance is nil")
	}

	env := buildEnv(instance.Env)
	args := []string{spec.Prompt}

	start := time.Now()
	stdout, stderr, code, err := r.run(ctx, codexBin, args, instance.Workdir, env)
	dur := time.Since(start)
	if err != nil {
		return Result{}, fmt.Errorf("runner/codex: exec: %w", err)
	}

	return Result{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: code,
		Duration: dur,
		Telemetry: AgentTelemetry{
			Harness: "codex",
			// Tokens is nil: Codex CLI does not surface machine-readable usage
			// in v1; the rubric treats absent token data as a renormalisable
			// signal rather than a zero score.
		},
	}, nil
}
