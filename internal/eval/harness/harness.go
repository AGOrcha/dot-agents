package harness

import (
	"context"
	"errors"
	"fmt"

	"github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/runner"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
	"github.com/AGOrcha/dot-agents/internal/eval/scoringbridge"
	"github.com/AGOrcha/dot-agents/internal/eval/verifier"
)

// Config carries the seam dependencies a Harness orchestrates. Every field is
// required; New rejects a nil dependency so a mis-wired harness fails at
// construction rather than mid-run.
type Config struct {
	// Generators resolves a language to its task generator (the generate
	// stage). The harness only ever reads it via Lookup.
	Generators *eval.Registry
	// Sandbox provisions the isolated working tree each run executes in (the
	// provision stage).
	Sandbox sandbox.Sandbox
	// Runner invokes the agent inside a provisioned sandbox (the run stage).
	Runner runner.Runner
	// Verifiers maps a language to the verifier that runs its build/test
	// commands (the verify stage). At least one entry is required.
	Verifiers map[eval.Language]verifier.Verifier
}

// Harness sequences the five eval stages behind their seam interfaces. It is
// safe to reuse across runs (it holds no per-run mutable state); concurrency
// safety is inherited from the injected seams.
type Harness struct {
	generators *eval.Registry
	sandbox    sandbox.Sandbox
	runner     runner.Runner
	verifiers  map[eval.Language]verifier.Verifier

	// producedSolution reports whether the agent produced a solution in the
	// sandbox working tree (any tracked edit or untracked file). It gates the
	// agent auth-failure detection: a run whose working tree changed is scored,
	// never auth-aborted, regardless of its output text. The seam defaults to
	// detectWorktreeChanges and is injected by tests.
	producedSolution func(workdir string) bool
}

// New validates cfg and returns a Harness. It errors when any dependency is
// absent so wiring bugs surface immediately.
func New(cfg Config) (*Harness, error) {
	if cfg.Generators == nil {
		return nil, fmt.Errorf("harness: generators registry is required")
	}
	if cfg.Sandbox == nil {
		return nil, fmt.Errorf("harness: sandbox is required")
	}
	if cfg.Runner == nil {
		return nil, fmt.Errorf("harness: runner is required")
	}
	if len(cfg.Verifiers) == 0 {
		return nil, fmt.Errorf("harness: at least one verifier is required")
	}
	for lang, v := range cfg.Verifiers {
		if v == nil {
			return nil, fmt.Errorf("harness: verifier for language %q is nil", lang)
		}
	}
	return &Harness{
		generators:       cfg.Generators,
		sandbox:          cfg.Sandbox,
		runner:           cfg.Runner,
		verifiers:        cfg.Verifiers,
		producedSolution: detectWorktreeChanges,
	}, nil
}

// Options are the per-run inputs Run threads into the generate stage.
type Options struct {
	// Language selects both the generator and the verifier for the run.
	Language eval.Language
	// Difficulty optionally constrains the generated task's band; the zero
	// value lets the generator choose.
	Difficulty eval.Difficulty
	// TemplateID optionally selects a specific generator template; the zero
	// value lets the generator pick its default.
	TemplateID string
}

// EvalRun aggregates the output of every stage of one [Harness.Run]. It is the
// harness's result — distinct from the scoringbridge.EvalRun that Run assembles
// internally as the score stage's input.
type EvalRun struct {
	// Spec is the TaskSpec the generate stage produced (stage 1).
	Spec *eval.TaskSpec
	// RunID, RunDir, and BaseCommit identify the provisioned sandbox instance
	// (stage 2) and pin the run for reproducibility.
	RunID      string
	RunDir     string
	BaseCommit string
	// Run is the agent runner's output and telemetry (stage 3).
	Run runner.Result
	// Verify is the verifier's full build/test outcome (stage 4).
	Verify *verifier.VerifyResult
	// Score is everything the scoring bridge persisted and computed (stage 5).
	Score scoringbridge.Result
}

