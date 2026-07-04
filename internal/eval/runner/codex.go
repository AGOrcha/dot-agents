package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
)

// codexBin is the Codex CLI binary name resolved via PATH; codexExecSub is its
// headless run subcommand (`codex exec <prompt>`, arg-delivered prompt) — the
// repo's canonical non-interactive invocation (see the cross-harness reviewer
// dispatch table in prompts/reviewers/cross-harness-adversarial.md). The
// read-only `-s read-only` sandbox flag is deliberately NOT set: this is a
// code-writing task, not the read-only reviewer-dispatch pattern.
const (
	codexBin     = "codex"
	codexExecSub = "exec"
	codexHarness = "codex"
)

// codexRunner invokes the OpenAI Codex CLI against a sandbox workdir.
//
// Token telemetry gap (documented, not silent): Codex DOES persist per-turn
// token counts — the platform layer scans them via codexScanSessionTokens over
// ~/.codex/sessions/YYYY/MM/DD/*-<sessionID>.jsonl. That scanner is keyed by
// the Codex session id, and `codex exec`'s plain (non-JSON) stdout does not
// surface one, so this package cannot locate the session file without
// reimplementing Codex's session-dir layout. v1 therefore leaves
// Telemetry.Tokens nil for codex; the follow-up is to invoke `codex exec
// --json` and capture the session id from its session-configured event (or to
// expose an id-less, mtime-scoped scan on the platform codex type), after
// which the same scanFn seam the claude/copilot adapters use can be wired here.
type codexRunner struct {
	// run is the exec seam; production code uses realExec, tests inject a fake.
	run cmdFn
}

func newCodexRunner() *codexRunner {
	return &codexRunner{run: realExec}
}

var _ Runner = (*codexRunner)(nil)

// Run implements Runner. It invokes `codex exec <prompt>` in instance.Workdir
// with instance.Env appended to the process environment. stdout/stderr are
// captured verbatim. See the type doc for why token telemetry is a documented
// v1 gap rather than a wired scan.
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
	args := []string{codexExecSub, spec.Prompt}

	start := time.Now()
	stdout, stderr, code, err := r.run(ctx, codexBin, args, instance.Workdir, env)
	dur := time.Since(start)
	if err != nil {
		if se := classifyExecError(codexHarness, codexBin, err); se != nil {
			return Result{}, se
		}
		return Result{}, fmt.Errorf("runner/codex: exec: %w", err)
	}

	return Result{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: code,
		Duration: dur,
		Telemetry: AgentTelemetry{
			Harness: codexHarness,
			// Tokens is nil — see the codexRunner type doc for the documented
			// session-id gap. The rubric renormalises over the absent signal.
		},
	}, nil
}
