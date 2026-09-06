// Package eval implements the `da eval` command tree: the operator surface over
// the R4 agent-evaluation harness. It mirrors the `da score` command-surface
// idioms (subcommands under a group, a --repo-dir root resolver, --json output)
// and is pure CLI wiring — every heavy stage lives behind the internal/eval
// seam packages (generator registry, sandbox, runner, verifier, scoring bridge,
// store).
//
//   - `da eval gen`  synthesises one TaskSpec from the knowledge graph.
//   - `da eval run`  drives one task end-to-end (gen → sandbox → run → verify →
//     score → persist) and prints the R1-scored outcome.
//   - `da eval ls`   lists the persisted eval runs under the eval root.
//
// The gen/run/ls RunE handlers are injected by the root command rather than
// defined here (see NewCmd). internal/globalflagcov statically traces each
// handler's global-flag reads by indexing a fixed set of command packages that
// does not include this subpackage, so a RunE closure defined here is opaque to
// it ("unresolved closure"). Wiring the handlers from package commands — where
// they read the global --json flag through a traceable commands.Flags access —
// keeps global-flag coverage analysable while the run logic stays here.
package eval

import (
	"os"

	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/spf13/cobra"
)

// repoDirFlagName / repoDirFlagHelp are shared by every eval subcommand that
// resolves the repository/eval root, so the flag surface stays identical across
// subcommands and a single edit reaches all of them (mirrors `da score`'s
// --repo-dir contract).
const (
	repoDirFlagName = "repo-dir"
	repoDirFlagHelp = "Repository root (default: current working directory)"
)

// languageFlagName is shared by gen and run: the language selects both the
// generator and (for run) the verifier.
const languageFlagName = "language"

// languageEnum, difficultyEnum, and agentAdapterEnum are the closed-set flags
// shared by `eval gen` and `eval run`. Declared once so the help listing, the
// completions, and the validation error all read the same vocabulary
// (docs/CLI_HELP_CONVENTIONS.md).
var languageEnum = cmdutil.EnumSpec{
	Name:   languageFlagName,
	Usage:  "Language the generated task is written in",
	Values: []string{"go", "python", "typescript"},
}

var difficultyEnum = cmdutil.EnumSpec{
	Name:   difficultyFlagName,
	Usage:  "Constrain the generated difficulty band",
	Values: []string{"easy", "medium", "hard"},
	Note:   "omit to let the generator choose",
}

var agentAdapterEnum = cmdutil.EnumSpec{
	Name:    agentFlagName,
	Usage:   "Agent adapter that runs the task",
	Values:  []string{"claude", "codex", "copilot"},
	Default: defaultAdapter,
}

// Shared generation-tuning + run flag names, named once so the constructors
// (which define them) and the option readers (which read them back) cannot
// drift apart.
const (
	difficultyFlagName = "difficulty"
	templateFlagName   = "template"
	outFlagName        = "out"
	taskFlagName       = "task"
	// agentFlagName is the CLI flag selecting the agent runner (spec R3 /
	// Done-Criterion 3: `da eval run --task <spec> --agent <runner>`). Only the
	// flag string is "agent"; the internal runner.Adapter type is unchanged.
	agentFlagName = "agent"
)

// handlerFunc is the cobra RunE signature the root wires per subcommand.
type handlerFunc = func(*cobra.Command, []string) error

// NewCmd builds the `da eval` command group. The gen/run/ls RunE handlers are
// supplied by the caller (root) so they compile in package commands and stay
// statically analysable by internal/globalflagcov; the exported RunGen/RunEval/
// RunLs entry points below are what those handlers call.
func NewCmd(genRunE, runRunE, lsRunE handlerFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "Generate, run, and inspect agent evaluation tasks",
		Long: "Drives the R4 agent-evaluation harness: synthesise a reproducible TaskSpec\n" +
			"from the knowledge graph, run it end-to-end in an isolated sandbox, and score\n" +
			"the outcome against the same rubric `da score` uses.\n\n" +
			"`eval gen` writes a TaskSpec. `eval run` executes one task (generating it when\n" +
			"--task is not given) and persists eval-run.yaml + the scored iteration-log\n" +
			"sidecars under .agents/eval/runs/<run-id>/. `eval ls` lists those runs.",
	}
	cmd.AddCommand(newGenCmd(genRunE))
	cmd.AddCommand(newRunCmd(runRunE))
	cmd.AddCommand(newLsCmd(lsRunE))
	return cmd
}

// resolveRepoDir mirrors `da score`'s --repo-dir contract: an explicit value
// wins, otherwise the current working directory. os.Getwd is treated as
// effectively infallible on a live process (the same guidance runScoreRun
// follows); a downstream stage surfaces a more useful error if the result is
// somehow empty.
func resolveRepoDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	cwd, _ := os.Getwd()
	return cwd
}

// flagString reads a string flag off cmd, tolerating an undefined flag (empty).
// The subcommand constructors define the flags; the exported RunGen/RunEval/
// RunLs handlers read them back through this helper so the injected RunE
// handlers in package commands need no knowledge of the flag wiring.
func flagString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}
