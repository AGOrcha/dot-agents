package settings

import (
	"github.com/NikashPrakash/dot-agents/commands/internal/canonical"
	"github.com/spf13/cobra"
)

// NewCmd builds the `da settings` command tree from injected dependencies.
// Mirrors agents.NewAgentsCmd / skills.NewSkillsCmd: helpers come from Deps
// so the subpackage stays independent of the parent commands/ package. The
// cobra-tree assembly is delegated to canonical.NewCanonicalResourceCmd
// so mcp/settings/rules share one implementation.
func NewCmd(deps Deps) *cobra.Command {
	return canonical.NewCanonicalResourceCmd(canonical.ResourceCmdSpec{
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
		List:   newListSpec(deps),
		Show:   newShowSpec(deps),
		Remove: newRemoveSpec(deps),
	})
}

// NewListCmd builds the `da settings list` subcommand standalone.
func NewListCmd(deps Deps) *cobra.Command { return canonical.NewSubCmd(newListSpec(deps)) }

// NewShowCmd builds the `da settings show` subcommand standalone.
func NewShowCmd(deps Deps) *cobra.Command { return canonical.NewSubCmd(newShowSpec(deps)) }

// NewRemoveCmd builds the `da settings remove` subcommand standalone.
func NewRemoveCmd(deps Deps) *cobra.Command { return canonical.NewSubCmd(newRemoveSpec(deps)) }

// newListSpec returns the canonical.SubCmdSpec for `da settings list`.
func newListSpec(deps Deps) canonical.SubCmdSpec {
	return canonical.SubCmdSpec{
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

// newShowSpec returns the canonical.SubCmdSpec for `da settings show`.
func newShowSpec(deps Deps) canonical.SubCmdSpec {
	return canonical.SubCmdSpec{
		Use:   "show <scope> <name>",
		Short: "Show metadata for one settings file under ~/.agents/settings/",
		Args:  deps.ExactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` is the file (e.g. cursor.json) or stem."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunShow(deps, args[0], args[1])
		},
	}
}

// newRemoveSpec returns the canonical.SubCmdSpec for `da settings remove`.
func newRemoveSpec(deps Deps) canonical.SubCmdSpec {
	return canonical.SubCmdSpec{
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
