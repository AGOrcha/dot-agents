package config

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// authority.go implements the §15 D1a authority/value two-axis resolver — the
// policy-authority pass (Phase 1) that runs AHEAD of the existing value-merge
// (Phase 2). It is the security core of config-distribution-model §15.9 items
// 1-6: the AUTHORITY-RANK total order, the source-authority registry write-guard
// (no self-blessing), locked-field collisions with provenance, cross-authority
// deny-overrides, and the two explicitly-named orderings (authority_rank vs
// value_precedence).
//
// The pass is ADDITIVE: a layer set that declares no locks and no
// authority_grants produces exactly the pre-§15 value-merge result, so shipped
// behavior is unchanged. It only does work when a manifest carries `locks` or
// `authority_grants`.

// AuthorityScope is one rung on the §15 D1a AUTHORITY-RANK axis. It is distinct
// from EditScope (the editability projection in editability.go): authority
// governs who may emit locks/caps, editability governs who may write a source.
type AuthorityScope string

const (
	// AuthProduct is the floor: ships defaults, carries ZERO authority, and is
	// overridden by everything (§15 D1a "SPECIAL scopes").
	AuthProduct AuthorityScope = "product"
	// AuthPublic is a value-only, zero-authority scope. It may supply values at
	// the lowest precedence, but ANY authority/lock claim it ships is inert
	// unless co-signed by a trusted root (the supply-chain-signing guard). It is
	// also the default authority of an UNGRANTED imported (extends) layer.
	AuthPublic AuthorityScope = "public"
	// AuthRuntime is the highest value-precedence, zero-authority scope: it sets
	// values but never locks.
	AuthRuntime AuthorityScope = "runtime"
	// AuthProjectLocal is the uncommitted per-project overlay: value-only, zero
	// authority (§15 D9 / D1a).
	AuthProjectLocal AuthorityScope = "project-local"
	// AuthUser is the LOWEST authority rung on the chain — beneath repo. A user
	// scope may emit locks only at its own scope; every higher scope, including
	// repo, overrides it (a personal scope can never constrain a shared repo).
	AuthUser AuthorityScope = "user"
	// AuthRepo is a shared committed scope. It MAY set its own local guardrails
	// but ranks below team, so a team lock still binds the repo.
	AuthRepo AuthorityScope = "repo"
	// AuthTeam ranks above repo and below org.
	AuthTeam AuthorityScope = "team"
	// AuthOrg is the highest authority rung: its locks/caps are absolute over
	// every lower scope.
	AuthOrg AuthorityScope = "org"
)

// AuthorityRankOf returns the §15 D1a AUTHORITY-RANK weight for a scope. The
// total order is org > team > repo > user; every value-only scope (product,
// public, runtime, project-local) carries rank 0 (zero authority — it may set
// values but never emit a binding lock). A higher rank binds every lower one.
func AuthorityRankOf(s AuthorityScope) int {
	switch s {
	case AuthOrg:
		return 4
	case AuthTeam:
		return 3
	case AuthRepo:
		return 2
	case AuthUser:
		return 1
	default:
		return 0
	}
}

// ScopeOrdering names the TWO co-existing orderings the §15 D1a scope axis
// carries. They are deliberately two explicitly-named fields (§15.9 item 6,
// F1.1) — NOT one reused `scope_chain` — because they govern different
// questions and run in two resolver phases.
type ScopeOrdering struct {
	// AuthorityRank is the Phase-1 total order (highest authority first): who
	// may emit locks/caps. deny-overrides; higher binds lower.
	AuthorityRank []AuthorityScope `json:"authority_rank"`
	// ValuePrecedence is the Phase-2 value-merge order (lowest precedence
	// first): whose VALUE wins within the locks Phase 1 leaves surviving. This
	// is the verbatim preservation of the original D1 chain.
	ValuePrecedence []AuthorityScope `json:"value_precedence"`
}

