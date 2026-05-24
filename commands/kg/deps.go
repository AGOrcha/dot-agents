package kg

import "github.com/NikashPrakash/dot-agents/internal/graphstore"

// GlobalFlags mirrors the subset of commands.Flags used by kg subcommands.
type GlobalFlags struct {
	JSON   bool
	DryRun bool
}

// Deps carries UX helpers from commands without an import cycle.
//
// gcc3 / di-refactor OD-1: Store is a contract-typed handle whose provider
// owns pooling and serialization. The Deps struct is justified as a holder
// of this contract-typed handle — it is NOT the concurrency story (the
// provider behind the contract is). graphstore.Handle's Store() accessor is
// nil-safe: when unset, callers fall back to their existing direct-open
// path via openKGStore (this preserves behavior for the gcc3 pass, which
// only pins the boundary; wiring every call site to read from the Deps
// handle is the deferred follow-up tracked on di-refactor OD-1 / the kg-pkg
// task on seam-interface-di-migration). See internal/graphstore/CONTRACT.md
// "Deps boundary".
type Deps struct {
	Flags        GlobalFlags
	ExampleBlock func(lines ...string) string
	Store        graphstore.Handle
}
