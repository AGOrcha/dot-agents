package settings

import "github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"

// RunShow prints metadata for one canonical settings file under
// ~/.agents/settings/<scope>/.
func RunShow(deps Deps, scope, name string) error {
	return cmdutil.RunCanonicalShow(scope, name, canonicalSpec(deps))
}
