package config

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// lockTokenRe is the STRICT grammar for a lock path segment or a deny-lock
// category/member token: a clean identifier — letters, digits, `_`, `-` — with no
// whitespace, brackets, dots, colons, or control characters. Real config keys
// (app types like `go-cli`, feature-flag names, profile slugs) are all clean
// identifiers, so this rejects only mistyped tokens. A token failing the grammar
// is a fail-closed resolve error, never silently trimmed or no-op'd (§15.9/D1a).
var lockTokenRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

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
	violSelfBlessing   = "self_blessing"
	violInertGrant     = "inert_grant"
	violForceAllow     = "force_allow"
	violZeroAuthLock   = "zero_authority_lock"
	violGrantOverwrite = "grant_overwrite"
	violOverlapLock    = "overlapping_lock"

	collisionValueLock = "value_lock"
	collisionDenyLock  = "deny_lock"
)

// validScope reports whether s names a known authority scope. A grant or other
// policy carrying an unknown scope is a malformed (fail-closed) input, not a
// silently-ignored one.
func validScope(s AuthorityScope) bool {
	switch s {
	case AuthProduct, AuthPublic, AuthRuntime, AuthProjectLocal,
		AuthUser, AuthRepo, AuthTeam, AuthOrg:
		return true
	default:
		return false
	}
}

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

// authorityLayer is one resolved layer paired with its authority scope and its
// pre-parsed, pre-validated policy (locks + grants), fed to Phase 1 in
// value-precedence order (lowest first). Parsing/validation happens before the
// pass (buildAuthorityLayers) so a malformed policy fails closed there.
type authorityLayer struct {
	id     string
	scope  AuthorityScope
	raw    map[string]any
	locks  PolicyLockSpec
	grants map[string]AuthorityScope
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
	owners := map[string]int{} // granter rank that set each honored grant
	var viols []AuthorityViolation
	for _, l := range layers {
		applyGrantBlock(l.scope, l.grants, grants, owners, &viols)
	}
	return grants, viols
}

