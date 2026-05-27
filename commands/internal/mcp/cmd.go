package mcp

import (
	"github.com/spf13/cobra"
)

// NewCmd builds the `da mcp` command tree from injected dependencies.
// Mirrors agents.NewAgentsCmd / skills.NewSkillsCmd: helpers come from
// Deps so the subpackage stays independent of the parent commands/
// package.
func NewCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
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
	}
	cmd.AddCommand(NewListCmd(deps))
	cmd.AddCommand(NewShowCmd(deps))
	cmd.AddCommand(NewRemoveCmd(deps))
	return cmd
}

// NewListCmd builds the `da mcp list` subcommand.
func NewListCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
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

// NewShowCmd builds the `da mcp show` subcommand.
func NewShowCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <scope> <name>",
		Short: "Show metadata for one MCP file under ~/.agents/mcp/",
		Args:  deps.ExactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` is the file (e.g. mcp.json) or stem (mcp)."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunShow(deps, args[0], args[1])
		},
	}
}

// NewRemoveCmd builds the `da mcp remove` subcommand.
func NewRemoveCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
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
