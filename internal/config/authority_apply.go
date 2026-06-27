package config

import (
	"encoding/json"
	"fmt"
	"strings"
)

// authority_apply.go wires the policy-authority pass (authority.go, Phase 1)
// onto the existing value-merge (Phase 2). It derives each layer's authority
// scope (via the source-authority registry), runs the pass, and applies the
// surviving value-locks / deny-locks to the already-merged map — recording the
// rejected lower-scope writes as LockCollisions so `da config explain` can
// surface attempted/winning/owner. With no locks and no grants the merged map
// is returned untouched (zero shipped-behavior change).

// applyAuthority runs Phase 1 over the resolved layers and applies the effective
// locks to the merged value map (mutated in place). It returns the recorded
// collisions and non-fatal violations, or a fatal error when a layer's policy is
// malformed, or when a self-blessing/overwrite grant or a force-allow lock is
// present (all fail-closed).
func applyAuthority(layers []ResolvedLayer, merged map[string]any) ([]LockCollision, []AuthorityViolation, error) {
	al, err := buildAuthorityLayers(layers)
	if err != nil {
		return nil, nil, err
	}
	grants, grantViols := resolveAuthorityGrants(al)
	applyGrantsToScopes(al, grants)

	res := runAuthorityPass(al)
	viols := append(grantViols, res.violations...)
	if fatal := fatalViolations(viols); len(fatal) > 0 {
		return nil, viols, authorityError(fatal)
	}

	collisions := applyValueLocks(al, res.valueLocks, merged)
	collisions = append(collisions, applyDenyLocks(al, res.denyLocks, merged)...)
	return collisions, viols, nil
}

// buildAuthorityLayers maps each resolved layer to its BASE authority scope and
// parses+validates its declared policy fail-closed. Built-in layers carry a fixed
// scope; an imported (extends) layer defaults to AuthPublic — value-only — until
// the source-authority registry grants it a real scope. A malformed locks or
// authority_grants block aborts the resolve rather than being silently skipped.
func buildAuthorityLayers(layers []ResolvedLayer) ([]authorityLayer, error) {
	out := make([]authorityLayer, 0, len(layers))
	for _, l := range layers {
		locks, err := extractLocks(l.Raw)
		if err != nil {
			return nil, fmt.Errorf("layer %q: %w", l.ID, err)
		}
		grants, err := parseGrants(l.Raw)
		if err != nil {
			return nil, fmt.Errorf("layer %q: %w", l.ID, err)
		}
		out = append(out, authorityLayer{
			id:    l.ID,
			scope: baseLayerScope(l.ID),
			raw:   l.Raw,
			locks: locks, grants: grants,
		})
	}
	return out, nil
}

// baseLayerScope returns the fixed authority scope for a built-in layer id, or
// AuthPublic for an imported (extends-ref) layer that has not been granted a
// scope yet.
func baseLayerScope(id string) AuthorityScope {
	switch id {
	case LayerProductDefaults:
		return AuthProduct
	case LayerUserLocal:
		return AuthUser
	case LayerRepoLocal:
		return AuthRepo
	case LayerProjectLocal:
		return AuthProjectLocal
	default:
		return AuthPublic
	}
}

// applyGrantsToScopes upgrades each imported layer's scope from the resolved
// source-authority grants. The source id is the segment before the first ':' in
// the extends ref (the layer id). Built-in layers keep their fixed scope.
func applyGrantsToScopes(layers []authorityLayer, grants map[string]AuthorityScope) {
	if len(grants) == 0 {
		return
	}
	for i := range layers {
		if isBuiltinLayer(layers[i].id) {
			continue
		}
		src := sourceIDOf(layers[i].id)
		if scope, ok := grants[src]; ok {
			layers[i].scope = scope
		}
	}
}

func isBuiltinLayer(id string) bool {
	switch id {
	case LayerProductDefaults, LayerUserLocal, LayerRepoLocal, LayerProjectLocal:
		return true
	default:
		return false
	}
}

// sourceIDOf returns the source id of an extends ref ("acme:org/base" → "acme").
func sourceIDOf(ref string) string {
	if idx := strings.Index(ref, ":"); idx > 0 {
		return ref[:idx]
	}
	return ref
}

// extractLocks decodes and VALIDATES a layer's `locks` block fail-closed. An
// absent block yields an empty spec; a present-but-malformed block (not an
// object, or a deny_lock without "field:member" shape) is an ERROR, never a
// silent empty — a silently-ignored lock is fail-open.
func extractLocks(raw map[string]any) (PolicyLockSpec, error) {
	v, ok := raw["locks"]
	if !ok {
		return PolicyLockSpec{}, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return PolicyLockSpec{}, fmt.Errorf("malformed locks block: %w", err)
	}
	var spec PolicyLockSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return PolicyLockSpec{}, fmt.Errorf("malformed locks block: %w", err)
	}
	if err := validateLockSpec(spec); err != nil {
		return PolicyLockSpec{}, err
	}
	return spec, nil
}

