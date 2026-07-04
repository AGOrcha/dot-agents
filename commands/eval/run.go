package eval

import (
	"context"
	"fmt"
	"io"
	"os"

	evalcore "github.com/AGOrcha/dot-agents/internal/eval"
	"github.com/AGOrcha/dot-agents/internal/eval/harness"
	"github.com/AGOrcha/dot-agents/internal/eval/runner"
	"github.com/AGOrcha/dot-agents/internal/eval/sandbox"
	"github.com/AGOrcha/dot-agents/internal/eval/store"
	"github.com/AGOrcha/dot-agents/internal/eval/verifier"
	"github.com/spf13/cobra"
)

// defaultAdapter is the agent runner used when --agent is not given. Claude is
// the v1 default (runner.AdapterClaude).
const defaultAdapter = string(runner.AdapterClaude)

// warnOut is where best-effort run diagnostics (e.g. a stale-sandbox prune
// failure) are surfaced. It defaults to stderr and is overridable in tests.
var warnOut io.Writer = os.Stderr

// newSandbox and newRunner are the production-wiring seams for the two agent-
// facing collaborators. Production returns the worktree sandbox and the
// configured agent adapter; tests replace them with a scripted sandbox and a
// runner.FakeRunner so the end-to-end pipeline runs without a git worktree or a
// live agent (the acceptance criterion's FakeRunner path).
var (
	newSandbox = func(cfg sandbox.Config) (sandbox.Sandbox, error) {
		return sandbox.NewWorktreeSandbox(cfg)
	}
	newRunner = func(a runner.Adapter) (runner.Runner, error) {
		return runner.New(a)
	}
)

// runOptions are the parsed flags of `da eval run`.
type runOptions struct {
	language   string
	task       string
	difficulty string
	template   string
	adapter    string
	repoDir    string
}

// newRunCmd builds `da eval run`. The RunE handler is injected by the root (see
// package doc); this constructor owns only the command shape + flag definitions.
func newRunCmd(runE handlerFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run one eval task end-to-end and score the outcome",
		Long: "Executes the full eval pipeline for one task: generate (or load via --task),\n" +
			"provision an isolated sandbox, run the configured agent, verify build/test, score\n" +
			"the outcome against the R1 rubric, and persist the sidecars under\n" +
			".agents/eval/runs/<run-id>/.",
		Example: "  da eval run --language go\n" +
			"  da eval run --language go --agent codex\n" +
			"  da eval run --task task.yaml",
		Args: cobra.NoArgs,
		RunE: runE,
	}
	cmd.Flags().String(languageFlagName, "", languageFlagHelp+" (inferred from --task when given)")
	cmd.Flags().String(taskFlagName, "", "Run a pre-generated TaskSpec YAML instead of generating one")
	cmd.Flags().String(difficultyFlagName, "", "Constrain the generated difficulty band (ignored with --task)")
	cmd.Flags().String(templateFlagName, "", "Task template id (ignored with --task)")
	cmd.Flags().String(agentFlagName, defaultAdapter, "Agent adapter: claude, codex, or copilot")
	cmd.Flags().String(repoDirFlagName, "", repoDirFlagHelp)
	return cmd
}

// RunEval is the `da eval run` entry point the root wires as the subcommand's
// RunE. asJSON is the resolved global --json flag, passed by the root handler so
// the flag read stays statically traceable in package commands.
func RunEval(cmd *cobra.Command, asJSON bool) error {
	return runEvalCommand(cmd.Context(), cmd.OutOrStdout(), runOptionsFrom(cmd), asJSON)
}

// runOptionsFrom reads the run subcommand's flags off cmd.
func runOptionsFrom(cmd *cobra.Command) runOptions {
	return runOptions{
		language:   flagString(cmd, languageFlagName),
		task:       flagString(cmd, taskFlagName),
		difficulty: flagString(cmd, difficultyFlagName),
		template:   flagString(cmd, templateFlagName),
		adapter:    flagString(cmd, agentFlagName),
		repoDir:    flagString(cmd, repoDirFlagName),
	}
}

