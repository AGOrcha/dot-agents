// Package forkbproto is a SELF-CONTAINED faithful re-implementation of the
// dot-agents profile selector-merge engine (internal/config/profile.go +
// profile_resolver.go), MINIMIZED to exactly the semantics Fork B needs, and
// EXTENDED with a new ModelFamily selector dimension so the two-phase-resolution
// hazard can be exercised empirically.
//
// It does NOT import the real repo. Fidelity anchors (verified against the real
// source this session):
//   - ProfileSelector exact-match + wildcard-on-empty (matches), specificity =
//     count of constrained keys (profile.go:89-117) — here EXTENDED to count
//     ModelFamily as a 5th key.
//   - Value-axis ordering: Order first, then value-precedence rank of Scope,
//     then specificity, then ref (profile_resolver.go orderProfiles:288-307).
//   - Deep-map bundle merge: objects recurse, arrays/scalars replace
//     (resolver.go mergeMaps:324-347).
//   - PolicyMode narrow/replace fold (foldPolicy:135-147): replace resets
//     precedence+permissions, locks ALWAYS accumulate.
//   - Locks absolute: value-lock pins a field, deny-lock subtracts members; a
//     lock beats any fragment write (profile.go ProfileLock:209-223, Decision 4).
package forkbproto

import (
	"encoding/json"
	"fmt"
	"sort"
)

// AuthorityScope is the source-derived trust scope. Kept as a plain string; the
// value-precedence order is supplied explicitly (valuePrecedence).
type AuthorityScope string

// valuePrecedence is the canonical low→high VALUE ordering (mirrors §15
// ValuePrecedence: product→user→…→repo→project-local→runtime). Later wins.
var valuePrecedence = []AuthorityScope{"product", "user", "repo", "project-local", "runtime"}

func valueRank(s AuthorityScope) int {
	for i, v := range valuePrecedence {
		if v == s {
			return i
		}
	}
	return -1
}

// ModelFamilyKey is the NEW selector/context key Fork B adds. It is the sole
// addition to the closed selector-key set (real: profile.go selectorKeys:50-52).
const ModelFamilyKey = "model_family"

// ProfileSelector constrains where a fragment applies. Each present key is matched
// EXACTLY; an absent key is a wildcard (real profile.go:57-62). ModelFamily is the
// Fork-B extension.
type ProfileSelector struct {
	Role        string
	AppType     string
	Stage       string
	Harness     string
	ModelFamily string // Fork B: new selector dimension (H_B1).
}

// matches reports whether the selector applies to ctx (real matches:89-103),
// extended with the ModelFamily key. ctxFamily is the phase-resolved family
// available to the caller ("" in phase-1 → any family-scoped selector cannot
// match, which is what freezes phase-1 against family-scoped fragments).
func (s ProfileSelector) matches(ctx Context, ctxFamily string) bool {
	if s.Role != "" && s.Role != ctx.Role {
		return false
	}
	if s.AppType != "" && s.AppType != ctx.AppType {
		return false
	}
	if s.Stage != "" && s.Stage != ctx.Stage {
		return false
	}
	if s.Harness != "" && s.Harness != ctx.Harness {
		return false
	}
	if s.ModelFamily != "" && s.ModelFamily != ctxFamily {
		return false
	}
	return true
}

// specificity counts constrained (non-wildcard) selector keys (real:109-117),
// NOW counting ModelFamily as a 5th key.
func (s ProfileSelector) specificity() int {
	n := 0
	for _, v := range []string{s.Role, s.AppType, s.Stage, s.Harness, s.ModelFamily} {
		if v != "" {
			n++
		}
	}
	return n
}

// legacySpecificity is the PRE-Fork-B specificity (4 keys, no ModelFamily). Used
// only by the invariant-3 regression test to prove adding ModelFamily does not
// silently change tie outcomes for family-free profiles.
func (s ProfileSelector) legacySpecificity() int {
	n := 0
	for _, v := range []string{s.Role, s.AppType, s.Stage, s.Harness} {
		if v != "" {
			n++
		}
	}
	return n
}

// familyScoped reports whether this fragment SELECTS on model_family.
func (s ProfileSelector) familyScoped() bool { return s.ModelFamily != "" }

// ConfigProfile is one selector-scoped config fragment (real profile.go:125-157).
type ConfigProfile struct {
	Ref      string
	Scope    AuthorityScope
	Order    int
	Selector ProfileSelector
	Bundle   map[string]any
}

// PolicyMode — narrow (default) vs replace (real profile.go:190-202).
type PolicyMode string

const (
	PolicyModeNarrow  PolicyMode = "narrow"
	PolicyModeReplace PolicyMode = "replace"
)

