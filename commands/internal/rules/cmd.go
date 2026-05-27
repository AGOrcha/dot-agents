package rules

import (
	"github.com/spf13/cobra"
)

// NewRulesCmd builds the `da rules` command tree from injected dependencies.
// Mirrors agents.NewAgentsCmd / skills.NewSkillsCmd: helpers come from Deps so
// the subpackage stays independent of the parent commands/ package.
func NewRulesCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Inspect and manage canonical ~/.agents/rules files",
		Long: `Commands for rule files stored under ~/.agents/rules/<scope>/.

Scopes are either global (~/.agents/rules/global/) or a managed project name
(~/.agents/rules/<project>/), matching da status.

These files are what add, import, refresh, install, and remove wire into
Cursor, Claude Code, Codex, and Copilot projections. Prefer editing canonical
paths here, then run refresh or install for the project — do not hand-edit
platform copies unless you know they are unmanaged.`,
		Example: exampleBlock(
			"  da rules list",
			"  da rules list my-app",
			"  da rules show global rules.mdc",
			"  da rules remove global old-rule.mdc",
		),
	}
	cmd.AddCommand(NewListCmd(deps))
	cmd.AddCommand(NewShowCmd(deps))
	cmd.AddCommand(NewRemoveCmd(deps))
	return cmd
}

// NewListCmd builds the `da rules list` cobra command. Exported so the
// parent-package shim can wire it for cross-cutting RunE coverage tests.
func NewListCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list [scope]",
		Short: "List canonical rule files for a scope",
		Example: exampleBlock(
			"  da rules list",
			"  da rules list billing-api",
		),
		Args: deps.MaximumNArgsWithHints(1, "Optionally pass a project scope (or `global`) to inspect that rules tree."),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return RunList(deps, scope)
		},
	}
}

// NewShowCmd builds the `da rules show` cobra command. Exported so the
// parent-package shim can wire it for cross-cutting RunE coverage tests.
func NewShowCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <scope> <name>",
		Short: "Show metadata for one rule file under ~/.agents/rules/",
		Args:  deps.ExactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` is the file (e.g. rules.mdc) or stem (rules)."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunShow(deps, args[0], args[1])
		},
	}
}

// NewRemoveCmd builds the `da rules remove` cobra command. Exported so the
// parent-package shim can wire it for cross-cutting RunE coverage tests.
func NewRemoveCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <scope> <name>",
		Short: "Remove a rule file from ~/.agents/rules/ (canonical storage only)",
		Long: `Deletes the file from managed rule storage only (not repo links). After removal,
run da refresh or install for the relevant project so platform rule
links stay consistent.`,
		Args: deps.ExactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` matches list/show."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunRemove(deps, args[0], args[1])
		},
	}
}
