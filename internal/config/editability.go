package config

import "fmt"

// Editability answers the single question "can principal P write source S?".
//
// Per config-distribution-model §7A.6 and the config-v2-coherence proposal §8
// governance open decision, CRUD (skills/agents/hooks/rules/mcp/settings) and
// `sync` route a write to a source plus an editability check — there are no
// per-store command trees. This file owns that check as a policy-backend-
// AGNOSTIC seam: the routing layer (downstream ch-scope-source-routing)
// CONSUMES this interface; org/team policy backends PLUG INTO it via the
// adapter-contract pattern (mirroring graph-backend-adapter-contract). Wiring a
// concrete backend, and the routing that calls this, are explicitly out of
// scope here — this is the interface plus a safe default impl only.
//
// The editability tiers (proposal §8 / §9 locked decisions):
//
//   - local   → always writable (the personal, machine-local asset store)
//   - team    → governed: a pluggable policy backend decides
//   - org     → governed: a pluggable policy backend decides
//   - project → derives: team/org-owned project defers to that governance;
//     an unowned (personal) project is local-writable
//
// No backend is wired yet, so a governed source with no backend resolves to a
// SAFE default (deny, surfaced as a prompt) rather than silently allowing a
// write to org/team-owned policy.

// EditScope is the ownership scope of a source, as it bears on who may write
// it. It is the editability-relevant projection of the precedence-scope axis in
// §7A.1; it is deliberately NOT the full precedence ladder (product/user/
// runtime never receive CRUD writes through this seam).
type EditScope string

const (
	// ScopeLocal is the personal, machine-local asset store. Always writable
	// by its owner — the `.git/config`/project-local-overlay analog.
	ScopeLocal EditScope = "local"
	// ScopeTeam is a team-owned source. Writes are governed by the team's
	// policy backend.
	ScopeTeam EditScope = "team"
	// ScopeOrg is an org-owned source. Writes are governed by the org's
	// policy backend.
	ScopeOrg EditScope = "org"
	// ScopeProject is a project-owned source. Its editability DERIVES: a
	// project owned by a team/org defers to that governance; otherwise it is
	// personal and local-writable.
	ScopeProject EditScope = "project"
)

// Valid reports whether s is a known editability scope.
func (s EditScope) Valid() bool {
	switch s {
	case ScopeLocal, ScopeTeam, ScopeOrg, ScopeProject:
		return true
	default:
		return false
	}
}

// Principal is the actor requesting a write. It is intentionally minimal: a
// stable id plus the groups/teams it belongs to, which is all a policy backend
// needs to answer ownership/membership questions. Backends may carry richer
// identity out of band; this seam does not model auth.
type Principal struct {
	// ID is the stable identifier of the actor (e.g. a user handle).
	ID string
	// Groups are the team/org memberships the principal holds. A governed
	// source whose Owner is in Groups is one the principal may be authorized
	// to write — the policy backend has the final say.
	Groups []string
}

// WriteTarget identifies the destination of a write through the editability
// seam. It is distinct from the wire-format Source (agentsrc.go): a Source
// declares transport/versioning; a WriteTarget carries the ownership SCOPE and
// owner that govern who may write it.
type WriteTarget struct {
	// ID is the stable local source identifier (the `id` in the `sources`
	// array, §3) used by `--source`.
	ID string
	// Scope is the source's editability scope (ownership tier).
	Scope EditScope
	// Owner names the team/org that owns a governed (team/org) source, or the
	// owner of a project source when project ownership is a team/org. Empty
	// Owner on a project scope means a personal/unowned project, which derives
	// to local-writable.
	Owner string
}

// Decision is the outcome of an editability check.
type Decision string

const (
	// DecisionAllow means the write is permitted.
	DecisionAllow Decision = "allow"
	// DecisionDeny means the write is refused.
	DecisionDeny Decision = "deny"
	// DecisionPrompt means the write requires explicit confirmation before it
	// proceeds — the safe default when a source is governed but no policy
	// backend is wired to decide.
	DecisionPrompt Decision = "prompt"
)

// Verdict is the full result of an editability check: a Decision plus a
// human-readable Reason and the Scope that drove it, so callers (and `config
// explain`/`doctor`) can render WHY a write was allowed, denied, or gated.
type Verdict struct {
	Decision Decision
	// Reason explains the decision in operator-facing terms.
	Reason string
	// Scope is the editability scope that produced the decision. For a derived
	// project source this is the scope it derived TO (ScopeLocal for a personal
	// project, the governing tier for an owned one).
	Scope EditScope
}