// ProfileLock is an absolute deny/value pin (real profile.go:209-223). Selector
// optionally context-scopes it.
type ProfileLock struct {
	Field    string
	Deny     []string
	Value    json.RawMessage
	Selector ProfileSelector
	Owner    AuthorityScope
}

func (l ProfileLock) isValueLock() bool { return len(l.Value) > 0 }

// LayeringPolicy is a scope-attached merge policy (real profile.go:306-319). The
// Selector lets a policy be family-scoped (applies only once phase-1 froze the
// family) — this is how a family-scoped `replace` enters in phase-2.
type LayeringPolicy struct {
	Scope       AuthorityScope
	Precedence  []AuthorityScope
	Locks       []ProfileLock
	Permissions map[AuthorityScope][]string // scope→allowed field paths; nil = no cap
	Mode        PolicyMode
	Selector    ProfileSelector
}

// ProfileSet is the parsed input (real profile_resolver.go:101-104).
type ProfileSet struct {
	Profiles []ConfigProfile
	Policies []LayeringPolicy
}

// Context is the dispatch context (real ProfileContext:26-32). ScopeChain lists
// the present scopes; a fragment/policy whose scope is absent does not contribute.
type Context struct {
	Role       string
	AppType    string
	Stage      string
	Harness    string
	ScopeChain []AuthorityScope
}

func (c Context) inChain(s AuthorityScope) bool {
	for _, x := range c.ScopeChain {
		if x == s {
			return true
		}
	}
	return false
}

// EffectivePolicy is the merged phase policy (real:43-53).
type EffectivePolicy struct {
	Precedence  []AuthorityScope
	Permissions map[AuthorityScope][]string
	Locks       []ProfileLock
	Replaced    AuthorityScope
	Mode        PolicyMode
}

// Resolution is the engine output for a context.
type Resolution struct {
	Bundle       map[string]any
	Contributing []string
	Family       string // the frozen phase-1 family (empty for single-phase)
	Mode         PolicyMode
	ReplacedBy   AuthorityScope
	AppliedLocks []string // "field=value" / "field-deny[...]" for explain
}

// ---------------------------------------------------------------------------
// Shared building blocks (faithful to profile_resolver.go)
// ---------------------------------------------------------------------------

// resolveEffectivePolicy merges every in-chain, selector-matching policy low→high
// (real:112-147). ctxFamily gates family-scoped policies (empty in phase-1).
func resolveEffectivePolicy(set ProfileSet, ctx Context, ctxFamily string) EffectivePolicy {
	ps := make([]LayeringPolicy, 0, len(set.Policies))
	for _, p := range set.Policies {
		if ctx.inChain(p.Scope) && p.Selector.matches(ctx, ctxFamily) {
			ps = append(ps, p)
		}
	}
	sort.SliceStable(ps, func(i, j int) bool { return valueRank(ps[i].Scope) < valueRank(ps[j].Scope) })
	var eff EffectivePolicy
	eff.Mode = PolicyModeNarrow
	for _, p := range ps {
		foldPolicy(&eff, p)
	}
	return eff
}

// foldPolicy folds one higher policy (real foldPolicy:135-147). Locks ALWAYS
// accumulate; replace resets precedence+permissions.
func foldPolicy(eff *EffectivePolicy, p LayeringPolicy) {
	eff.Locks = append(eff.Locks, p.Locks...)
	if p.Mode == PolicyModeReplace {
		eff.Precedence = append([]AuthorityScope{}, p.Precedence...)
		eff.Permissions = p.Permissions
		eff.Replaced = p.Scope
		eff.Mode = PolicyModeReplace
		return
	}
	if len(p.Precedence) > 0 {
		eff.Precedence = append([]AuthorityScope{}, p.Precedence...)
	}
	eff.Permissions = narrowPermissions(eff.Permissions, p.Permissions)
}

// narrowPermissions intersects caps (real:155-169). nil next = no constraint;
// nil acc = universe.
func narrowPermissions(acc, next map[AuthorityScope][]string) map[AuthorityScope][]string {
	if next == nil {
		return acc
	}
	if acc == nil {
		return next
	}
	out := map[AuthorityScope][]string{}
	for scope, af := range acc {
		if nf, ok := next[scope]; ok {
			out[scope] = intersectFields(af, nf)
		}
	}
	return out
}

