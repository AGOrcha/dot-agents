package config

import (
	"encoding/json"
	"sort"
)

// profile_locks.go implements the Phase-2 capability value-merge and the
// same-scope conflict detection, and routes the lock/grant axis THROUGH the §15
// authority substrate (runAuthorityPass + applyValueLocks/applyDenyLocks). It
// does NOT re-implement value-locks, deny-locks, or deny-provenance — those are
// §15's, so the profile engine inherits the round-2 fix where a deny subtracts a
// member only when its highest contributing authority-rank is strictly below the
// lock owner's (a lower deny can never erase a higher/peer allow).

// mergeCapabilityBundle unions a capability fragment's additive leaf fields into
// the accumulator, gated by the Decision-2 permission cap at FIELD-PATH
// granularity (e.g. "tools_allow", "mcp", "model"). Additive sets (arrays) union
// via the §15 unionSlices primitive; scalars are last-writer. A field the scope
// may not change is skipped. Deny enforcement is NOT done here — it is applied
// after the merge by the §15 deny-lock pass (applyProfileAuthority), which alone
// owns the contributor-rank provenance.
func mergeCapabilityBundle(acc map[string]any, pr ConfigProfile, policy EffectivePolicy) {
	for _, leaf := range flattenLeaves(pr.Bundle) {
		if !policy.Permissions.mayChange(pr.Scope, leaf.path) {
			continue
		}
		parts := splitFieldPath(leaf.path)
		if arr, ok := leaf.value.([]any); ok {
			existing, _ := lookupPath(acc, parts)
			setPath(acc, parts, unionSlices(existing, arr))
			continue
		}
		setPath(acc, parts, leaf.value)
	}
}

// applyProfileAuthority runs the lock/grant axis over the value-merged bundle by
// reusing the §15 substrate end-to-end — the same orchestration applyAuthority
// uses for the layer stack, only sourcing locks from the resolved layering policy
// (context-matched) and contributors from the matched profiles:
//
//  1. runAuthorityPass folds the policy's locks into the effective value/deny
//     set (owner-precedence, zero-authority rejection, force-allow validation);
//  2. overlapping value-locks + any force-allow are fatal, fail-closed;
//  3. applyValueLocks / applyDenyLocks pin/subtract over the bundle — the deny
//     pass uses highestContributingRank so a lower-scope deny cannot erase a
//     higher/peer allow (the round-2 invariant, ported for free).
//
// It mutates bundle in place and returns the effective binding locks (for
// explain) and the lower-scope write collisions.
func applyProfileAuthority(bundle map[string]any, matched []ConfigProfile, policy EffectivePolicy, ctx ProfileContext) ([]ResolvedLockInfo, []LockCollision, error) {
	res := runAuthorityPass(profileLockLayers(policy, ctx))
	viols := append([]AuthorityViolation{}, res.violations...)
	viols = append(viols, overlappingLockPaths(res.valueLocks)...)
	if fatal := fatalViolations(viols); len(fatal) > 0 {
		return nil, nil, authorityError(fatal)
	}
	contrib := profileContribLayers(matched)
	collisions := applyValueLocks(contrib, res.valueLocks, bundle)
	collisions = append(collisions, applyDenyLocks(contrib, res.denyLocks, bundle)...)
	return effectiveLockInfos(res), collisions, nil
}

// profileLockLayers translates the resolved policy's context-matched locks into
// §15 authorityLayers — one per owning policy scope — so runAuthorityPass can
// fold them with full §15 semantics. Only locks whose selector matches the
// context contribute (lock context-scoping happens here, before §15 sees them);
// each lock's authority is its owning policy scope.
func profileLockLayers(policy EffectivePolicy, ctx ProfileContext) []authorityLayer {
	byOwner := map[AuthorityScope]*PolicyLockSpec{}
	owners := []AuthorityScope{}
	for _, lock := range policy.Locks {
		if !lock.Selector.matches(ctx) {
			continue
		}
		spec, ok := byOwner[lock.Owner]
		if !ok {
			spec = &PolicyLockSpec{ValueLocks: map[string]json.RawMessage{}}
			byOwner[lock.Owner] = spec
			owners = append(owners, lock.Owner)
		}
		if len(lock.Value) > 0 {
			spec.ValueLocks[lock.Field] = lock.Value
			continue
		}
		for _, m := range lock.Deny {
			spec.DenyLocks = append(spec.DenyLocks, lock.Field+":"+m)
		}
	}
	sort.SliceStable(owners, func(i, j int) bool {
		return AuthorityRankOf(owners[i]) < AuthorityRankOf(owners[j])
	})
	out := make([]authorityLayer, 0, len(owners))
	for _, scope := range owners {
		out = append(out, authorityLayer{id: string(scope), scope: scope, locks: *byOwner[scope]})
	}
	return out
}

// profileContribLayers maps the matched profiles to §15 authorityLayers (scope +
// bundle) so applyValueLocks/applyDenyLocks read contributor provenance off the
// real fragments — the per-member highest-contributing-rank the deny pass needs.
func profileContribLayers(matched []ConfigProfile) []authorityLayer {
	out := make([]authorityLayer, 0, len(matched))
	for _, pr := range matched {
		out = append(out, authorityLayer{id: pr.Ref, scope: pr.Scope, raw: pr.Bundle})
	}
	return out
}

// effectiveLockInfos projects the §15 authority-pass result into the explain/JSON
// lock shape, grouping deny-lock members by (field, owner), in deterministic order.
func effectiveLockInfos(res authorityResult) []ResolvedLockInfo {
	out := make([]ResolvedLockInfo, 0, len(res.valueLocks)+len(res.denyLocks))
	for field, vl := range res.valueLocks {
		out = append(out, ResolvedLockInfo{Field: field, Kind: collisionValueLock, Owner: vl.owner, Value: vl.value})
	}
	denyByKey := map[string]*ResolvedLockInfo{}
	var denyOrder []string
	for _, dl := range res.denyLocks {
		key := dl.field + "\x00" + string(dl.owner)
		info, ok := denyByKey[key]
		if !ok {
			info = &ResolvedLockInfo{Field: dl.field, Kind: collisionDenyLock, Owner: dl.owner}
			denyByKey[key] = info
			denyOrder = append(denyOrder, key)
		}
		info.Deny = append(info.Deny, dl.member)
	}
	for _, key := range denyOrder {
		out = append(out, *denyByKey[key])
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
