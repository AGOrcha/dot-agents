package settings

import "github.com/AGOrcha/dot-agents/commands/internal/cmdutil"

// RunRemove deletes a canonical settings file under ~/.agents/settings/<scope>/,
// honoring the dry-run / yes / force flags carried in Deps.
func RunRemove(deps Deps, scope, name string) error {
	return cmdutil.RunCanonicalRemove(
		cmdutil.RemoveDeps{
			DryRun: deps.Flags.DryRun,
			Yes:    deps.Flags.Yes,
			Force:  deps.Flags.Force,
		},
		scope,
		name,
		canonicalSpec(deps),
	)
}