// Run sequences the five eval stages for one task and returns the aggregated
// EvalRun. A stage's infrastructure failure aborts the run and is returned as
// an error; a completed-but-failing agent or verifier outcome is not an error —
// it flows into the score.
//
// The provisioned sandbox instance is always cleaned up before Run returns. A
// Cleanup failure is not swallowed: on an otherwise-successful run it turns the
// run into an error (spec R8 — a run must not silently leak working trees),
// while the returned EvalRun stays fully populated (the score was already
// computed and persisted). A cleanup failure never masks a primary-stage error;
// that root cause wins.
func (h *Harness) Run(ctx context.Context, opts Options) (result EvalRun, err error) {
	if cerr := ctx.Err(); cerr != nil {
		return EvalRun{}, fmt.Errorf("harness: run canceled: %w", cerr)
	}

	spec, err := h.generate(ctx, opts)
	if err != nil {
		return EvalRun{}, err
	}

	instance, err := h.sandbox.Provision(ctx, spec)
	if err != nil {
		return EvalRun{}, fmt.Errorf("harness: provision sandbox: %w", err)
	}
	defer func() { err = finalizeCleanup(instance.Cleanup(), err) }()

	agentResult, err := h.runAgent(ctx, spec, instance)
	if err != nil {
		return EvalRun{}, err
	}

	verifyResult, err := h.verify(ctx, spec, instance)
	if err != nil {
		return EvalRun{}, err
	}

	score, err := h.score(spec, instance, agentResult, verifyResult)
	if err != nil {
		return EvalRun{}, err
	}

	return EvalRun{
		Spec:       spec,
		RunID:      instance.RunID,
		RunDir:     instance.RunDir,
		BaseCommit: instance.BaseCommit,
		Run:        agentResult,
		Verify:     verifyResult,
		Score:      score,
	}, nil
}

// finalizeCleanup folds a sandbox Cleanup() error into the run's outcome. A
// primary-stage error always wins (the cleanup failure must not mask the root
// cause). Otherwise a cleanup failure turns an otherwise-successful run into an
// error so a leaked working tree is reported rather than swallowed (spec R8).
func finalizeCleanup(cleanupErr, runErr error) error {
	if runErr != nil {
		return runErr
	}
	if cleanupErr != nil {
		return fmt.Errorf("harness: cleanup sandbox: %w", cleanupErr)
	}
	return nil
}

// generate resolves the language's generator and synthesises a TaskSpec. A
// missing generator is a wiring error; a generator failure is surfaced as-is.
func (h *Harness) generate(ctx context.Context, opts Options) (*eval.TaskSpec, error) {
	gen, ok := h.generators.Lookup(opts.Language)
	if !ok {
		return nil, fmt.Errorf("harness: no generator registered for language %q", opts.Language)
	}
	spec, err := gen.Generate(ctx, eval.GenerateOptions{
		Difficulty: opts.Difficulty,
		TemplateID: opts.TemplateID,
	})
	if err != nil {
		return nil, fmt.Errorf("harness: generate task: %w", err)
	}
	return spec, nil
}

// runAgent invokes the agent runner inside the provisioned sandbox. A non-zero
// agent exit code is recorded in the returned Result, not surfaced as an error;
// only a launch failure (non-nil error) stops the run. An *AgentStartError (the
// CLI was absent, or it failed to authenticate under the sandbox's isolated
// HOME) is wrapped with a distinct "agent did not run" prefix so the operator
// sees an environment/credential problem rather than a poor model-quality score
// (dogfood #10).
func (h *Harness) runAgent(ctx context.Context, spec *eval.TaskSpec, instance *sandbox.Instance) (runner.Result, error) {
	result, err := h.runner.Run(ctx, spec, instance)
	if err != nil {
		var startErr *runner.AgentStartError
		if errors.As(err, &startErr) {
			return runner.Result{}, fmt.Errorf("harness: agent did not run: %w", err)
		}
		return runner.Result{}, fmt.Errorf("harness: run agent: %w", err)
	}
	if authErr := h.detectAuthFailure(instance, result); authErr != nil {
		return runner.Result{}, fmt.Errorf("harness: agent did not run: %w", authErr)
	}
	return result, nil
}

