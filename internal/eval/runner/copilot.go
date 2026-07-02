package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
)

// copilotBin and copilotSubCmd are the GitHub CLI binary and the subcommand
// used to invoke GitHub Copilot in non-interactive code-suggestion mode.
const (
	copilotBin    = "gh"
	copilotSubCmd = "copilot"
)

// copilotRunner invokes the GitHub Copilot CLI (via `gh copilot suggest`)
// against a sandbox workdir. Like the Codex adapter, token counts are not
// available from the `gh copilot` CLI surface; Telemetry.Tokens is nil.
type copilotRunner struct {
	// run is the exec seam; production code uses realExec, tests inject a fake.
	run cmdFn
}

func newCopilotRunner() *copilotRunner {
	return &copilotRunner{run: realExec}
}

var _ Runner = (*copilotRunner)(nil)

// Run implements Runner. It invokes `gh copilot suggest -t code <prompt>` in
// instance.Workdir with instance.Env appended to the process environment.
// The `-t code` flag requests code-oriented suggestions. stdout/stderr are
// captured verbatim; token telemetry is absent in v1.
func (r *copilotRunner) Run(
	ctx context.Context,
	spec *eval.TaskSpec,
	instance *sandbox.Instance,
) (Result, error) {
	if spec == nil {
		return Result{}, fmt.Errorf("runner/copilot: task spec is nil")
	}
	if instance == nil {
		return Result{}, fmt.Errorf("runner/copilot: sandbox instance is nil")
	}

	env := buildEnv(instance.Env)
	// `gh copilot suggest -t code <prompt>` requests code-generation
	// suggestions. The -y flag accepts the first suggestion non-interactively
	// so the subprocess does not block waiting for TTY input.
	args := []string{copilotSubCmd, "suggest", "-t", "code", "-y", spec.Prompt}

	start := time.Now()
	stdout, stderr, code, err := r.run(ctx, copilotBin, args, instance.Workdir, env)
	dur := time.Since(start)
	if err != nil {
		return Result{}, fmt.Errorf("runner/copilot: exec: %w", err)
	}

	return Result{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: code,
		Duration: dur,
		Telemetry: AgentTelemetry{
			Harness: "gh-copilot",
			// Tokens is nil: gh copilot CLI does not expose machine-readable
			// usage data; the rubric renormalises over absent signals.
		},
	}, nil
}