func intersectFields(a, b []string) []string {
	has := func(xs []string) bool {
		for _, x := range xs {
			if x == "*" {
				return true
			}
		}
		return false
	}
	if has(a) {
		return append([]string{}, b...)
	}
	if has(b) {
		return append([]string{}, a...)
	}
	bs := map[string]bool{}
	for _, f := range b {
		bs[f] = true
	}
	var out []string
	for _, f := range a {
		if bs[f] {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out
}

func mayChange(perm map[AuthorityScope][]string, scope AuthorityScope, field string) bool {
	if perm == nil {
		return true // OMITTED ⇒ no restriction (real mayChange nil receiver:263-266)
	}
	fields, ok := perm[scope]
	if !ok {
		return false
	}
	for _, f := range fields {
		if f == "*" || f == field {
			return true
		}
	}
	return false
}

// orderProfiles: Order → value-rank(Scope) → specificity → Ref (real:288-307).
func orderProfiles(profiles []ConfigProfile, policy EffectivePolicy) {
	order := policy.Precedence
	rank := func(s AuthorityScope) int {
		if len(order) > 0 {
			for i, x := range order {
				if x == s {
					return i
				}
			}
			return -1
		}
		return valueRank(s)
	}
	sort.SliceStable(profiles, func(i, j int) bool {
		if profiles[i].Order != profiles[j].Order {
			return profiles[i].Order < profiles[j].Order
		}
		if ri, rj := rank(profiles[i].Scope), rank(profiles[j].Scope); ri != rj {
			return ri < rj
		}
		if si, sj := profiles[i].Selector.specificity(), profiles[j].Selector.specificity(); si != sj {
			return si < sj
		}
		return profiles[i].Ref < profiles[j].Ref
	})
}

// mergeMaps: objects deep-merge, arrays/scalars replace (real resolver.go:324-347).
func mergeMaps(prev, next any) any {
	pm, pok := prev.(map[string]any)
	nm, nok := next.(map[string]any)
	if !nok || !pok {
		return next
	}
	out := make(map[string]any, len(pm)+len(nm))
	for k, v := range pm {
		out[k] = v
	}
	for k, v := range nm {
		if ex, ok := out[k]; ok {
			if _, both := ex.(map[string]any); both {
				out[k] = mergeMaps(ex, v)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// applyLocks applies the effective, context-matching locks over the merged bundle
// (absolute — real applyValueLocks/applyDenyLocks via §15, Decision 4). Applied
// LAST so no fragment write can beat a lock. suppressField, when set, prevents a
// lock from writing that field (used to keep phase-1's frozen family from being
// re-pinned in phase-2 — belt-and-suspenders; locks on model_family are honored
// in phase-1 already).
func applyLocks(bundle map[string]any, locks []ProfileLock, ctx Context, ctxFamily string) []string {
	var applied []string
	// Deterministic order: owner value-rank then field.
	ls := append([]ProfileLock{}, locks...)
	sort.SliceStable(ls, func(i, j int) bool {
		if ri, rj := valueRank(ls[i].Owner), valueRank(ls[j].Owner); ri != rj {
			return ri < rj
		}
		return ls[i].Field < ls[j].Field
	})
	for _, l := range ls {
		if !l.Selector.matches(ctx, ctxFamily) {
			continue
		}
		if l.isValueLock() {
			var v any
			_ = json.Unmarshal(l.Value, &v)
			setPath(bundle, l.Field, v)
			applied = append(applied, fmt.Sprintf("%s=value-lock(%v)", l.Field, v))
			continue
		}
		// deny-lock: subtract members from the field's []any list.
		if cur, ok := getPath(bundle, l.Field).([]any); ok {
			denied := map[string]bool{}
			for _, d := range l.Deny {
				denied[d] = true
			}
			var kept []any
			for _, m := range cur {
				if s, ok := m.(string); ok && denied[s] {
					continue
				}
				kept = append(kept, m)
			}
			setPath(bundle, l.Field, kept)
		}
		applied = append(applied, fmt.Sprintf("%s=deny-lock(%v)", l.Field, l.Deny))
	}
	return applied
}

// getPath/setPath handle simple dot-paths (e.g. "tools.allow").
func getPath(m map[string]any, path string) any {
	cur := any(m)
	for _, seg := range splitDot(path) {
		cm, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = cm[seg]
	}
	return cur
}

func setPath(m map[string]any, path string, v any) {
	segs := splitDot(path)
	cur := m
	for _, seg := range segs[:len(segs)-1] {
		nxt, ok := cur[seg].(map[string]any)
		if !ok {
			nxt = map[string]any{}
			cur[seg] = nxt
		}
		cur = nxt
	}
	cur[segs[len(segs)-1]] = v
}

func splitDot(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '.' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

func familyOf(bundle map[string]any) string {
	if v, ok := bundle[ModelFamilyKey].(string); ok {
		return v
	}
	return ""
}
