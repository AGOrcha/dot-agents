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
package eval

import (
	"os"

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

// languageFlagName / languageFlagHelp are shared by gen and run: the language
// selects both the generator and (for run) the verifier.
const (
	languageFlagName = "language"
	languageFlagHelp = "Task language: go, python, or typescript"
)

// Deps carries the cross-cutting collaborators the eval command tree needs from
// root. Today that is only the resolved global --json getter, threaded the same
// way config/mcp/settings receive theirs.
type Deps struct {
	// JSON reports whether the global --json flag is set. A nil getter is
	// treated as "text output" so a zero Deps is usable in tests.
	JSON func() bool
}

// json reports the resolved --json flag, tolerating a nil getter.
func (d Deps) json() bool {
	if d.JSON == nil {
		return false
	}
	return d.JSON()
}

// NewCmd builds the `da eval` command group and its gen/run/ls subcommands.
func NewCmd(deps Deps) *cobra.Command {
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
	cmd.AddCommand(newGenCmd(deps))
	cmd.AddCommand(newRunCmd(deps))
	cmd.AddCommand(newLsCmd(deps))
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