// Allowed reports whether the verdict permits the write outright (Decision is
// DecisionAllow). A DecisionPrompt is NOT allowed without confirmation.
func (v Verdict) Allowed() bool {
	return v.Decision == DecisionAllow
}

// PolicyBackend is the policy-backend-AGNOSTIC seam org/team governance plugs
// into (the adapter-contract pattern). A backend answers, for a governed
// (team/org) source, whether the principal may write it. The default Checker
// calls a backend only for governed scopes; local and personal-project writes
// never reach a backend.
//
// Backends are intentionally NOT registered here — registration/selection is a
// downstream concern. This file ships only the contract and a nil-safe default.
type PolicyBackend interface {
	// CanWrite returns the verdict for a governed source. Implementations
	// should return DecisionAllow/DecisionDeny per their policy; returning a
	// non-nil error signals the backend could not reach a decision (e.g. the
	// policy service was unreachable), which the Checker treats as a safe
	// fail-closed prompt rather than an allow.
	CanWrite(p Principal, s WriteTarget) (Verdict, error)
}

// Checker decides editability. It owns the scope-derivation rules and delegates
// the governed (team/org) tiers to an optional PolicyBackend. A zero Checker
// (nil Backend) is valid and SAFE: governed sources resolve to DecisionPrompt
// (deny-then-confirm), never a silent allow.
type Checker struct {
	// Backend is the pluggable policy implementation. When nil, no backend is
	// wired and governed scopes fall back to the safe default.
	Backend PolicyBackend
}

// NewChecker returns a Checker bound to backend. Passing nil yields the safe
// default-deny-or-prompt behavior for governed scopes.
func NewChecker(backend PolicyBackend) *Checker {
	return &Checker{Backend: backend}
}

// CanWrite answers "can principal p write source s?".
//
// Rules (proposal §8 / §9):
//
//   - local                → always allow (personal).
//   - project with no Owner → derives to local: allow (personal project).
//   - project with an Owner → derives to that owner's governance: treated as a
//     governed write (team/org) and delegated to the backend.
//   - team/org             → governed: delegate to the backend; with no backend
//     wired (or a backend that errors), return the SAFE default DecisionPrompt.
//
// An unknown/empty scope is denied — callers must route through a known scope.
func (c *Checker) CanWrite(p Principal, s WriteTarget) Verdict {
	switch s.Scope {
	case ScopeLocal:
		return Verdict{
			Decision: DecisionAllow,
			Reason:   "local scope is always writable (personal)",
			Scope:    ScopeLocal,
		}
	case ScopeProject:
		if s.Owner == "" {
			return Verdict{
				Decision: DecisionAllow,
				Reason:   "personal project (no owning team/org) is local-writable",
				Scope:    ScopeLocal,
			}
		}
		// Owned project derives to its owner's governance. Re-target the
		// source to a governed tier so the backend sees a governed source.
		return c.governed(p, s, ScopeProject)
	case ScopeTeam, ScopeOrg:
		return c.governed(p, s, s.Scope)
	default:
		return Verdict{
			Decision: DecisionDeny,
			Reason:   fmt.Sprintf("unknown editability scope %q", s.Scope),
			Scope:    s.Scope,
		}
	}
}

// governed resolves a governed (team/org or owned-project) write through the
// policy backend, applying the safe default when no backend is wired or the
// backend cannot decide. derived is the scope reported back to the caller so an
// owned project surfaces ScopeProject while team/org surface their own tier.
func (c *Checker) governed(p Principal, s WriteTarget, derived EditScope) Verdict {
	if c.Backend == nil {
		return Verdict{
			Decision: DecisionPrompt,
			Reason: fmt.Sprintf(
				"%s scope is governed but no policy backend is wired — confirm before writing %q",
				s.Scope, s.ID,
			),
			Scope: derived,
		}
	}
	v, err := c.Backend.CanWrite(p, s)
	if err != nil {
		return Verdict{
			Decision: DecisionPrompt,
			Reason: fmt.Sprintf(
				"policy backend could not decide for %q (%v) — fail-closed, confirm before writing",
				s.ID, err,
			),
			Scope: derived,
		}
	}
	// Preserve the backend's decision/reason; normalize the reported scope to
	// the derived tier when the backend left it unset.
	if v.Scope == "" {
		v.Scope = derived
	}
	if v.Reason == "" {
		v.Reason = fmt.Sprintf("policy backend decided %s for %q", v.Decision, s.ID)
	}
	return v
}
