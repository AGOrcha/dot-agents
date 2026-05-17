package hooks

import "github.com/spf13/cobra"

// GlobalFlags mirrors the subset of commands.Flags used by hooks subcommands.
type GlobalFlags struct {
	DryRun bool
	Yes    bool
	Force  bool
}

// Deps carries UX helpers from commands without an import cycle.
type Deps struct {
	Flags              GlobalFlags
	ErrorWithHints     func(message string, hints ...string) error
	UsageError         func(message string, hints ...string) error
	MaxArgsWithHints   func(n int, hints ...string) cobra.PositionalArgs
	ExactArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
}
