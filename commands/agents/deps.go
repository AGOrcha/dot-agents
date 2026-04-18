package agents

import "github.com/spf13/cobra"

// GlobalFlags mirrors the subset of commands.Flags used by agents subcommands.
type GlobalFlags struct {
	Yes bool
}

// Deps carries UX helpers from commands without an import cycle.
type Deps struct {
	Flags                 GlobalFlags
	ErrorWithHints        func(message string, hints ...string) error
	UsageError            func(message string, hints ...string) error
	MaximumNArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
	RangeArgsWithHints    func(min, max int, hints ...string) cobra.PositionalArgs
	ExactArgsWithHints    func(n int, hints ...string) cobra.PositionalArgs
}