// CanonicalScopeOrdering returns the one canonical §15 D1a ordering. It is a
// fixed substrate constant, NOT a manifest-overridable field: letting a lower
// scope redefine authority_rank would be an authority-escalation hole, so the
// orderings live in code, not in user-writable config.
func CanonicalScopeOrdering() ScopeOrdering {
	return ScopeOrdering{
		AuthorityRank: []AuthorityScope{AuthOrg, AuthTeam, AuthRepo, AuthUser},
		ValuePrecedence: []AuthorityScope{
			AuthProduct, AuthUser, AuthOrg, AuthTeam, AuthRepo, AuthProjectLocal, AuthRuntime,
		},
	}
}

// PolicyLockSpec is a layer's declared §15 D1a authority-axis locks. A layer may
// value-lock a field (pin its value, rejecting lower-scope writes) or deny-lock
// a set member (force it out, with deny-overrides). force_allow is INVALID — its
// presence is a validation error (there is no force-allow: a lower scope can
// never punch a capability through a higher deny).
type PolicyLockSpec struct {
	// ValueLocks pins a field path to a value. A lower-authority scope that
	// writes a different value has its write rejected (the lock wins).
	ValueLocks map[string]json.RawMessage `json:"value_locks,omitempty"`
	// DenyLocks subtracts set members, each "field:member" (e.g.
	// "skills:risky-skill"). deny-overrides: a lower-scope union-add of the
	// member is dropped.
	DenyLocks []string `json:"deny_locks,omitempty"`
	// ForceAllow is ALWAYS invalid. Any entry is a resolve-time validation
	// error — the no-force-allow invariant.
	ForceAllow []string `json:"force_allow,omitempty"`
}

// IsZero reports whether the spec declares nothing.
func (p *PolicyLockSpec) IsZero() bool {
	if p == nil {
		return true
	}
	return len(p.ValueLocks) == 0 && len(p.DenyLocks) == 0 && len(p.ForceAllow) == 0
}

// Violation kinds, recorded as stable strings so explain/audit can branch.
const (
	violSelfBlessing = "self_blessing"
	violInertGrant   = "inert_grant"
	violForceAllow   = "force_allow"
	violZeroAuthLock = "zero_authority_lock"

	collisionValueLock = "value_lock"
	collisionDenyLock  = "deny_lock"
)

// AuthorityViolation is a fatal or recorded breach surfaced by Phase 1. Fatal
// violations (self-elevation, force-allow) abort the resolve fail-closed;
// non-fatal ones (an inert public grant, a zero-authority lock) are recorded.
type AuthorityViolation struct {
	Kind   string         `json:"kind"`
	Source string         `json:"source,omitempty"`
	Scope  AuthorityScope `json:"scope,omitempty"`
	Reason string         `json:"reason"`
	Fatal  bool           `json:"fatal"`
}

// LockCollision records a rejected lower-scope write surfaced through
// `da config explain` (§15.9 item 2). It carries the attempted value, the
// winning (locked) value, and the owning scope.
type LockCollision struct {
	Field     string         `json:"field"`
	Attempted any            `json:"attempted"`
	Winning   any            `json:"winning"`
	Owner     AuthorityScope `json:"owner"`
	OwnerRank int            `json:"owner_rank"`
	Kind      string         `json:"kind"`
}

// authorityLayer is one resolved layer paired with its authority scope and
// declared locks, fed to Phase 1 in value-precedence order (lowest first).
type authorityLayer struct {
	id    string
	scope AuthorityScope
	raw   map[string]any
	locks PolicyLockSpec
}

// effectiveValueLock is the winning value-lock for a field after the authority
// pass: the highest-authority owner's pinned value.
type effectiveValueLock struct {
	owner AuthorityScope
	rank  int
	value any
}

// effectiveDenyLock is the winning deny-lock for a set member.
type effectiveDenyLock struct {
	field  string
	member string
	owner  AuthorityScope
	rank   int
}