// runEvalCommand assembles the real harness around the pipeline seams (KG
// registry or a --task spec, the worktree sandbox, the configured agent runner,
// and the go verifier) and drives the shared runEval core. It is the un-mocked
// CLI entry; the acceptance test drives the same path with the sandbox/runner
// seams overridden.
func runEvalCommand(ctx context.Context, out io.Writer, opts runOptions, asJSON bool) error {
	root := resolveRepoDir(opts.repoDir)
	h, lang, closeFn, err := buildHarness(root, opts)
	if err != nil {
		return err
	}
	defer closeReader(closeFn)
	return runEval(ctx, out, runContext{
		harness:    h,
		root:       root,
		language:   lang,
		difficulty: evalcore.Difficulty(opts.difficulty),
		template:   opts.template,
		asJSON:     asJSON,
	})
}

// buildHarness resolves the generator source, provisions the sandbox and runner
// seams, and constructs the harness. It returns the resolved run language and a
// reader closer (nil in --task mode, which opens no graph). Every early-exit
// releases the reader so a partial wiring never leaks the graph handle.
func buildHarness(root string, opts runOptions) (*harness.Harness, evalcore.Language, func() error, error) {
	reg, lang, closeFn, err := resolveGenerators(opts)
	if err != nil {
		return nil, "", nil, err
	}
	sb, err := newSandbox(sandbox.Config{RepoPath: root})
	if err != nil {
		closeReader(closeFn)
		return nil, "", nil, fmt.Errorf("eval run: sandbox: %w", err)
	}
	// OQ6: sweep stale worktrees from crashed prior runs before this run
	// provisions its own sandbox. Best-effort — a prune failure never aborts.
	pruneStaleSandbox(sb)
	run, err := newRunner(runner.Adapter(opts.adapter))
	if err != nil {
		closeReader(closeFn)
		return nil, "", nil, fmt.Errorf("eval run: runner: %w", err)
	}
	h, err := harness.New(harness.Config{
		Generators: reg,
		Sandbox:    sb,
		Runner:     run,
		Verifiers:  verifiers(),
	})
	if err != nil {
		closeReader(closeFn)
		return nil, "", nil, fmt.Errorf("eval run: build harness: %w", err)
	}
	return h, lang, closeFn, nil
}

// pruneStaleSandbox runs the OQ6 stale-worktree sweep. It is deliberately
// best-effort: neither a prune failure nor a pruned-run report aborts the run
// (the failure is surfaced as a warning on warnOut rather than swallowed, and a
// non-empty sweep is noted). The v1 spec default is "pruned on next da eval
// run", and R4 is CLI-first (D4.8), so the run path is the sweep's trigger.
func pruneStaleSandbox(sb sandbox.Sandbox) {
	pruned, err := sb.PruneStale(context.Background())
	if err != nil {
		fmt.Fprintf(warnOut, "eval run: warning: stale-sandbox prune failed: %v\n", err)
		return
	}
	if len(pruned) > 0 {
		fmt.Fprintf(warnOut, "eval run: pruned %d stale sandbox worktree(s)\n", len(pruned))
	}
}

// resolveGenerators picks the generator source. With --task it registers a
// single fixed generator that replays the loaded spec (the harness always
// generates through its registry, so a caller-supplied spec is delivered this
// way); otherwise it opens the KG registry for the requested language.
func resolveGenerators(opts runOptions) (*evalcore.Registry, evalcore.Language, func() error, error) {
	if opts.task != "" {
		reg, lang, err := fixedRegistry(opts)
		return reg, lang, nil, err
	}
	lang := evalcore.Language(opts.language)
	if err := validateLanguage(lang); err != nil {
		return nil, "", nil, err
	}
	reg, closeFn, err := kgRegistry()
	return reg, lang, closeFn, err
}

