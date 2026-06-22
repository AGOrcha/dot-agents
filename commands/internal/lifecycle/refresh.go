package lifecycle

import (
	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/spf13/cobra"
)

// DetectAndEnableNewPlatforms re-probes every known platform and reconciles
// cfg with what is actually installed on this machine:
//
//   - A platform installed but currently disabled in cfg is flipped to enabled
//     and its live version recorded. This is the refresh-driven fix for a
//     platform installed AFTER `da init`: init writes enabled:false for tools
//     that were not on PATH at init time, and a plain refresh would otherwise
//     iterate only already-enabled platforms forever (the "Nothing to refresh"
//     dead-end).
//   - A platform already enabled and still installed has its recorded version
//     refreshed from the live probe (stale version strings are updated).
//
// The probe is the same one init uses: platform.Platform.IsInstalled() (CLI on
// PATH) and Version() for the recorded version string. Detection is
// conservative — it never DISABLES anything. A platform that is enabled but no
// longer installed is left enabled (callers simply skip projecting/refreshing
// what is absent); refresh does not auto-disable.
//
// The caller is responsible for persisting cfg (cfg.Save()) after this returns.
// Returns the display names of the platforms that were newly enabled, in All()
// order, so the caller can announce each one.
func DetectAndEnableNewPlatforms(cfg *config.Config) []string {
	newlyEnabled := []string{}
	for _, p := range platform.All() {
		if !p.IsInstalled() {
			continue
		}
		alreadyEnabled := cfg.IsPlatformEnabled(p.ID())
		cfg.SetPlatformState(p.ID(), true, p.Version())
		if !alreadyEnabled {
			newlyEnabled = append(newlyEnabled, p.DisplayName())
		}
	}
	return newlyEnabled
}

// NewRefreshCmd builds the `da refresh` cobra command. The cobra
// metadata (Use/Short/Long/Example/Args) and the `--import` / `--inexact`
// flags live here in lifecycle; the RunE delegates to deps.RunRefresh which
// still holds the legacy runRefresh body in commands/ until t04 (add) and
// t06 (import) merge. See SHAPE.md §4a (refresh row) and the fold-back
// at .agents/active/fold-back/t07-refresh-body-deferred.md for the
// rationale.
func NewRefreshCmd(deps Deps) *cobra.Command {
	var importAlso bool
	var inexact bool
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
			return deps.RunRefresh(filter, importAlso, inexact)
		},
	}
	cmd.Flags().BoolVar(&importAlso, "import", false, "Also import global user configs into ~/.agents before relinking")
	cmd.Flags().BoolVar(&inexact, "inexact", false, "Keep additive behavior: write the resolved set but do NOT prune managed outputs no longer in it (refresh otherwise converges the tree to exactly what the lock declares)")
	return cmd
}
