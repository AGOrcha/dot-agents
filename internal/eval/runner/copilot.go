package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// copilotBin and copilotPromptFlag are the standalone GitHub Copilot CLI binary
// and its headless code-generation prompt flag. `copilot -p <prompt>` is the
// repo's canonical non-interactive code-gen invocation (cross-harness reviewer
// dispatch table, prompts/reviewers/cross-harness-adversarial.md:56;
// skill-architect providers.md:43). This is deliberately NOT the `gh copilot
// suggest` gh-extension, which targets shell-COMMAND suggestions rather than
// code generation. The invocation stays behind the exec seam, so if the
// Copilot CLI flag changes it is a one-line swap here with no caller impact.
const (
	copilotBin        = "copilot"
	copilotPromptFlag = "-p"
	copilotHarness    = "gh-copilot"
)

// copilotRunner invokes the standalone GitHub Copilot CLI against a sandbox
// workdir. After the run it recovers token telemetry from the Copilot CLI's
// on-disk session store via the platform copilot scanner (see the scan field
// docs).
type copilotRunner struct {
	// run is the exec seam; production code uses realExec, tests inject a fake.
	run cmdFn
	// scan is the token-scanner seam wired to platform copilot's
	// ScanSessionTokens, which walks <home>/.copilot/session-state/*/
	// events.jsonl for session.shutdown token totals by mtime. It is a struct
	// field (not a package var) so tests inject a deterministic fake without
	// touching a real ~/.copilot. The store is populated by the standalone
	// Copilot CLI this adapter now invokes; when a run writes none, the scan
	// returns empty and Telemetry.Tokens stays nil — a first-class absent
	// signal, not a silent drop.
	scan scanFn
}

func newCopilotRunner() *copilotRunner {
	return &copilotRunner{run: realExec, scan: asScanner(platform.NewCopilot())}
}

var _ Runner = (*copilotRunner)(nil)

// Run implements Runner. It invokes `copilot -p <prompt>` in instance.Workdir
// with instance.Env appended to the process environment, then scans the
// scratch HOME's Copilot session store for token telemetry emitted after the
// run started.
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
	// `copilot -p <prompt>` — v1 best-effort headless code-gen invocation
	// (cross-harness-adversarial.md:56). The prompt is arg-delivered; realExec
	// pins stdin to an empty reader so the CLI cannot block on TTY input.
	args := []string{copilotPromptFlag, spec.Prompt}

	start := time.Now()
	after := start.UTC().Format(time.RFC3339)
	stdout, stderr, code, err := r.run(ctx, copilotBin, args, instance.Workdir, env)
	dur := time.Since(start)
	if err != nil {
		if se := classifyExecError(copilotHarness, copilotBin, err); se != nil {
			return Result{}, se
		}
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
			Harness: copilotHarness,
			Tokens:  tokens,
		},
	}, nil
}