// applyGrantBlock evaluates every grant a single layer declares against the
// write-guard, honoring or rejecting each, in sorted source order so the
// violation sequence is deterministic. A grant that passes the guard is recorded
// ONLY if the granter STRICTLY outranks any incumbent grant for that source —
// closing the downgrade/overwrite vector where a same-or-lower-rank later layer
// replaces a higher scope's grant.
func applyGrantBlock(granter AuthorityScope, block map[string]AuthorityScope, grants map[string]AuthorityScope, owners map[string]int, viols *[]AuthorityViolation) {
	g := AuthorityRankOf(granter)
	for _, src := range sortedGrantKeys(block) {
		conferred := block[src]
		if honored, v := evaluateGrant(granter, src, conferred); !honored {
			*viols = append(*viols, v)
			continue
		}
		if incumbent, ok := owners[src]; ok && g <= incumbent {
			*viols = append(*viols, AuthorityViolation{
				Kind: violGrantOverwrite, Source: src, Scope: conferred, Fatal: true,
				Reason: fmt.Sprintf("grant overwrite rejected: %q (rank %d) cannot replace an existing grant owned by rank %d", granter, g, incumbent),
			})
			continue
		}
		grants[src] = conferred
		owners[src] = g
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
//
// A grant is honored ONLY when the granter STRICTLY outranks the conferred scope
// (`g > c`, §15.9 item 4 / D1a :1316 "only a strictly-higher scope may write a
// grant"). A PEER (same rank) cannot confer its own rank — so no scope can spread
// its authority laterally to a source it controls. Conferring org-level authority
// onto an org source is the deferred trusted-root/governance-backend bootstrap
// path (§15.7), which does not flow through this peer guard.
//
// Outcomes:
//   - conferred rank == 0: not honored (a value-only scope carries no authority).
//   - granter rank > conferred rank: HONORED.
//   - granter rank == 0 below a real conferred scope: INERT (recorded, non-fatal)
//     — a foreign/public source's claim is ignored unless co-signed by a trusted
//     root.
//   - a SCOPED layer (user/repo/team/org) at or below the conferred rank: FATAL
//     self-blessing rejection (peer or elevation).
func evaluateGrant(granter AuthorityScope, src string, conferred AuthorityScope) (bool, AuthorityViolation) {
	g := AuthorityRankOf(granter)
	c := AuthorityRankOf(conferred)
	switch {
	case c == 0:
		return false, AuthorityViolation{
			Kind: violInertGrant, Source: src, Scope: conferred,
			Reason: "conferred scope carries no authority; grant ignored",
		}
	case g > c:
		return true, AuthorityViolation{}
	case g == 0:
		return false, AuthorityViolation{
			Kind: violInertGrant, Source: src, Scope: conferred,
			Reason: fmt.Sprintf("value-only source cannot carry %q authority; grant inert (needs a trusted-root co-sign)", conferred),
		}
	default:
		return false, AuthorityViolation{
			Kind: violSelfBlessing, Source: src, Scope: conferred, Fatal: true,
			Reason: fmt.Sprintf("self-elevation rejected: %q (rank %d) cannot grant %q (rank %d) authority (only a strictly-higher scope may grant)", granter, g, conferred, c),
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

// parseGrants decodes and VALIDATES a layer's authority_grants block fail-closed.
// An absent block yields (nil, nil). A present block that is not an object of
// known-scope strings is a malformed-policy ERROR — never silently skipped, since
// a silently-ignored authority grant is fail-open.
func parseGrants(raw map[string]any) (map[string]AuthorityScope, error) {
	v, ok := raw["authority_grants"]
	if !ok {
		return nil, nil
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("malformed authority_grants: must be an object")
	}
	out := make(map[string]AuthorityScope, len(obj))
	for _, src := range sortedAnyKeys(obj) {
		s, ok := obj[src].(string)
		if !ok {
			return nil, fmt.Errorf("malformed authority_grants[%q]: must be a scope string", src)
		}
		scope := AuthorityScope(s)
		if !validScope(scope) {
			return nil, fmt.Errorf("malformed authority_grants[%q]: unknown scope %q", src, s)
		}
		out[src] = scope
	}
	return out, nil
}

func sortedAnyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// validateLockSpec checks a decoded lock spec fail-closed. A value_lock path or a
// deny_lock that is malformed is a policy error — a typo'd lock that silently
// protected nothing would be fail-open.
func validateLockSpec(spec PolicyLockSpec) error {
	for path := range spec.ValueLocks {
		if err := validFieldPath(path); err != nil {
			return fmt.Errorf("malformed value_lock path %q: %w", path, err)
		}
	}
	for _, d := range spec.DenyLocks {
		if err := validateDenyLock(d); err != nil {
			return fmt.Errorf("malformed deny_lock %q: %w", d, err)
		}
	}
	return nil
}

// validateDenyLock checks a "category:member" deny-lock fail-closed: EXACTLY one
// `:` separator, with a valid field-path category and a clean-identifier member on
// either side. Whitespace ("skills :risky", "skills: risky"), bracket notation,
// missing/empty tokens, or extra colons are all errors.
func validateDenyLock(raw string) error {
	if strings.Count(raw, ":") != 1 {
		return fmt.Errorf("want exactly one ':' separating category:member")
	}
	field, member, ok := splitDenyLock(raw)
	if !ok {
		return fmt.Errorf("empty category or member")
	}
	if err := validFieldPath(field); err != nil {
		return err
	}
	return validToken(member, "member")
}

// validFieldPath checks a dot-separated lock path fail-closed: every segment must
// be a clean identifier (lockTokenRe), and array-index segments (all-digit) are
// rejected as unsupported in v1 (§15.9/D1a) — array-index paths are NOT silently
// treated as map keys.
func validFieldPath(path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	for _, seg := range strings.Split(path, ".") {
		if err := validToken(seg, "path segment"); err != nil {
			return err
		}
		if isArrayIndex(seg) {
			return fmt.Errorf("array-index segment %q is unsupported in v1", seg)
		}
	}
	return nil
}

// validToken enforces the strict identifier grammar (lockTokenRe) on a single
// path segment or deny-lock member, rejecting empty/whitespace/bracket/colon/
// control-char tokens with a clear error.
func validToken(tok, kind string) error {
	if tok == "" {
		return fmt.Errorf("empty %s", kind)
	}
	if !lockTokenRe.MatchString(tok) {
		return fmt.Errorf("%s %q is not a clean identifier (letters/digits/_/- only, no whitespace or brackets)", kind, tok)
	}
	return nil
}

// isArrayIndex reports whether a non-empty segment is all digits (an array
// index). Callers reject empty segments before this.
func isArrayIndex(seg string) bool {
	for _, r := range seg {
		if r < '0' || r > '9' {
			return false
		}
	}
	return seg != ""
}

// overlappingLockPaths returns a fatal violation for every pair of effective
// value-lock paths where one is a strict prefix of the other (e.g. "features" and
// "features.graph_bridge"). Such an overlap is ambiguous — a broad lock and a
// nested lock on the same subtree have no well-defined precedence — so it is
// rejected fail-closed rather than resolved in nondeterministic map order
// (§15.9/D1a). Paths are sorted so the violation sequence is deterministic.
func overlappingLockPaths(locks map[string]effectiveValueLock) []AuthorityViolation {
	paths := make([]string, 0, len(locks))
	for p := range locks {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var viols []AuthorityViolation
	for i := 0; i < len(paths); i++ {
		for j := i + 1; j < len(paths); j++ {
			if isPathPrefix(paths[i], paths[j]) {
				viols = append(viols, AuthorityViolation{
					Kind: violOverlapLock, Fatal: true,
					Reason: fmt.Sprintf("ambiguous overlapping value-locks: %q is a prefix of %q", paths[i], paths[j]),
				})
			}
		}
	}
	return viols
}

// isPathPrefix reports whether dot-path a is a STRICT segment-wise prefix of b.
func isPathPrefix(a, b string) bool {
	as := strings.Split(a, ".")
	bs := strings.Split(b, ".")
	if len(as) >= len(bs) {
		return false
	}
	for i := range as {
		if as[i] != bs[i] {
			return false
		}
	}
	return true
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