// fixedRegistry loads the --task spec and wraps it in a registry whose only
// generator replays it. When --language is also set it must agree with the
// spec's own language, so a mismatched invocation fails loudly rather than
// silently ignoring one of the two.
func fixedRegistry(opts runOptions) (*evalcore.Registry, evalcore.Language, error) {
	spec, err := loadTaskSpec(opts.task)
	if err != nil {
		return nil, "", err
	}
	if opts.language != "" && evalcore.Language(opts.language) != spec.Language {
		return nil, "", fmt.Errorf("eval run: --%s %q conflicts with task spec language %q",
			languageFlagName, opts.language, spec.Language)
	}
	reg := evalcore.NewRegistry()
	if err := reg.Register(fixedGenerator{spec: spec}); err != nil {
		return nil, "", fmt.Errorf("eval run: register task: %w", err)
	}
	return reg, spec.Language, nil
}

// loadTaskSpec reads and validates a TaskSpec YAML from path.
func loadTaskSpec(path string) (*evalcore.TaskSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("eval run: read task %s: %w", path, err)
	}
	spec, err := evalcore.ParseTaskSpec(data)
	if err != nil {
		return nil, fmt.Errorf("eval run: invalid task spec: %w", err)
	}
	return spec, nil
}

// fixedGenerator is an eval.Generator that returns a pre-loaded TaskSpec
// verbatim, ignoring GenerateOptions. It backs `da eval run --task <path>`.
type fixedGenerator struct {
	spec *evalcore.TaskSpec
}

// Language reports the loaded spec's language.
func (g fixedGenerator) Language() evalcore.Language { return g.spec.Language }

// Generate returns the pre-loaded spec, satisfying eval.Generator.
func (g fixedGenerator) Generate(context.Context, evalcore.GenerateOptions) (*evalcore.TaskSpec, error) {
	return g.spec, nil
}

// verifierFactories is the additive per-language verifier registry. Each
// language verifier self-registers its factory via registerVerifier from its
// own file (verifiers_go.go, and sibling verifiers_python.go / _typescript.go),
// so adding a language drops in a new registration file instead of editing this
// one — keeping run.go language-agnostic and the sibling deliveries disjoint.
var verifierFactories []func() verifier.Verifier

// registerVerifier appends a verifier factory to the registry. It is invoked
// from the package-level init in each per-language registration file.
func registerVerifier(f func() verifier.Verifier) {
	verifierFactories = append(verifierFactories, f)
}

// verifiers builds the language→verifier map the harness verifies against from
// the registered factories, keyed by each verifier's reported Language.
func verifiers() map[evalcore.Language]verifier.Verifier {
	m := make(map[evalcore.Language]verifier.Verifier, len(verifierFactories))
	for _, f := range verifierFactories {
		v := f()
		m[v.Language()] = v
	}
	return m
}

// runContext bundles the resolved inputs runEval threads into the harness and
// store, keeping runEval within the S107 parameter budget.
type runContext struct {
	harness    *harness.Harness
	root       string
	language   evalcore.Language
	difficulty evalcore.Difficulty
	template   string
	asJSON     bool
}

// runEval executes one task through the harness and persists it via the store,
// then renders the outcome. It is the shared core the acceptance test exercises
// end-to-end with a FakeRunner; buildHarness assembles the real seams around it.
//
// The harness's score stage writes the iteration-log sidecars into the sandbox
// instance's RunDir; store.WriteEvalRun then reconciles that RunDir with the
// canonical <root>/.agents/eval/runs/<run-id> location — adopting the sidecars
// in place when the sandbox already provisioned there (the default worktree
// wiring, where RunsRoot is <root>/.agents/eval/runs) or copying them in
// otherwise. Passing harness.Run's EvalRun straight into WriteEvalRun with the
// same root is what makes that reconciliation resolve consistently.
func runEval(ctx context.Context, out io.Writer, rc runContext) error {
	evalRun, err := rc.harness.Run(ctx, harness.Options{
		Language:   rc.language,
		Difficulty: rc.difficulty,
		TemplateID: rc.template,
	})
	if err != nil {
		return fmt.Errorf("eval run: %w", err)
	}
	res, err := store.WriteEvalRun(evalRun, rc.root)
	if err != nil {
		return fmt.Errorf("eval run: persist: %w", err)
	}
	return renderRun(out, evalRun, res, rc.asJSON)
}
