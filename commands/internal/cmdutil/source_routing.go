package cmdutil

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/AGOrcha/dot-agents/internal/config"
)

// source_routing.go owns the --scope/--source routing seam shared by the
// mutating canonical commands (skills/agents/hooks/rules/mcp/settings and
// `sync`). Per config-distribution-model §7A.6 and the config-v2-coherence
// proposal §8 governance decision, those commands do NOT each grow a per-store
// subtree; instead they take a (scope, source) pair, resolve it to a write
// target, and run a single editability check before mutating.
//
// This file is the ONE place that:
//   - declares the --scope / --source cobra flag pair (FlagScope/FlagSource);
//   - resolves the flag values to a config.WriteTarget (default scope local);
//   - CONSUMES the governance seam (config.Checker / config.WriteAuthorizer
//     from internal/config/editability.go) to gate a mutating op.
//
// It deliberately does NOT redefine the authorizer, the scopes, or the verdict
// — those live in internal/config. Wiring this Router into each individual CRUD
// command is a follow-on; this task ships only the routing helper.

const (
	// FlagScope is the long name of the ownership-scope flag.
	FlagScope = "scope"
	// FlagSource is the long name of the source-id flag.
	FlagSource = "source"
)

// errNoTarget is the message used when an editability check is requested
// without a resolved target. Hoisted to a const so the literal is not
// duplicated across the resolve/check paths.
const errNoTarget = "no write target resolved — call ResolveTarget before the editability check"

// ScopeSourceFlags holds the raw --scope/--source flag values for a mutating
// command. Bind it once per command with BindScopeSourceFlags; the cobra flag
// package writes the parsed values back into these fields.
type ScopeSourceFlags struct {
	// Scope is the requested ownership scope (local/team/org/project). Empty
	// resolves to the default, ScopeLocal.
	Scope string
	// Source is the stable local source id (the `id` in the `sources` array,
	// §3) the write targets. Required for governed scopes; for local it names
	// the personal store and may be left to the command's default.
	Source string
}

// BindScopeSourceFlags registers --scope and --source on cmd, writing parsed
// values back into f. Every mutating canonical command calls this so the flag
// surface stays identical across the family.
func BindScopeSourceFlags(cmd *cobra.Command, f *ScopeSourceFlags) {
	cmd.Flags().StringVar(
		&f.Scope, FlagScope, string(config.ScopeLocal),
		"ownership scope to write to: local | team | org | project",
	)
	cmd.Flags().StringVar(
		&f.Source, FlagSource, "",
		"id of the source to write to (matches an entry in the `sources` array)",
	)
}

// RoutedTarget is what a command writes to once routing succeeds: the resolved
// ownership scope plus the source id. It is the command-facing projection of
// config.WriteTarget — the descriptor the caller threads into its write path.
type RoutedTarget struct {
	// Scope is the resolved ownership scope.
	Scope config.EditScope
	// SourceID is the stable local source identifier the write lands in.
	SourceID string
	// Owner names the team/org that owns a governed or owned-project source.
	// Empty for local and personal-project targets.
	Owner string
}

// writeTarget projects the RoutedTarget into the governance seam's
// config.WriteTarget shape.
func (t RoutedTarget) writeTarget() config.WriteTarget {
	return config.WriteTarget{
		ID:    t.SourceID,
		Scope: t.Scope,
		Owner: t.Owner,
	}
}

// Router maps --scope/--source flags to a RoutedTarget and gates mutating ops
// through the governance seam. Construct it with NewRouter, binding the
// config.Checker the host wired (a nil checker uses the safe default-prompt
// behavior for governed scopes).
type Router struct {
	checker *config.Checker
}

// NewRouter returns a Router that runs editability checks through checker. A
// nil checker is replaced with config.NewChecker(nil) so governed scopes still
// fail closed to a prompt rather than panicking.
func NewRouter(checker *config.Checker) *Router {
	if checker == nil {
		checker = config.NewChecker(nil)
	}
	return &Router{checker: checker}
}

// ResolveTarget maps the parsed flags to a RoutedTarget, applying the default
// scope (local) and validating the requested scope. owner names the owning
// team/org for a governed or owned-project source; pass "" for local/personal.
//
// It performs NO editability check — that is CheckWrite's job — so a command
// can resolve once and reuse the target.
func ResolveTarget(f ScopeSourceFlags, owner string) (RoutedTarget, error) {
	scope := config.EditScope(f.Scope)
	if f.Scope == "" {
		scope = config.ScopeLocal
	}
	if !scope.Valid() {
		return RoutedTarget{}, fmt.Errorf(
			"invalid --%s %q: want local, team, org, or project", FlagScope, f.Scope,
		)
	}
	return RoutedTarget{
		Scope:    scope,
		SourceID: f.Source,
		Owner:    owner,
	}, nil
}

// CheckWrite runs the editability check for target on behalf of principal,
// CONSUMING the governance seam. It returns:
//
//   - the verdict and nil error when the write is ALLOWED;
//   - the verdict and a non-nil error when the write is DENIED or requires a
//     PROMPT, with the verdict's operator-facing Reason surfaced in the error.
//
// Callers that want to honor a prompt (confirm-then-write) inspect the returned
// verdict's Decision; callers that treat any non-allow as a hard stop can rely
// on the returned error alone.
func (r *Router) CheckWrite(p config.Principal, target RoutedTarget) (config.Verdict, error) {
	if target.Scope == "" {
		return config.Verdict{}, errors.New(errNoTarget)
	}
	verdict := r.checker.CanWrite(p, target.writeTarget())
	switch verdict.Decision {
	case config.DecisionAllow:
		return verdict, nil
	case config.DecisionPrompt:
		return verdict, fmt.Errorf(
			"write to source %q (%s scope) needs confirmation: %s",
			target.SourceID, verdict.Scope, verdict.Reason,
		)
	default:
		return verdict, fmt.Errorf(
			"write to source %q (%s scope) denied: %s",
			target.SourceID, verdict.Scope, verdict.Reason,
		)
	}
}

// Route is the one-call convenience wrapping ResolveTarget + CheckWrite for the
// common path: resolve the flags, then gate the write. It returns the resolved
// target alongside the verdict so an allowed caller writes straight to it, and a
// prompt-honoring caller can re-check the verdict's Decision.
func (r *Router) Route(f ScopeSourceFlags, p config.Principal, owner string) (RoutedTarget, config.Verdict, error) {
	target, err := ResolveTarget(f, owner)
	if err != nil {
		return RoutedTarget{}, config.Verdict{}, err
	}
	verdict, err := r.CheckWrite(p, target)
	return target, verdict, err
}
