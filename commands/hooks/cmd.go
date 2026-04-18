package hooks

import "github.com/spf13/cobra"

// NewHooksCmd builds the `dot-agents hooks` command tree from injected dependencies.
func NewHooksCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hooks",
		Short: "Inspect and manage canonical ~/.agents/hooks bundles",
		Long: `Commands for hook resources stored under ~/.agents/hooks/.

Each scope directory is either global (~/.agents/hooks/global/) or a managed project
name (~/.agents/hooks/<project>/), matching names from dot-agents status.

Canonical hooks live in bundle directories: hooks/<scope>/<logical-name>/HOOK.yaml
(optionally with sidecar scripts). Legacy single-file JSON hooks
(hooks/<scope>/<name>.json) are still listed for visibility; prefer HOOK.yaml bundles
for new work — the same layout import and refresh use when canonicalizing hook content.`,
		Example: exampleBlock(
			"  dot-agents hooks list",
			"  dot-agents hooks list my-app",
			"  dot-agents hooks show global session-orient",
			"  dot-agents hooks remove global old-hook-bundle",
		),
	}
	cmd.AddCommand(newHooksListCmd(deps))
	cmd.AddCommand(newHooksShowCmd(deps))
	cmd.AddCommand(newHooksRemoveCmd(deps))
	return cmd
}

func newHooksListCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "list [scope]",
		Short: "List configured hooks for a scope",
		Example: exampleBlock(
			"  dot-agents hooks list",
			"  dot-agents hooks list billing-api",
		),
		Args: deps.MaxArgsWithHints(1, "Optionally pass a project scope (or `global`) to inspect that hooks tree."),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return runHooksList(scope)
		},
	}
}

func newHooksShowCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <scope> <name>",
		Short: "Show one hook bundle or legacy hook file in ~/.agents/hooks/",
		Args:  deps.ExactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` is the hook logical name."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHooksShow(deps, args[0], args[1])
		},
	}
}

func newHooksRemoveCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <scope> <name>",
		Short: "Remove a hook bundle directory or legacy hooks/*.json file from ~/.agents/hooks/",
		Long: `Deletes managed hook storage only (not project symlinks). After removal, run
dot-agents refresh or install for the relevant project so platform hook files stay
consistent.`,
		Args: deps.ExactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` matches list/show."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runHooksRemove(deps, args[0], args[1])
		},
	}
}
