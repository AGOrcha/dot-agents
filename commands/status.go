package commands

import (
	"github.com/NikashPrakash/dot-agents/commands/lifecycle"
	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/spf13/cobra"
)

// lifecycleStatusDeps builds the lifecycle.Deps struct from the root commands
// package's UX helpers. Mirrors agentsDeps() in commands/agents.go.
func lifecycleStatusDeps() lifecycle.Deps {
	return lifecycle.Deps{
		Flags: lifecycle.GlobalFlags{
			Yes: Flags.Yes,
		},
		ErrorWithHints:        ErrorWithHints,
		UsageError:            UsageError,
		MaximumNArgsWithHints: MaximumNArgsWithHints,
		RangeArgsWithHints:    RangeArgsWithHints,
		ExactArgsWithHints:    ExactArgsWithHints,
	}
}

// NewStatusCmd wires the status subcommand. Thin shim around
// lifecycle.NewStatusCmd; the JSON flag is read from the root commands.Flags
// package-var seam at RunE time via the closure so lifecycle avoids an
// import-cycle on the parent commands package. Per SHAPE.md the shim is
// removed in t13; this file lives only during the t08→t13 window.
//
// The RunE closure is intentionally re-bound here (rather than inherited
// from lifecycle.NewStatusCmd) so the globalflagcov static analyzer — which
// loads ./commands but not ./commands/lifecycle — can resolve the
// Flags.JSON read it requires for handler coverage. After t13 strips the
// shim, the analyzer's load set should be widened to include
// ./commands/lifecycle (tracked as a follow-up in the t13 PR description).
func NewStatusCmd() *cobra.Command {
	jsonFlag := func() bool { return Flags.JSON }
	cmd := lifecycle.NewStatusCmd(lifecycleStatusDeps(), jsonFlag)
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		audit, _ := c.Flags().GetBool("audit")
		agentFilter, _ := c.Flags().GetString("agent")
		// Route the JSON read through jsonFlag() so the globalflagcov
		// static analyzer sees the Flags.JSON load on ./commands (it
		// does not load ./commands/lifecycle yet — see t13 follow-up).
		// Without this hop the closure passed to lifecycle.NewStatusCmd
		// would be unreferenced from any executed path (its RunE is
		// overridden here), leaving the Flags.JSON read uncovered.
		return lifecycle.RunStatusDefault(audit, agentFilter, jsonFlag())
	}
	return cmd
}

// --- t08→t11 cross-package window shims ---
//
// The following thin wrappers keep commands/doctor.go and
// commands/seams_test.go (both out of t08's write scope) compiling after
// status.go moved into commands/lifecycle. They delegate to the exported
// lifecycle.* entry points and are deleted in t13 alongside the rest of the
// root lifecycle shims. See SHAPE.md OD-2.

// statusConfigLoader aliases lifecycle.StatusConfigLoader so seams_test.go's
// fakeStatusConfigLoader (defined in commands/status_test.go) still satisfies
// the interface that runStatus expects.
type statusConfigLoader = lifecycle.StatusConfigLoader

// runStatus is the root-package shim for lifecycle.RunStatus.
// commands/seams_test.go's TestRunStatus_ConfigLoadError calls it with a
// fake loader; the JSON flag is read off Flags.JSON to preserve the
// pre-move call site (no extra jsonOut argument).
func runStatus(audit bool, agentFilter string, deps statusConfigLoader) error {
	return lifecycle.RunStatus(audit, agentFilter, deps, Flags.JSON)
}

// printAudit is the root-package shim for lifecycle.PrintAudit, called by
// commands/doctor.go's verbose branches in reportOneProjectLinkHealth.
func printAudit(name, path, agentsHome, agentFilter string, cfg *config.Config) {
	lifecycle.PrintAudit(name, path, agentsHome, agentFilter, cfg)
}

// printSymlinkDirAudit is the root-package shim for
// lifecycle.PrintSymlinkDirAudit, called by
// commands/seams_test.go's TestPrintSymlinkDirAudit_NonexistentDir.
func printSymlinkDirAudit(dir, emptyLabel, nameFormat string) (int, int) {
	return lifecycle.PrintSymlinkDirAudit(dir, emptyLabel, nameFormat)
}

// countClaudeRules is the root-package shim for lifecycle.CountClaudeRules,
// called by commands/seams_test.go's TestCountClaudeRules_ReadlinkFails.
func countClaudeRules(path string) (int, int) {
	return lifecycle.CountClaudeRules(path)
}
