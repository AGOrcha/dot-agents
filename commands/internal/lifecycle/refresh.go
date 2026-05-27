package lifecycle

import "github.com/spf13/cobra"

// NewRefreshCmd builds the `da refresh` cobra command. The cobra
// metadata (Use/Short/Long/Example/Args) and the `--import` flag live
// here in lifecycle; the RunE delegates to deps.RunRefresh which still
// holds the legacy runRefresh body in commands/ until t04 (add) and
// t06 (import) merge. See SHAPE.md §4a (refresh row) and the fold-back
// at .agents/active/fold-back/t07-refresh-body-deferred.md for the
// rationale.
func NewRefreshCmd(deps Deps) *cobra.Command {
	var importAlso bool
	cmd := &cobra.Command{
		Use:   "refresh [project]",
		Short: "Refresh managed setup in projects from ~/.agents/",
		Long: `Re-applies links and config from ~/.agents/ into project directories.
Use after pulling changes to ~/.agents/ or when a project's agent config is out of sync.`,
		Example: deps.ExampleBlock(
			"  da refresh",
			"  da refresh billing-api",
			"  da refresh --import --dry-run",
		),
		Args: deps.MaximumNArgsWithHints(1, "Optionally pass one managed project name to limit the refresh."),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			return deps.RunRefresh(filter, importAlso)
		},
	}
	cmd.Flags().BoolVar(&importAlso, "import", false, "Also import global user configs into ~/.agents before relinking")
	return cmd
}
