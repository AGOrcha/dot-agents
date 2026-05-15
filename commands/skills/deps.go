package skills

import "github.com/spf13/cobra"

// GlobalFlags mirrors the subset of commands.Flags used by skills subcommands.
// Kept as a parallel type to commands.GlobalFlags so the skills subpackage has
// no import on the parent commands/ package.
type GlobalFlags struct {
	Yes bool
}

// Deps carries UX helpers from the commands package without an import cycle.
// Mirrors agents.Deps so the two extracted subpackages share the same shape.
// Only fields actually consumed by skills subcommands are present.
type Deps struct {
	Flags                 GlobalFlags
	ErrorWithHints        func(message string, hints ...string) error
	UsageError            func(message string, hints ...string) error
	MaximumNArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
	RangeArgsWithHints    func(min, max int, hints ...string) cobra.PositionalArgs
	ExactArgsWithHints    func(n int, hints ...string) cobra.PositionalArgs
}
