package kg

import (
	"fmt"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

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
//
// IO is the kgIO collaborator (interface-DI per docs/TEST_SEAMS.md) used
// by every kg handler that needs filesystem / serialization IO. Production
// wires stdKGIO{} via cmd.go; tests construct a fakeKGIO and assign it on
// the Deps value before invoking the handler. When IO is the zero value
// (e.g. legacy call sites that have not been updated), kgIOFrom(deps)
// substitutes stdKGIO{} so the handlers never carry a nil collaborator.
type Deps struct {
	Flags          GlobalFlags
	ExampleBlock   func(lines ...string) string
	Store          graphstore.Handle
	IO             kgIO
	UsageError     func(message string, hints ...string) error
	ErrorWithHints func(message string, hints ...string) error
}

// kgUsageError emits deps.UsageError when wired (the commands package's shared
// usage-class writer), otherwise falls back to a plain message so zero-Deps
// tests still get the primary text. Mirrors agents' agentUserError.
func kgUsageError(deps Deps, message string, hints ...string) error {
	if deps.UsageError != nil {
		return deps.UsageError(message, hints...)
	}
	return fmt.Errorf("%s", message)
}

// kgIOFrom returns deps.IO when set, otherwise the production stdKGIO{}.
// Every kg handler / helper that needs to derive an io for downstream calls
// uses this so the test fixtures and the Cobra wiring share one fallback
// rule.
func kgIOFrom(deps Deps) kgIO {
	if deps.IO == nil {
		return stdKGIO{}
	}
	return deps.IO
}
