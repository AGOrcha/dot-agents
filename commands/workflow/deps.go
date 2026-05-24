package workflow

import (
	"github.com/NikashPrakash/dot-agents/internal/graphstore"
	"github.com/spf13/cobra"
)

// GlobalFlags mirrors the subset of commands.Flags read by workflow subcommands at runtime.
type GlobalFlags struct {
	JSON   func() bool
	Yes    func() bool
	DryRun func() bool
}

// Deps carries UX helpers and sentinels from package commands without an
// import cycle.
//
// gcc3 / di-refactor OD-1: Store is a contract-typed handle whose provider
// owns pooling and serialization. The package-level deps singleton is
// justified as a holder of this contract-typed handle — it is NOT the
// concurrency story (the provider behind the contract is). graphstore.Handle's
// Store() accessor is nil-safe: when unset, callers fall back to their
// existing direct-open path (preserving today's behavior). End-to-end wiring
// from the singleton to every call site is the deferred follow-up tracked on
// di-refactor OD-1. See internal/graphstore/CONTRACT.md "Deps boundary".
type Deps struct {
	Flags                 GlobalFlags
	ErrNoProject          error
	ErrorWithHints        func(message string, hints ...string) error
	UsageError            func(message string, hints ...string) error
	NoArgsWithHints       func(hints ...string) cobra.PositionalArgs
	ExactArgsWithHints    func(n int, hints ...string) cobra.PositionalArgs
	MaximumNArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
	ExampleBlock          func(lines ...string) string
	Store                 graphstore.Handle
}

var deps Deps

// InitTestDeps wires workflow package dependencies for tests. Call from TestMain before m.Run().
func InitTestDeps(d Deps) {
	deps = d
}