// authorityResult is the output of the policy-authority pass.
type authorityResult struct {
	valueLocks map[string]effectiveValueLock
	denyLocks  []effectiveDenyLock
	collisions []LockCollision
	violations []AuthorityViolation
}

// resolveAuthorityGrants applies the source-authority registry WRITE-GUARD
// (§15 D1a invariants (a)+(b), §15.9 item 4). Each layer's `authority_grants`
// confers a scope on a source ONLY when the WRITING layer's own authority is at
// least the conferred authority. A lower scope (user/repo/public) cannot grant
// itself or any source a HIGHER authority — that is self-elevation, a
// resolve-time rejection, NOT a silent no-op.
//
// Outcomes:
//   - granter rank >= conferred rank (and conferred > 0): HONORED.
//   - conferred rank == 0: not honored (a value-only scope carries no authority).
//   - granter rank == 0 (public/product/runtime/project-local) below a real
//     conferred scope: INERT (recorded, non-fatal) — a foreign/public source's
//     authority claim is ignored unless co-signed by a trusted root.
//   - a SCOPED lower layer (user/repo) elevating above its own rank: FATAL
//     self-blessing rejection.
func resolveAuthorityGrants(layers []authorityLayer) (map[string]AuthorityScope, []AuthorityViolation) {
	grants := map[string]AuthorityScope{}
	var viols []AuthorityViolation
	for _, l := range layers {
		raw, ok := l.raw["authority_grants"]
		if !ok {
			continue
		}
		decoded := decodeGrantBlock(raw)
		applyGrantBlock(l.scope, decoded, grants, &viols)
	}
	return grants, viols
}

// decodeGrantBlock coerces a raw authority_grants block into source→scope. A
// malformed block (not an object of scope strings) contributes nothing.
func decodeGrantBlock(raw any) map[string]AuthorityScope {
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]AuthorityScope, len(obj))
	for src, v := range obj {
		if s, ok := v.(string); ok {
			out[src] = AuthorityScope(s)
		}
	}
	return out
}

// applyGrantBlock evaluates every grant a single layer declares against the
// write-guard, honoring or rejecting each. It iterates sources in sorted order
// so the violation sequence is deterministic.
func applyGrantBlock(granter AuthorityScope, block map[string]AuthorityScope, grants map[string]AuthorityScope, viols *[]AuthorityViolation) {
	for _, src := range sortedGrantKeys(block) {
		conferred := block[src]
		honored, v := evaluateGrant(granter, src, conferred)
		if honored {
			grants[src] = conferred
			continue
		}
		*viols = append(*viols, v)
	}
}

