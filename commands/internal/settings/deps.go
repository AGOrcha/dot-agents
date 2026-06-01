// Package settings hosts the `da settings` command tree. It splits out from
// the parent commands/ package per plan root-command-decomposition (t10b),
// mirroring the agents/ and skills/ subpackages. Canonical list/show/remove
// flows route through commands/internal/cmdutil so the rules/mcp/settings
// trio share one implementation of the resource-command shape.
package settings

import (
	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/spf13/cobra"
)

// GlobalFlags mirrors the subset of commands.Flags used by settings subcommands.
// Kept as a parallel type to commands.GlobalFlags so the settings subpackage has
// no import on the parent commands/ package.
type GlobalFlags struct {
	DryRun bool
	Yes    bool
	Force  bool
}

// Deps carries UX helpers from the commands package without an import cycle.
// Mirrors agents.Deps / skills.Deps so the extracted subpackages share the
// same wiring shape. Only fields actually consumed by settings subcommands
// are present.
type Deps struct {
	Flags                 GlobalFlags
	ErrorWithHints        func(message string, hints ...string) error
	UsageError            func(message string, hints ...string) error
	MaximumNArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
	ExactArgsWithHints    func(n int, hints ...string) cobra.PositionalArgs
}

// canonicalFlags projects Deps.Flags into the cmdutil flag shape so the
// shared RunCanonical{List,Show,Remove} helpers don't need to know about
// the settings-local GlobalFlags type.
func (d Deps) canonicalFlags() cmdutil.CanonicalCmdFlags {
	return cmdutil.CanonicalCmdFlags{
		DryRun: d.Flags.DryRun,
		Yes:    d.Flags.Yes,
		Force:  d.Flags.Force,
	}
}
