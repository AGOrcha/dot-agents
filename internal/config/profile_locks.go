package config

import (
	"sort"
)

// profile_locks.go implements the Phase-2 capability merge, the absolute-lock
// application, and the same-scope conflict detection for the profile engine. It
// reuses the §15 dot-path helpers (lookupPath, setPath, splitFieldPath) so the
// profile engine and the layer-stack authority pass share one path vocabulary.

// mergeCapabilityBundle unions a capability fragment's additive leaf fields into
// the accumulator, gated by the Decision-2 permission cap at FIELD-PATH
// granularity (e.g. "tools.allow", "mcp", "model"). Additive sets (arrays) union
// with stable dedup; scalars are last-writer. A leaf the scope may not change is
// skipped. Lock-forbidden values are dropped from additive allow sets so a lower
// scope can never re-grant a denied capability (Decision 4).
func mergeCapabilityBundle(acc map[string]any, pr ConfigProfile, policy EffectivePolicy, ctx ProfileContext) {
	for _, leaf := range flattenLeaves(pr.Bundle) {
		if !policy.Permissions.mayChange(pr.Scope, leaf.path) {
			continue
		}
		parts := splitFieldPath(leaf.path)
		if arr, ok := leaf.value.([]any); ok {
			add := dropLockForbidden(leaf.path, arr, policy, ctx)
			existing, _ := lookupPath(acc, parts)
			setPath(acc, parts, unionSlices(existing, add))
			continue
		}
		setPath(acc, parts, leaf.value)
	}
}

// dropLockForbidden removes from an additive allow set any member a matching
// deny-lock forbids re-granting (the org-lock-wins enforcement on the additive
// path — remove this and H8(a) fails).
func dropLockForbidden(path string, add []any, policy EffectivePolicy, ctx ProfileContext) []any {
	forbidden := lockedDenyMembers(policy, ctx, path)
	if len(forbidden) == 0 {
		return add
	}
	out := make([]any, 0, len(add))
	for _, v := range add {
		if s, ok := v.(string); ok && forbidden[s] {
			continue
		}
		out = append(out, v)
	}
	return out
}

// applyEffectiveLocks forces every effective lock into the merged bundle: a
// value-lock pins its field, a deny-lock subtracts its members from the field
// set (absolute — permission never beats a lock, Decision 4). A lock binds only
// scopes ranked BELOW its owner: a member a peer/higher scope contributed
// survives (Decision 8), so a lower-scope deny can never erase a higher allow.
func applyEffectiveLocks(bundle map[string]any, policy EffectivePolicy, ctx ProfileContext) {
	for _, lock := range policy.Locks {
		if !lock.Selector.matches(ctx) {
			continue
		}
		if AuthorityRankOf(lock.Owner) == 0 {
			continue // a zero-authority scope cannot bind
		}
		parts := splitFieldPath(lock.Field)
		if len(lock.Value) > 0 {
			setPath(bundle, parts, decodeLockValue(lock.Value))
			continue
		}
		applyDenyMembers(bundle, parts, lock)
	}
}

// applyDenyMembers removes each denied member from the set at the lock's field
// path.
func applyDenyMembers(bundle map[string]any, parts []string, lock ProfileLock) {
	cur, ok := lookupPath(bundle, parts)
	if !ok {
		return
	}
	arr, ok := cur.([]any)
	if !ok {
		return
	}
	deny := map[string]bool{}
	for _, m := range lock.Deny {
		deny[m] = true
	}
	out := make([]any, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok && deny[s] {
			continue
		}
		out = append(out, v)
	}
	setPath(bundle, parts, out)
}

// lockedDenyMembers returns the set of deny-locked members for a field path in
// this context (selector-matched, authority-bearing owner).
func lockedDenyMembers(policy EffectivePolicy, ctx ProfileContext, field string) map[string]bool {
	out := map[string]bool{}
	for _, lock := range policy.Locks {
		if lock.Field != field || len(lock.Value) > 0 {
			continue
		}
		if AuthorityRankOf(lock.Owner) == 0 || !lock.Selector.matches(ctx) {
			continue
		}
		for _, m := range lock.Deny {
			out[m] = true
		}
	}
	return out
}

