package settings

import "github.com/spf13/cobra"

// NewCmd builds the `da settings` command tree from injected dependencies.
// Mirrors agents.NewAgentsCmd / skills.NewSkillsCmd: helpers come from Deps
// so the subpackage stays independent of the parent commands/ package.
func NewCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Inspect and manage canonical ~/.agents/settings files",
		Long: `Commands for platform settings files stored under ~/.agents/settings/<scope>/.

Scopes are either global (~/.agents/settings/global/) or a managed project name
(~/.agents/settings/<project>/), matching da status.

Files include JSON/TOML/YAML configs (e.g. cursor.json, claude-code.json) and
cursorignore. These are wired by add, import, refresh, install, and remove.
Prefer editing canonical paths here, then run refresh or install.`,
		Example: exampleBlock(
			"  da settings list",
			"  da settings list my-app",
			"  da settings show global cursor.json",
			"  da settings remove proj cursorignore",
		),
	}
	cmd.AddCommand(NewListCmd(deps))
	cmd.AddCommand(NewShowCmd(deps))
	cmd.AddCommand(NewRemoveCmd(deps))
	return cmd
}

// NewListCmd builds the `da settings list` subcommand.
func NewListCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list [scope]",
		Short: "List canonical settings files for a scope",
		Example: exampleBlock(
			"  da settings list",
			"  da settings list billing-api",
		),
		Args: deps.MaximumNArgsWithHints(1, "Optionally pass a project scope (or `global`) to inspect that settings tree."),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return RunList(scope)
		},
	}
}

// NewShowCmd builds the `da settings show` subcommand.
func NewShowCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <scope> <name>",
		Short: "Show metadata for one settings file under ~/.agents/settings/",
		Args:  deps.ExactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` is the file (e.g. cursor.json) or stem."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunShow(deps, args[0], args[1])
		},
	}
}

// NewRemoveCmd builds the `da settings remove` subcommand.
func NewRemoveCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <scope> <name>",
		Short: "Remove a settings file from ~/.agents/settings/ (canonical storage only)",
		Long: `Deletes the file from managed settings storage only (not repo links). After removal,
run da refresh or install for the relevant project so platform settings
links stay consistent.`,
		Args: deps.ExactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` matches list/show."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunRemove(deps, args[0], args[1])
		},
	}
}
