package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// copilotBin and copilotSubCmd are the GitHub CLI binary and the subcommand
// used to invoke GitHub Copilot in non-interactive code-suggestion mode.
const (
	copilotBin    = "gh"
	copilotSubCmd = "copilot"
)

// copilotRunner invokes the GitHub Copilot CLI (via `gh copilot suggest`)
// against a sandbox workdir. After the run it recovers token telemetry from the
// Copilot CLI's on-disk session store via the platform copilot scanner (see
// the run field docs).
type copilotRunner struct {
	// run is the exec seam; production code uses realExec, tests inject a fake.
	run cmdFn
	// scan is the token-scanner seam wired to platform copilot's
	// ScanSessionTokens, which walks <home>/.copilot/session-state/*/
	// events.jsonl for session.shutdown token totals by mtime. It is a struct
	// field (not a package var) so tests inject a deterministic fake without
	// touching a real ~/.copilot. The store is populated by the Copilot CLI;
	// when a run writes none (e.g. gh copilot suggest short-circuits), the scan
	// returns empty and Telemetry.Tokens stays nil — a first-class absent
	// signal, not a silent drop.
	scan scanFn
}

func newCopilotRunner() *copilotRunner {
	return &copilotRunner{run: realExec, scan: asScanner(platform.NewCopilot())}
}

var _ Runner = (*copilotRunner)(nil)

// Run implements Runner. It invokes `gh copilot suggest -t code -y <prompt>` in
// instance.Workdir with instance.Env appended to the process environment, then
// scans the scratch HOME's Copilot session store for token telemetry emitted
// after the run started.
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
	after := start.UTC().Format(time.RFC3339)
	stdout, stderr, code, err := r.run(ctx, copilotBin, args, instance.Workdir, env)
	dur := time.Since(start)
	if err != nil {
		return Result{}, fmt.Errorf("runner/copilot: exec: %w", err)
	}

	// Copilot publishes no session-id env var, so the scanner filters by
	// events.jsonl mtime > after; the fresh scratch HOME scopes the walk to
	// this run alone.
	home := scratchHomeFromEnv(instance.Env)
	tokens := tokensFromMetrics(r.scan(home, "", "", after))

	return Result{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: code,
		Duration: dur,
		Telemetry: AgentTelemetry{
			Harness: "gh-copilot",
			Tokens:  tokens,
		},
	}, nil
}
