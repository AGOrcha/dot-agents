package mcp

import (
	"github.com/NikashPrakash/dot-agents/commands/internal/canonical"
	"github.com/spf13/cobra"
)

// NewCmd builds the `da mcp` command tree from injected dependencies.
// Mirrors agents.NewAgentsCmd / skills.NewSkillsCmd: helpers come from
// Deps so the subpackage stays independent of the parent commands/
// package. The cobra-tree assembly is delegated to
// canonical.NewCanonicalResourceCmd so mcp/settings/rules share one
// implementation.
func NewCmd(deps Deps) *cobra.Command {
	return canonical.NewCanonicalResourceCmd(canonical.ResourceCmdSpec{
		Use:   "mcp",
		Short: "Inspect and manage canonical ~/.agents/mcp config files",
		Long: `Commands for MCP server configs stored under ~/.agents/mcp/<scope>/.

Scopes are either global (~/.agents/mcp/global/) or a managed project name
(~/.agents/mcp/<project>/), matching da status.

These files are what add, import, refresh, install, and remove wire into
Cursor, Claude Code, Copilot, and related projections. Prefer editing canonical
paths here, then run refresh or install for the project.`,
		Example: exampleBlock(
			"  da mcp list",
			"  da mcp list my-app",
			"  da mcp show global mcp.json",
			"  da mcp remove global stale.json",
		),
		List:   newListSpec(deps),
		Show:   newShowSpec(deps),
		Remove: newRemoveSpec(deps),
	})
}

// NewListCmd builds the `da mcp list` subcommand standalone (used by the
// parent-package shim's cross-cutting coverage tests).
func NewListCmd(deps Deps) *cobra.Command { return canonical.NewSubCmd(newListSpec(deps)) }

// NewShowCmd builds the `da mcp show` subcommand standalone.
func NewShowCmd(deps Deps) *cobra.Command { return canonical.NewSubCmd(newShowSpec(deps)) }

// NewRemoveCmd builds the `da mcp remove` subcommand standalone.
func NewRemoveCmd(deps Deps) *cobra.Command { return canonical.NewSubCmd(newRemoveSpec(deps)) }

// newListSpec returns the canonical.SubCmdSpec for `da mcp list`,
// pre-binding Args via deps.MaxArgsWithHints (mcp's Deps shape, which
// differs from settings/rules' MaximumNArgsWithHints).
func newListSpec(deps Deps) canonical.SubCmdSpec {
	return canonical.SubCmdSpec{
		Use:   "list [scope]",
		Short: "List canonical MCP config files for a scope",
		Example: exampleBlock(
			"  da mcp list",
			"  da mcp list billing-api",
		),
		Args: deps.MaxArgsWithHints(1, "Optionally pass a project scope (or `global`) to inspect that MCP tree."),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return RunList(scope)
		},
	}
}

// newShowSpec returns the canonical.SubCmdSpec for `da mcp show`.
func newShowSpec(deps Deps) canonical.SubCmdSpec {
	return canonical.SubCmdSpec{
		Use:   "show <scope> <name>",
		Short: "Show metadata for one MCP file under ~/.agents/mcp/",
		Args:  deps.ExactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` is the file (e.g. mcp.json) or stem (mcp)."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunShow(deps, args[0], args[1])
		},
	}
}

// newRemoveSpec returns the canonical.SubCmdSpec for `da mcp remove`.
func newRemoveSpec(deps Deps) canonical.SubCmdSpec {
	return canonical.SubCmdSpec{
		Use:   "remove <scope> <name>",
		Short: "Remove an MCP file from ~/.agents/mcp/ (canonical storage only)",
		Long: `Deletes the file from managed MCP storage only (not repo links). After removal,
run da refresh or install for the relevant project so platform MCP
links stay consistent.`,
		Args: deps.ExactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` matches list/show."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunRemove(deps, args[0], args[1])
		},
	}
}