func sortedGrantKeys(block map[string]AuthorityScope) []string {
	keys := make([]string, 0, len(block))
	for k := range block {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// evaluateGrant decides a single grant against the write-guard, returning the
// honored flag and (when not honored) the violation to record.
func evaluateGrant(granter AuthorityScope, src string, conferred AuthorityScope) (bool, AuthorityViolation) {
	g := AuthorityRankOf(granter)
	c := AuthorityRankOf(conferred)
	switch {
	case c == 0:
		return false, AuthorityViolation{
			Kind: violInertGrant, Source: src, Scope: conferred,
			Reason: "conferred scope carries no authority; grant ignored",
		}
	case g >= c:
		return true, AuthorityViolation{}
	case g == 0:
		return false, AuthorityViolation{
			Kind: violInertGrant, Source: src, Scope: conferred,
			Reason: fmt.Sprintf("value-only source cannot carry %q authority; grant inert (needs a trusted-root co-sign)", conferred),
		}
	default:
		return false, AuthorityViolation{
			Kind: violSelfBlessing, Source: src, Scope: conferred, Fatal: true,
			Reason: fmt.Sprintf("self-elevation rejected: %q (rank %d) cannot grant %q (rank %d) authority", granter, g, conferred, c),
		}
	}
}

// runAuthorityPass is the policy-authority pass (Phase 1). It validates
// force-allow, then folds every layer's value-locks and deny-locks into the
// effective set, with the highest-authority owner winning each field. A lock
// emitted by a zero-authority scope is recorded and dropped (it can bind
// nothing).
func runAuthorityPass(layers []authorityLayer) authorityResult {
	res := authorityResult{valueLocks: map[string]effectiveValueLock{}}
	for _, l := range layers {
		rank := AuthorityRankOf(l.scope)
		recordForceAllow(l, &res)
		if rank == 0 {
			recordZeroAuthorityLocks(l, &res)
			continue
		}
		foldValueLocks(l, rank, &res)
		foldDenyLocks(l, rank, &res)
	}
	return res
}

// recordForceAllow flags any force_allow entry as a fatal validation error
// (the no-force-allow invariant, §15.9 item 4) regardless of the layer's scope.
func recordForceAllow(l authorityLayer, res *authorityResult) {
	for _, fa := range l.locks.ForceAllow {
		res.violations = append(res.violations, AuthorityViolation{
			Kind: violForceAllow, Scope: l.scope, Fatal: true,
			Reason: fmt.Sprintf("force_allow %q is invalid: a lower scope can never punch a capability through a higher deny", fa),
		})
	}
}

// recordZeroAuthorityLocks notes that a value-only scope tried to emit a binding
// lock; the lock is dropped (it carries no authority) but recorded so explain
// can show why it did nothing.
func recordZeroAuthorityLocks(l authorityLayer, res *authorityResult) {
	if len(l.locks.ValueLocks) == 0 && len(l.locks.DenyLocks) == 0 {
		return
	}
	res.violations = append(res.violations, AuthorityViolation{
		Kind: violZeroAuthLock, Scope: l.scope,
		Reason: fmt.Sprintf("scope %q carries zero authority and cannot emit locks; ignored", l.scope),
	})
}

// foldValueLocks folds one layer's value-locks into the effective set. A higher
// (or equal-and-later) authority owner replaces a lower one.
func foldValueLocks(l authorityLayer, rank int, res *authorityResult) {
	for field, rawVal := range l.locks.ValueLocks {
		val := decodeLockValue(rawVal)
		if cur, ok := res.valueLocks[field]; ok && cur.rank > rank {
			continue
		}
		res.valueLocks[field] = effectiveValueLock{owner: l.scope, rank: rank, value: val}
	}
}

// foldDenyLocks folds one layer's deny-locks into the effective set, skipping
// malformed entries (those without a "field:member" shape).
func foldDenyLocks(l authorityLayer, rank int, res *authorityResult) {
	for _, raw := range l.locks.DenyLocks {
		field, member, ok := splitDenyLock(raw)
		if !ok {
			continue
		}
		res.denyLocks = append(res.denyLocks, effectiveDenyLock{
			field: field, member: member, owner: l.scope, rank: rank,
		})
	}
}

// splitDenyLock parses a "field:member" deny-lock entry.
func splitDenyLock(raw string) (string, string, bool) {
	idx := strings.Index(raw, ":")
	if idx <= 0 || idx == len(raw)-1 {
		return "", "", false
	}
	return raw[:idx], raw[idx+1:], true
}

// decodeLockValue unmarshals a value-lock's raw JSON into a comparable any.
func decodeLockValue(raw json.RawMessage) any {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	return v
}

// fatalViolations returns the subset of violations that must abort the resolve.
func fatalViolations(viols []AuthorityViolation) []AuthorityViolation {
	var out []AuthorityViolation
	for _, v := range viols {
		if v.Fatal {
			out = append(out, v)
		}
	}
	return out
}

// authorityError formats fatal violations into one resolve-time error so the
// rejection is loud, never a silent no-op.
func authorityError(viols []AuthorityViolation) error {
	reasons := make([]string, 0, len(viols))
	for _, v := range viols {
		reasons = append(reasons, v.Reason)
	}
	return fmt.Errorf("authority resolution rejected: %s", strings.Join(reasons, "; "))
}
