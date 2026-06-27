package config

import (
	"encoding/json"
	"fmt"
	"sort"
)

// profile_snapshot.go bridges the resolved layer stack (Snapshot) to the profile
// engine. It derives the engine input — app_type + stage profiles plus any
// scope-attached layering policy — from each layer's raw object, tagging every
// derived unit with the layer's SOURCE-derived authority scope (Decision 1),
// AFTER applying the §15 source-authority grants so a granted org/team source
// resolves under its conferred authority (FIX 3). The `da config explain
// --role/--app-type/--stage/--harness` surface (R6) and the zero-diff migration
// test both resolve through this one bridge, so explain shows exactly what the
// engine resolves.

// ProfileSetFromSnapshot derives the profile engine input from a resolved
// snapshot. Each present layer contributes its execution_profile.by_app_type and
// stage_profiles entries as kind:profile units and, when present, its
// layering_policy unit — all carried at the layer's EFFECTIVE authority scope
// (base scope, upgraded by any §15 authority_grant the stack confers, FIX 3). A
// malformed layering_policy or authority_grants block on any layer is a
// fail-closed error (R9), surfaced rather than silently dropped.
func ProfileSetFromSnapshot(snap *Snapshot) (ProfileSet, error) {
	scopes, err := effectiveLayerScopes(snap.Layers)
	if err != nil {
		return ProfileSet{}, err
	}
	var set ProfileSet
	for i, layer := range snap.Layers {
		if layer.Raw == nil {
			continue
		}
		// i is the layer's position in the value-precedence-ordered stack
		// (product → user → extends → repo → project-local), which becomes each
		// derived profile's value-merge Order — so values merge in layer order
		// exactly as legacy resolveSnapshot does, independent of authority scope.
		if err := appendLayerProfiles(&set, layer.Raw, scopes[layer.ID], layer.ID, i); err != nil {
			return ProfileSet{}, fmt.Errorf("layer %q: %w", layer.ID, err)
		}
	}
	return set, nil
}

// effectiveLayerScopes computes each layer's authority scope AFTER §15
// source-authority grants. It reuses the §15 grant machinery — buildAuthorityLayers
// (which parses+validates each layer's authority_grants fail-closed),
// resolveAuthorityGrants (the write-guard: no self-blessing), and
// applyGrantsToScopes (the conferred-scope upgrade) — so a profile derived from a
// granted org/team source carries org/team authority, not the AuthPublic default
// of an ungranted import (FIX 3 / Decision 1).
func effectiveLayerScopes(layers []ResolvedLayer) (map[string]AuthorityScope, error) {
	al, err := buildAuthorityLayers(layers)
	if err != nil {
		return nil, err
	}
	grants, grantViols := resolveAuthorityGrants(al)
	if fatal := fatalViolations(grantViols); len(fatal) > 0 {
		return nil, authorityError(fatal)
	}
	applyGrantsToScopes(al, grants)
	out := make(map[string]AuthorityScope, len(al))
	for _, l := range al {
		out[l.id] = l.scope
	}
	return out, nil
}

// appendLayerProfiles folds one layer's derived profiles + policy into the set,
// using the layer's effective (grant-upgraded) scope as the profiles' authority
// and the layer id as their source provenance (FIX 4).
func appendLayerProfiles(set *ProfileSet, raw map[string]any, scope AuthorityScope, source string, order int) error {
	rc, err := decodeEffective(raw)
	if err != nil {
		return fmt.Errorf("decoding layer manifest: %w", err)
	}
	set.Profiles = append(set.Profiles, profilesFromExecutionProfile(rc.ExecutionProfile, scope, source, order)...)
	set.Profiles = append(set.Profiles, profilesFromStageProfiles(rc.StageProfiles, scope, source, order)...)

	if rawPolicy, ok := raw["layering_policy"]; ok {
		// rawPolicy came from a decoded JSON object, so re-encoding cannot fail
		// (same impossible-marshal convention as WriteUnitsLock).
		data, _ := json.Marshal(rawPolicy)
		policy, err := decodeLayeringPolicy(data, scope)
		if err != nil {
			return err
		}
		set.Policies = append(set.Policies, policy)
	}
	return nil
}

// SnapshotScopeChain returns the authority scopes present in the snapshot, in
// VALUE-PRECEDENCE order (CanonicalScopeOrdering().ValuePrecedence) — NOT
// authority-rank — because the chain seeds the VALUE-merge ordering and must
// match the legacy layer order (an imported org/team source sorts BELOW repo for
// values). Scopes reflect §15 grant upgrades (FIX 3). It is the default context
// scope chain `da config explain` resolves a profile against when the caller does
// not pin one.
func SnapshotScopeChain(snap *Snapshot) []AuthorityScope {
	scopes, err := effectiveLayerScopes(snap.Layers)
	if err != nil {
		scopes = map[string]AuthorityScope{}
	}
	present := map[AuthorityScope]bool{}
	for _, layer := range snap.Layers {
		if layer.Raw == nil {
			continue
		}
		if s, ok := scopes[layer.ID]; ok {
			present[s] = true
		} else {
			present[baseLayerScope(layer.ID)] = true
		}
	}
	chain := make([]AuthorityScope, 0, len(present))
	for s := range present {
		chain = append(chain, s)
	}
	valueRank := valuePrecedenceRanker()
	sort.SliceStable(chain, func(i, j int) bool {
		return valueRank(chain[i]) < valueRank(chain[j])
	})
	return chain
}

// valuePrecedenceRanker returns the value-axis rank function from the canonical
// §15 ValuePrecedence order (the same axis the resolver's precedenceRanker uses).
func valuePrecedenceRanker() func(AuthorityScope) int {
	idx := map[AuthorityScope]int{}
	for i, s := range CanonicalScopeOrdering().ValuePrecedence {
		idx[s] = i
	}
	return func(s AuthorityScope) int {
		if r, ok := idx[s]; ok {
			return r
		}
		return -1
	}
}

// ResolveProfileContext is the high-level entry the explain surface calls: derive
// the engine input from the snapshot and resolve the given dispatch context
// against the snapshot's scope chain. It is the single readback path that makes
// `da config explain` the profile truth surface (R6).
func ResolveProfileContext(snap *Snapshot, role, appType, stage, harness string) (ResolvedProfile, error) {
	set, err := ProfileSetFromSnapshot(snap)
	if err != nil {
		return ResolvedProfile{}, err
	}
	ctx := ProfileContext{
		Role:       role,
		AppType:    appType,
		Stage:      stage,
		Harness:    harness,
		ScopeChain: SnapshotScopeChain(snap),
	}
	return ResolveProfile(set, ctx)
}
