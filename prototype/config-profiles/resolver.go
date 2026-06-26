package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// resolver.go — the two-phase selector-merge resolver.
//
// Phase 1: resolve the effective layering policy across the scope chain.
// Phase 2: selector-merge the matching profiles, governed by that policy.

// authorityRank gives each scope an authority weight. Higher binds lower.
func authorityRank(s Scope) int {
	switch s {
	case ScopeRepo:
		return 0
	case ScopeProject:
		return 1
	case ScopeUser:
		return 2
	case ScopeTeam:
		return 3
	case ScopeOrg:
		return 4
	}
	return -1
}

// ---------------------------------------------------------------------------
// Phase 1 — effective policy
// ---------------------------------------------------------------------------

// resolvePolicy merges all layering_policy units in scope-chain order. A
// higher-authority scope binds lower ones: its precedence wins, its locks are
// absolute, and its override_permissions cap what lower scopes may set.
func resolvePolicy(src SourceSet, ctx Context) ResolvedPolicy {
	policies := relevantPolicies(src, ctx)
	sort.SliceStable(policies, func(i, j int) bool {
		return authorityRank(policies[i].Scope) < authorityRank(policies[j].Scope)
	})

	out := ResolvedPolicy{
		OverridePermissions: map[Scope][]string{},
		lockAuthority:       map[string]Scope{},
	}
	for _, p := range policies {
		mergePolicyInto(&out, p)
	}
	return out
}

// relevantPolicies returns policies whose scope is part of the context chain.
func relevantPolicies(src SourceSet, ctx Context) []LayeringPolicy {
	inChain := map[Scope]bool{}
	for _, s := range ctx.ScopeChain {
		inChain[s] = true
	}
	out := []LayeringPolicy{}
	for _, p := range src.Policies {
		if inChain[p.Scope] {
			out = append(out, p)
		}
	}
	return out
}

// mergePolicyInto folds one policy (processed low->high authority) into out.
// Later (higher-authority) calls win on precedence and own their locks.
func mergePolicyInto(out *ResolvedPolicy, p LayeringPolicy) {
	if len(p.Precedence) > 0 {
		out.Precedence = append([]Scope{}, p.Precedence...)
	}
	for _, raw := range p.Locks {
		lk := parseLock(raw, p.Scope)
		out.Locks = append(out.Locks, lk)
		out.lockAuthority[lk.Raw] = p.Scope
	}
	for scope, fields := range p.OverridePermissions {
		out.OverridePermissions[scope] = append([]string{}, fields...)
	}
}

// parseLock parses "tools.deny:{Edit,Write}@role:reviewer".
func parseLock(raw string, owner Scope) Lock {
	lk := Lock{Raw: raw, Owner: owner}
	body := raw
	if at := strings.Index(raw, "@"); at >= 0 {
		body = raw[:at]
		lk.Selector = parseSelectorTail(raw[at+1:])
	}
	if colon := strings.Index(body, ":"); colon >= 0 {
		lk.Field = body[:colon]
		lk.Values = parseBraceList(body[colon+1:])
	} else {
		lk.Field = body
	}
	return lk
}

// parseSelectorTail parses "role:reviewer" (single key:value) into a Selector.
func parseSelectorTail(tail string) Selector {
	var sel Selector
	kv := strings.SplitN(tail, ":", 2)
	if len(kv) != 2 {
		return sel
	}
	switch kv[0] {
	case "role":
		sel.Role = kv[1]
	case "app_type":
		sel.AppType = kv[1]
	case "stage":
		sel.Stage = kv[1]
	case "harness":
		sel.Harness = kv[1]
	}
	return sel
}

