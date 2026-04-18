package workflow

import "github.com/spf13/cobra"

// GlobalFlags mirrors the subset of commands.Flags read by workflow subcommands at runtime.
type GlobalFlags struct {
	JSON func() bool
	Yes  func() bool
}

// Deps carries UX helpers and sentinels from package commands without an import cycle.
type Deps struct {
	Flags                 GlobalFlags
	ErrNoProject          error
	ErrorWithHints        func(message string, hints ...string) error
	UsageError            func(message string, hints ...string) error
	NoArgsWithHints       func(hints ...string) cobra.PositionalArgs
	ExactArgsWithHints    func(n int, hints ...string) cobra.PositionalArgs
	MaximumNArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
	ExampleBlock          func(lines ...string) string
}

var deps Deps

// InitTestDeps wires workflow package dependencies for tests. Call from TestMain before m.Run().
func InitTestDeps(d Deps) {
	deps = d
}
