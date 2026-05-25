package commands

import (
	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/NikashPrakash/dot-agents/commands/mcp"
	"github.com/spf13/cobra"
)

// mcpDeps is the parent commands-package alias for mcp.Deps so the legacy
// in-package tests (commands/coverage_test.go, commands/resource_parity_test.go,
// commands/mcp_test.go) continue to type-check while their reference to
// `mcpDeps` is gradually rehomed. t13 deletes this shim once the
// cross-cutting tests t12 relocates them have landed.
type mcpDeps = mcp.Deps

// mcpCommandDeps builds the Deps struct passed to mcp.NewCmd. Mirrors
// agentsDeps / skillsDeps so the three subcommand subpackages share the
// same wiring pattern.
func mcpCommandDeps() mcpDeps {
	return mcp.Deps{
		Flags: cmdutil.CanonicalCmdFlags{
			DryRun: Flags.DryRun,
			Yes:    Flags.Yes,
			Force:  Flags.Force,
		},
		MaxArgsWithHints:   MaximumNArgsWithHints,
		ExactArgsWithHints: ExactArgsWithHints,
		ErrorWithHints:     ErrorWithHints,
		UsageError:         UsageError,
	}
}

// NewMCPCmd wires the mcp subcommand tree. Thin shim preserved for
// source-compat with root.go and external callers; the implementation
// now lives entirely under commands/mcp/.
func NewMCPCmd() *cobra.Command {
	return mcp.NewCmd(mcpCommandDeps())
}

// The subcommand-builder shims below are retained because
// commands/coverage_test.go (out of t10a write_scope) still calls them
// directly to exercise the RunE wiring. Each delegates straight to the
// matching mcp.* exported constructor.
func newMCPListCmd(deps mcpDeps) *cobra.Command   { return mcp.NewListCmd(deps) }
func newMCPShowCmd(deps mcpDeps) *cobra.Command   { return mcp.NewShowCmd(deps) }
func newMCPRemoveCmd(deps mcpDeps) *cobra.Command { return mcp.NewRemoveCmd(deps) }

// The run* shims preserve the parent-package signatures used by
// commands/resource_parity_test.go's case table. Internally each calls
// the subpackage entry point with a fully-populated Deps so the
// CLIError shape produced by findMCPSpec stays consistent with
// pre-refactor behaviour.
func runMCPList(scope string) error       { return mcp.RunList(scope) }
func runMCPShow(scope, name string) error { return mcp.RunShow(mcpCommandDeps(), scope, name) }
func runMCPRemove(deps mcpDeps, scope, name string) error {
	return mcp.RunRemove(deps, scope, name)
}