// bindingLocks returns the effective binding locks for a context — those whose
// selector matches and whose owner carries authority — in deterministic order
// for explain (R6).
func bindingLocks(policy EffectivePolicy, ctx ProfileContext) []ResolvedLockInfo {
	var binding []ProfileLock
	for _, lock := range policy.Locks {
		if AuthorityRankOf(lock.Owner) == 0 || !lock.Selector.matches(ctx) {
			continue
		}
		binding = append(binding, lock)
	}
	return lockInfos(binding)
}

// lockInfos projects locks into the explain/digest shape in deterministic order.
func lockInfos(locks []ProfileLock) []ResolvedLockInfo {
	out := make([]ResolvedLockInfo, 0, len(locks))
	for _, lock := range locks {
		info := ResolvedLockInfo{Field: lock.Field, Kind: lock.kind(), Owner: lock.Owner}
		if len(lock.Value) > 0 {
			info.Value = decodeLockValue(lock.Value)
		} else {
			info.Deny = append([]string{}, lock.Deny...)
		}
		out = append(out, info)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Field != out[j].Field {
			return out[i].Field < out[j].Field
		}
		return out[i].Owner < out[j].Owner
	})
	return out
}

// leaf is one flattened (dot-path → leaf value) entry of a bundle.
type leaf struct {
	path  string
	value any
}

// flattenLeaves walks a bundle into its leaf dot-paths, where a leaf is any
// non-object value (array or scalar). Map nodes recurse; arrays/scalars are
// emitted. The result is sorted by path so the merge order is deterministic.
func flattenLeaves(bundle map[string]any) []leaf {
	var out []leaf
	flattenInto("", bundle, &out)
	sort.SliceStable(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func flattenInto(prefix string, node map[string]any, out *[]leaf) {
	for _, key := range sortedAnyKeys(node) {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		if child, ok := node[key].(map[string]any); ok {
			flattenInto(path, child, out)
			continue
		}
		*out = append(*out, leaf{path: path, value: node[key]})
	}
}

// conflictKey identifies a (scope, leaf-path) cell across fragments.
type conflictKey struct {
	scope AuthorityScope
	path  string
}

// conflictEntry is one fragment's contribution to a cell.
type conflictEntry struct {
	ref   string
	value any
}

// detectConflicts surfaces same-scope value conflicts (Decision 6): two fragments
// at the SAME authority scope that set the same leaf path to different values.
// Both contributing refs are surfaced (not just the winner). The matched
// profiles must already be precedence/specificity/ref-ordered (orderProfiles),
// so the last setter in each scope group is the winner.
func detectConflicts(matched []ConfigProfile, _ EffectivePolicy) []ProfileConflict {
	order := []conflictKey{}
	seen := map[conflictKey][]conflictEntry{}
	for _, pr := range matched {
		for _, lf := range flattenLeaves(pr.Bundle) {
			k := conflictKey{scope: pr.Scope, path: lf.path}
			if _, ok := seen[k]; !ok {
				order = append(order, k)
			}
			seen[k] = append(seen[k], conflictEntry{ref: pr.Ref, value: lf.value})
		}
	}
	return collectConflicts(order, seen)
}

// collectConflicts reduces the per-cell entries into the conflicts where two
// same-scope fragments disagree on a value, in deterministic (scope, path) order.
func collectConflicts(order []conflictKey, seen map[conflictKey][]conflictEntry) []ProfileConflict {
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].scope != order[j].scope {
			return order[i].scope < order[j].scope
		}
		return order[i].path < order[j].path
	})
	var out []ProfileConflict
	for _, k := range order {
		entries := seen[k]
		if !entriesDisagree(entries) {
			continue
		}
		refs := make([]string, 0, len(entries))
		for _, e := range entries {
			refs = append(refs, e.ref)
		}
		sort.Strings(refs)
		out = append(out, ProfileConflict{
			Field:  k.path,
			Scope:  k.scope,
			Winner: entries[len(entries)-1].ref, // last in precedence order wins
			Refs:   refs,
		})
	}
	return out
}

// entriesDisagree reports whether the cell's fragments set more than one distinct
// value.
func entriesDisagree(entries []conflictEntry) bool {
	if len(entries) < 2 {
		return false
	}
	first := entries[0].value
	for _, e := range entries[1:] {
		if !valuesEqual(first, e.value) {
			return true
		}
	}
	return false
}