// verify runs the language verifier over the sandbox workdir. A failing build
// or test is encoded in the returned VerifyResult (Passed=false), not an error;
// only a verifier step that could not start (a VerifyError) stops the run.
func (h *Harness) verify(ctx context.Context, spec *eval.TaskSpec, instance *sandbox.Instance) (*verifier.VerifyResult, error) {
	v, ok := h.verifiers[spec.Language]
	if !ok {
		return nil, fmt.Errorf("harness: no verifier registered for language %q", spec.Language)
	}
	result, err := v.Verify(ctx, spec, instance.Workdir, instance.Env)
	if err != nil {
		var tcErr *verifier.ToolchainError
		if errors.As(err, &tcErr) {
			// A missing interpreter/compiler is an environment fault, not a
			// verification failure — surface it distinctly so it is not read as
			// the agent's code failing its tests.
			return nil, fmt.Errorf("harness: toolchain unavailable: %w", err)
		}
		return nil, fmt.Errorf("harness: verify: %w", err)
	}
	return result, nil
}

// score bridges the completed run into R1: it assembles the scoringbridge input
// from the spec, the sandbox instance, the agent telemetry, and the verifier
// outcome, then persists the iteration record and rubric score.
func (h *Harness) score(
	spec *eval.TaskSpec,
	instance *sandbox.Instance,
	agentResult runner.Result,
	verifyResult *verifier.VerifyResult,
) (scoringbridge.Result, error) {
	result, err := scoringbridge.ScoreRun(scoringbridge.EvalRun{
		RunID:      instance.RunID,
		RunDir:     instance.RunDir,
		BaseCommit: instance.BaseCommit,
		Spec:       spec,
		Agent:      mapTelemetry(agentResult.Telemetry),
		Verify:     mapVerify(spec, verifyResult),
	})
	if err != nil {
		return scoringbridge.Result{}, fmt.Errorf("harness: score run: %w", err)
	}
	return result, nil
}

// mapTelemetry translates the runner's telemetry to the scoring bridge's shape.
// The two structs are field-for-field identical today; this call site is the
// single documented adapter between them (see runner.AgentTelemetry's doc).
func mapTelemetry(t runner.AgentTelemetry) scoringbridge.AgentTelemetry {
	return scoringbridge.AgentTelemetry{
		SessionID: t.SessionID,
		Harness:   t.Harness,
		Model:     t.Model,
		Retries:   t.Retries,
		Tokens:    t.Tokens,
	}
}

// mapVerify translates the go verifier's phase-based result into the scoring
// bridge's build/test boolean view. The verifier runs build then test and
// short-circuits on build failure, so the terminal Phase fully determines what
// ran:
//
//   - PhaseBuild: the build command ran and FAILED (the test step was skipped).
//   - PhaseTest: the test step ran, which means any build command present must
//     have passed first.
//
// PhaseValidate never reaches here — the verifier returns it only via a
// VerifyError, which the verify stage already surfaced as an error.
func mapVerify(spec *eval.TaskSpec, result *verifier.VerifyResult) scoringbridge.VerifyResult {
	switch result.Phase {
	case verifier.PhaseBuild:
		return scoringbridge.VerifyResult{BuildRan: true, BuildPassed: false}
	case verifier.PhaseTest:
		hasBuild := len(spec.Verification.BuildCmd) > 0
		return scoringbridge.VerifyResult{
			BuildRan:    hasBuild,
			BuildPassed: hasBuild,
			TestRan:     true,
			TestPassed:  result.Passed,
		}
	default:
		return scoringbridge.VerifyResult{}
	}
}