func parseBraceList(s string) []string {
	s = strings.TrimPrefix(strings.TrimSuffix(s, "}"), "{")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Phase 2 — effective config, governed by the policy
// ---------------------------------------------------------------------------

// Resolve runs both phases for a context. Input order of profiles does NOT
// affect output (H1 determinism): profiles are sorted by policy precedence
// then by ref before merging.
func Resolve(src SourceSet, ctx Context) Resolved {
	policy := resolvePolicy(src, ctx)
	matched := matchingProfiles(src, ctx)
	orderProfiles(matched, policy)

	var b Bundle
	refs := make([]string, 0, len(matched))
	for _, pr := range matched {
		applyProfile(&b, pr, policy, ctx)
		refs = append(refs, pr.Ref)
	}
	applyLockDenies(&b, policy, ctx)
	normalizeBundle(&b)

	sort.Strings(refs)
	return Resolved{
		Bundle:          b,
		Contributing:    refs,
		Digest:          digestBundle(b),
		EffectivePolicy: policy,
	}
}

// matchingProfiles returns profiles whose selector matches ctx and whose scope
// is part of the chain.
func matchingProfiles(src SourceSet, ctx Context) []Profile {
	inChain := map[Scope]bool{}
	for _, s := range ctx.ScopeChain {
		inChain[s] = true
	}
	out := []Profile{}
	for _, p := range src.Profiles {
		if p.Selector.Scope != "" && !inChain[p.Selector.Scope] {
			continue
		}
		if selectorMatches(p.Selector, ctx) {
			out = append(out, p)
		}
	}
	return out
}

// orderProfiles sorts matched profiles low->high precedence so later entries
// win scalar conflicts (local-wins tail). Deterministic tiebreak on ref.
func orderProfiles(profiles []Profile, policy ResolvedPolicy) {
	rank := precedenceRanker(policy)
	sort.SliceStable(profiles, func(i, j int) bool {
		ri, rj := rank(profiles[i].scope()), rank(profiles[j].scope())
		if ri != rj {
			return ri < rj
		}
		return profiles[i].Ref < profiles[j].Ref
	})
}

// precedenceRanker returns a scope->rank function from the policy precedence,
// falling back to authorityRank when the policy is silent.
func precedenceRanker(policy ResolvedPolicy) func(Scope) int {
	if len(policy.Precedence) == 0 {
		return authorityRank
	}
	idx := map[Scope]int{}
	for i, s := range policy.Precedence {
		idx[s] = i
	}
	return func(s Scope) int {
		if r, ok := idx[s]; ok {
			return r
		}
		return -1 // scopes absent from precedence sort first (lowest)
	}
}

// selectorMatches reports whether a selector matches the context. Empty fields
// are wildcards.
func selectorMatches(sel Selector, ctx Context) bool {
	if sel.Role != "" && sel.Role != ctx.Role {
		return false
	}
	if sel.AppType != "" && sel.AppType != ctx.AppType {
		return false
	}
	if sel.Stage != "" && sel.Stage != ctx.Stage {
		return false
	}
	if sel.Harness != "" && sel.Harness != ctx.Harness {
		return false
	}
	return true
}

// applyProfile merges one profile's bundle into b, gated by override
// permissions and locks. Additive sets union; deny subtracts; scalar fields
// (model) overwrite. A profile may only change a field its scope is permitted
// to change, and may not re-grant a locked value.
func applyProfile(b *Bundle, pr Profile, policy ResolvedPolicy, ctx Context) {
	scope := pr.scope()
	if mayChange(policy, scope, "tools.allow") {
		b.Tools.Allow = unionMinusLocked(b.Tools.Allow, pr.Bundle.Tools.Allow, policy, ctx, "tools.allow")
	}
	if mayChange(policy, scope, "tools.deny") {
		b.Tools.Deny = union(b.Tools.Deny, pr.Bundle.Tools.Deny)
	}
	if mayChange(policy, scope, "skills.preload") {
		b.Skills.Preload = union(b.Skills.Preload, pr.Bundle.Skills.Preload)
	}
	if mayChange(policy, scope, "skills.allow") {
		b.Skills.Allow = union(b.Skills.Allow, pr.Bundle.Skills.Allow)
	}
	if mayChange(policy, scope, "skills.deny") {
		b.Skills.Deny = union(b.Skills.Deny, pr.Bundle.Skills.Deny)
	}
	if mayChange(policy, scope, "hooks") {
		b.Hooks = union(b.Hooks, pr.Bundle.Hooks)
	}
	if mayChange(policy, scope, "mcp") {
		b.MCP = union(b.MCP, pr.Bundle.MCP)
	}
	if pr.Bundle.Model != "" && mayChange(policy, scope, "model") {
		b.Model = pr.Bundle.Model
	}
}

// mayChange reports whether a scope is permitted to change a field. An empty
// override-permissions map means "no restrictions" (everyone may change
// anything). A non-empty map restricts every scope to its granted fields; a
// scope absent from the map may change nothing.
func mayChange(policy ResolvedPolicy, scope Scope, field string) bool {
	if len(policy.OverridePermissions) == 0 {
		return true
	}
	fields, ok := policy.OverridePermissions[scope]
	if !ok {
		return false
	}
	for _, f := range fields {
		if f == field {
			return true
		}
	}
	return false
}

// unionMinusLocked unions allow values but drops any value that an applicable
// lock forbids re-granting (e.g. a reviewer Edit/Write lock). This is the
// org-lock-wins enforcement on the additive path. Remove this filtering and
// H8(a) fails.
func unionMinusLocked(dst, add []string, policy ResolvedPolicy, ctx Context, field string) []string {
	denyField := strings.TrimSuffix(field, ".allow") + ".deny"
	forbidden := lockedValues(policy, ctx, denyField)
	out := union(dst, nil)
	for _, v := range add {
		if forbidden[v] {
			continue // locked off — a lower scope cannot re-grant it
		}
		out = appendUnique(out, v)
	}
	return out
}

// applyLockDenies forces every locked deny value into the effective bundle
// deny set (absolute). Remove this and H8 fails (the lock no longer binds).
func applyLockDenies(b *Bundle, policy ResolvedPolicy, ctx Context) {
	for v := range lockedValues(policy, ctx, "tools.deny") {
		b.Tools.Deny = appendUnique(b.Tools.Deny, v)
		b.Tools.Allow = removeValue(b.Tools.Allow, v)
	}
	for v := range lockedValues(policy, ctx, "skills.deny") {
		b.Skills.Deny = appendUnique(b.Skills.Deny, v)
		b.Skills.Allow = removeValue(b.Skills.Allow, v)
		b.Skills.Preload = removeValue(b.Skills.Preload, v)
	}
}

// lockedValues returns the set of values locked for a field in this context.
func lockedValues(policy ResolvedPolicy, ctx Context, field string) map[string]bool {
	out := map[string]bool{}
	for _, lk := range policy.Locks {
		if lk.Field != field {
			continue
		}
		if !lockSelectorMatches(lk.Selector, ctx) {
			continue
		}
		for _, v := range lk.Values {
			out[v] = true
		}
	}
	return out
}

// lockSelectorMatches checks a lock's selector tail against the context.
func lockSelectorMatches(sel Selector, ctx Context) bool {
	return selectorMatches(sel, ctx)
}

// ---------------------------------------------------------------------------
// Bundle helpers
// ---------------------------------------------------------------------------

func normalizeBundle(b *Bundle) {
	// deny wins over allow: anything denied is removed from allow.
	for _, d := range b.Tools.Deny {
		b.Tools.Allow = removeValue(b.Tools.Allow, d)
	}
	for _, d := range b.Skills.Deny {
		b.Skills.Allow = removeValue(b.Skills.Allow, d)
		b.Skills.Preload = removeValue(b.Skills.Preload, d)
	}
	sort.Strings(b.Tools.Allow)
	sort.Strings(b.Tools.Deny)
	sort.Strings(b.Skills.Preload)
	sort.Strings(b.Skills.Allow)
	sort.Strings(b.Skills.Deny)
	sort.Strings(b.Hooks)
	sort.Strings(b.MCP)
}

func union(a, b []string) []string {
	out := append([]string{}, a...)
	for _, v := range b {
		out = appendUnique(out, v)
	}
	return out
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

func removeValue(s []string, v string) []string {
	out := s[:0:0]
	for _, x := range s {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}

// digestBundle produces a stable digest by marshalling the normalized bundle
// through canonical JSON (sorted slices already applied by normalizeBundle).
func digestBundle(b Bundle) string {
	raw, _ := json.Marshal(b)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}
