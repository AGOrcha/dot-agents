package config

import (
	"encoding/json"
	"fmt"
	"sort"
)

// profile_snapshot.go bridges the resolved layer stack (Snapshot) to the profile
// engine. It derives the engine input — app_type + stage profiles plus any
// scope-attached layering policy — from each layer's raw object, tagging every
// derived unit with the layer's SOURCE-derived authority scope (Decision 1). The
// `da config explain --role/--app-type/--stage/--harness` surface (R6) and the
// zero-diff migration test both resolve through this one bridge, so explain shows
// exactly what the engine resolves.

// ProfileSetFromSnapshot derives the profile engine input from a resolved
// snapshot. Each present layer contributes its execution_profile.by_app_type and
// stage_profiles entries as kind:profile units and, when present, its
// layering_policy unit — all carried at the layer's authority scope. A malformed
// layering_policy on any layer is a fail-closed error (R9), surfaced rather than
// silently dropped.
func ProfileSetFromSnapshot(snap *Snapshot) (ProfileSet, error) {
	var set ProfileSet
	for _, layer := range snap.Layers {
		if layer.Raw == nil {
			continue
		}
		scope := baseLayerScope(layer.ID)
		if err := appendLayerProfiles(&set, layer.Raw, scope); err != nil {
			return ProfileSet{}, fmt.Errorf("layer %q: %w", layer.ID, err)
		}
	}
	return set, nil
}

// appendLayerProfiles folds one layer's derived profiles + policy into the set.
func appendLayerProfiles(set *ProfileSet, raw map[string]any, scope AuthorityScope) error {
	rc, err := decodeEffective(raw)
	if err != nil {
		return fmt.Errorf("decoding layer manifest: %w", err)
	}
	set.Profiles = append(set.Profiles, profilesFromExecutionProfile(rc.ExecutionProfile, scope)...)
	set.Profiles = append(set.Profiles, profilesFromStageProfiles(rc.StageProfiles, scope)...)

	if rawPolicy, ok := raw["layering_policy"]; ok {
		data, err := json.Marshal(rawPolicy)
		if err != nil {
			return fmt.Errorf("re-encoding layering_policy: %w", err)
		}
		policy, err := decodeLayeringPolicy(data, scope)
		if err != nil {
			return err
		}
		set.Policies = append(set.Policies, policy)
	}
	return nil
}

// SnapshotScopeChain returns the authority scopes present in the snapshot, in
// canonical low→high order. It is the default context scope chain `da config
// explain` resolves a profile against when the caller does not pin one.
func SnapshotScopeChain(snap *Snapshot) []AuthorityScope {
	present := map[AuthorityScope]bool{}
	for _, layer := range snap.Layers {
		if layer.Raw != nil {
			present[baseLayerScope(layer.ID)] = true
		}
	}
	chain := make([]AuthorityScope, 0, len(present))
	for s := range present {
		chain = append(chain, s)
	}
	sort.SliceStable(chain, func(i, j int) bool {
		return AuthorityRankOf(chain[i]) < AuthorityRankOf(chain[j])
	})
	return chain
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
	return ResolveProfile(set, ctx), nil
}
