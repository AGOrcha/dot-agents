package config

import (
	"fmt"
	"path/filepath"
)

// homeconfig_init.go is the config-side resolution entrypoint for the L3
// home-config `da init --from` cross-machine bootstrap (home-config-portability
// D-D). On a fresh machine the cloned home's user-local layer
// (~/.agents/.agentsrc.json) declares the sources, layering policy/profiles, and
// the optional kind:manifest unit that names the whole setup. These helpers
// resolve that USER SCOPE through the SAME §15 + L1 + L2 engines every other
// scope uses (D-B/D-C) — they fork no resolution semantics of their own.

// UserScopeSnapshot resolves the user-scope configuration: product defaults
// merged with the user-local layer (~/.agents/.agentsrc.json), with NO project
// in the chain. There is no project being resolved during `init --from`, so the
// repo-local layer is deliberately absent — unlike FlatResolver.Resolve, which
// requires a repo-local manifest and is fatal without one. A missing user-local
// layer is not an error (a home may scaffold one lazily); the snapshot then
// carries only product defaults.
func UserScopeSnapshot() (*Snapshot, error) {
	layers := []ResolvedLayer{
		{ID: LayerProductDefaults, Present: true, Raw: map[string]any{}},
	}
	userPath := filepath.Join(AgentsHome(), AgentsRCFile)
	raw, ok, err := decodeObjectFile(userPath)
	if err != nil {
		return nil, fmt.Errorf("parsing user-local %s: %w", userPath, err)
	}
	if ok {
		layers = append(layers, ResolvedLayer{ID: LayerUserLocal, Present: true, Raw: raw})
	}
	return resolveSnapshot(layers)
}

// ResolveUserScopeManifests resolves every kind:manifest unit declared in the
// resolved user scope to its referenced source-set, bound layering policy, and
// optional project-set ref — the inputs `init --from` reports and reproduces a
// whole setup from (D-D/R1). It COMPOSES the engines, forking none: §15
// source/scope resolution builds the snapshot, the L1 policy engine resolves the
// bound fragments inside ResolveManifest, and L2's ResolveManifest carries the
// source-set + project-set ref through unchanged.
//
// An empty result is valid — a home may declare no manifest and still adopt
// (item 3 of ResolvedManifest). A fail-closed authority/validation error from
// any engine (force-allow, self-blessing, malformed ref) propagates rather than
// resolving silently to nothing.
func ResolveUserScopeManifests() ([]ResolvedManifest, error) {
	snap, err := UserScopeSnapshot()
	if err != nil {
		return nil, err
	}
	return resolveManifestsInSnapshot(snap)
}

// resolveManifestsInSnapshot resolves every manifest declared in an already-built
// snapshot. Split from ResolveUserScopeManifests so the composed engines'
// fail-closed branches (ManifestSetFromSnapshot, ProfileSetFromSnapshot,
// ResolveManifest) are reachable independent of UserScopeSnapshot's own typed
// pre-validation.
func resolveManifestsInSnapshot(snap *Snapshot) ([]ResolvedManifest, error) {
	manifests, err := ManifestSetFromSnapshot(snap)
	if err != nil {
		return nil, err
	}
	set, err := ProfileSetFromSnapshot(snap)
	if err != nil {
		return nil, err
	}
	ctx := ProfileContext{ScopeChain: SnapshotScopeChain(snap)}
	out := make([]ResolvedManifest, 0, len(manifests))
	for _, m := range manifests {
		rm, err := ResolveManifest(m, set, ctx)
		if err != nil {
			return nil, fmt.Errorf("resolving manifest %q: %w", m.Ref, err)
		}
		out = append(out, rm)
	}
	return out, nil
}