// applyValueLocks pins every value-locked field — walking the dot-separated FIELD
// PATH so a lock on a nested key (e.g. "features.flag") pins the nested value, not
// a literal top-level key — and records a collision for the highest-precedence
// lower-authority writer the lock rejected. A lock binds ONLY scopes ranked BELOW
// its owner (higher binds lower): a peer-or-higher scope that set the field with a
// plain value still wins over the pin, so a user-scope lock can never out-rank a
// repo-scope value.
func applyValueLocks(layers []authorityLayer, locks map[string]effectiveValueLock, merged map[string]any) []LockCollision {
	var collisions []LockCollision
	for field, lock := range locks {
		parts := splitFieldPath(field)
		winning := winningLockedValue(layers, parts, lock)
		setPath(merged, parts, winning)
		if attempted, found := rejectedWrite(layers, parts, winning, lock.rank); found {
			collisions = append(collisions, LockCollision{
				Field: field, Attempted: attempted, Winning: winning,
				Owner: lock.owner, OwnerRank: lock.rank, Kind: collisionValueLock,
			})
		}
	}
	return collisions
}

// winningLockedValue returns the value a value-locked field path resolves to: the
// owner's pinned value, unless a scope ranked at-or-above the owner set the path
// with a plain value (a peer/higher authority is NOT bound by the lock and wins).
// Layers are in value-precedence order (lowest first), so the last such writer is
// the one that would win the value-merge.
func winningLockedValue(layers []authorityLayer, parts []string, lock effectiveValueLock) any {
	winning := lock.value
	for _, l := range layers {
		if AuthorityRankOf(l.scope) < lock.rank {
			continue
		}
		if v, ok := lookupPath(l.raw, parts); ok {
			winning = v
		}
	}
	return winning
}

// rejectedWrite finds the highest-value-precedence lower-authority layer that set
// the field path to a value other than the winning one, returning that attempted
// value.
func rejectedWrite(layers []authorityLayer, parts []string, winning any, rank int) (any, bool) {
	var attempted any
	found := false
	for _, l := range layers {
		if AuthorityRankOf(l.scope) >= rank {
			continue
		}
		v, ok := lookupPath(l.raw, parts)
		if !ok || valuesEqual(v, winning) {
			continue
		}
		attempted, found = v, true
	}
	return attempted, found
}

// applyDenyLocks subtracts each deny-locked set member from the merged field —
// but ONLY when no scope ranked at-or-above the deny owner contributed it. A deny
// binds only LOWER scopes (§15 D1a :920/:1320): a member a peer/higher scope
// allowed SURVIVES, so a lower-scope deny can never erase a higher allow. When the
// member is removed, each lower contributor whose add was dropped is recorded.
func applyDenyLocks(layers []authorityLayer, locks []effectiveDenyLock, merged map[string]any) []LockCollision {
	var collisions []LockCollision
	for _, lock := range locks {
		if highestContributingRank(layers, lock.field, lock.member) >= lock.rank {
			continue // a peer/higher scope allowed it — deny binds only lower
		}
		removeSetMember(merged, lock.field, lock.member)
		collisions = append(collisions, denyCollisions(layers, lock)...)
	}
	return collisions
}

// highestContributingRank returns the highest authority rank among layers that
// contributed member to the set field, or -1 when no layer did. It is the
// provenance the deny-overrides rule needs to tell a lower allow from a higher one.
func highestContributingRank(layers []authorityLayer, field, member string) int {
	max := -1
	for _, l := range layers {
		if !setHasMember(l.raw[field], member) {
			continue
		}
		if r := AuthorityRankOf(l.scope); r > max {
			max = r
		}
	}
	return max
}

// denyCollisions records a collision for each lower-authority layer whose set for
// the denied field included the denied member.
func denyCollisions(layers []authorityLayer, lock effectiveDenyLock) []LockCollision {
	var out []LockCollision
	for _, l := range layers {
		if AuthorityRankOf(l.scope) >= lock.rank {
			continue
		}
		if setHasMember(l.raw[lock.field], lock.member) {
			out = append(out, LockCollision{
				Field: lock.field + ":" + lock.member, Attempted: lock.member,
				Winning: nil, Owner: lock.owner, OwnerRank: lock.rank, Kind: collisionDenyLock,
			})
		}
	}
	return out
}

// setPath sets val at the dot-path parts in m, COPY-ON-WRITE: each nested level
// is cloned before descending, so it never mutates a sub-map the value-merge may
// have aliased from a layer's raw object (mergeMaps returns the first writer's map
// by reference). Mutating in place would corrupt that layer's provenance and make
// the rejected-write check read the pinned value back. An intermediate that is not
// an object is replaced with a fresh one so a nested pin always lands; an empty
// path is a no-op.
func setPath(m map[string]any, parts []string, val any) {
	if len(parts) == 0 {
		return
	}
	if len(parts) == 1 {
		m[parts[0]] = val
		return
	}
	child, _ := m[parts[0]].(map[string]any)
	clone := make(map[string]any, len(child)+1)
	for k, v := range child {
		clone[k] = v
	}
	setPath(clone, parts[1:], val)
	m[parts[0]] = clone
}

// removeSetMember drops member from the merged field when it is a JSON array.
func removeSetMember(merged map[string]any, field, member string) {
	arr, ok := merged[field].([]any)
	if !ok {
		return
	}
	out := arr[:0:0]
	for _, item := range arr {
		if s, ok := item.(string); ok && s == member {
			continue
		}
		out = append(out, item)
	}
	merged[field] = out
}

// setHasMember reports whether a raw JSON array contains the string member.
func setHasMember(raw any, member string) bool {
	arr, ok := raw.([]any)
	if !ok {
		return false
	}
	for _, item := range arr {
		if s, ok := item.(string); ok && s == member {
			return true
		}
	}
	return false
}

// valuesEqual compares two decoded JSON values by their canonical encoding.
func valuesEqual(a, b any) bool {
	da, err := json.Marshal(a)
	if err != nil {
		return false
	}
	db, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(da) == string(db)
}
