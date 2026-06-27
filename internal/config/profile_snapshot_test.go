package config

import "testing"

func TestProfileSetFromSnapshotDerivesUnits(t *testing.T) {
	snap := mustResolveLayers(t, migrationLayers())
	set, err := ProfileSetFromSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	var appType, stage int
	for _, p := range set.Profiles {
		switch p.Kind {
		case ProfileKindAppType:
			appType++
		case ProfileKindStage:
			stage++
		}
	}
	if appType == 0 || stage == 0 {
		t.Fatalf("expected both app_type and stage profiles derived, got app=%d stage=%d", appType, stage)
	}
	// Every derived profile must carry a source-derived authority scope (the
	// imported "acme:org/base" layer is the ungranted-public default), and the
	// source-provenanced ref must carry the contributing layer id (FIX 4).
	sawImported := false
	for _, p := range set.Profiles {
		switch p.Scope {
		case AuthProduct, AuthRepo:
		case AuthPublic:
			sawImported = true
		default:
			t.Fatalf("profile %q has unexpected scope %q", p.Ref, p.Scope)
		}
	}
	if !sawImported {
		t.Fatal("expected the imported (public) layer to contribute a profile")
	}
}

func TestSnapshotScopeChainMembership(t *testing.T) {
	snap := mustResolveLayers(t, migrationLayers())
	chain := SnapshotScopeChain(snap)
	// The chain is membership for ctx.inChain; the value-merge ORDER is carried by
	// each profile's Order (layer index), not the chain order. It must include
	// every present scope (product, the imported public source, repo).
	want := map[AuthorityScope]bool{AuthProduct: true, AuthPublic: true, AuthRepo: true}
	if len(chain) != len(want) {
		t.Fatalf("scope chain = %v, want the 3 present scopes", chain)
	}
	for _, s := range chain {
		if !want[s] {
			t.Fatalf("unexpected scope %q in chain %v", s, chain)
		}
	}
}

func TestResolveProfileContextEndToEnd(t *testing.T) {
	snap := mustResolveLayers(t, migrationLayers())
	got, err := ResolveProfileContext(snap, "", "go-cli", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Contributing) == 0 {
		t.Fatal("expected go-cli context to resolve contributing refs")
	}
	if got.Digest == "" {
		t.Fatal("expected a non-empty digest")
	}
	if got.PolicyMode != PolicyModeNarrow {
		t.Fatalf("policy mode = %q, want narrow (no policy authored)", got.PolicyMode)
	}
}

func TestProfileSetFromSnapshotMalformedPolicyFailsClosed(t *testing.T) {
	layers := []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"layering_policy": map[string]any{"mode": "merge"}, // invalid mode
		}},
	}
	snap := mustResolveLayers(t, layers)
	if _, err := ProfileSetFromSnapshot(snap); err == nil {
		t.Fatal("expected a malformed layering_policy to fail closed (R9)")
	}
}

func TestProfileSetFromSnapshotSelfBlessingGrantFails(t *testing.T) {
	// A user-local layer (rank 1) granting org authority is self-blessing — a §15
	// fatal violation that must fail closed through the profile bridge (FIX 3 / R9).
	snap := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerUserLocal, Present: true, Raw: map[string]any{
			"authority_grants": map[string]any{"acme": "org"},
		}},
	}}
	if _, err := ProfileSetFromSnapshot(snap); err == nil {
		t.Fatal("expected a self-blessing authority_grant to fail closed")
	}
	// SnapshotScopeChain swallows the error and falls back to base scopes for
	// membership; it must still report the present user scope.
	chain := SnapshotScopeChain(snap)
	if len(chain) != 1 || chain[0] != AuthUser {
		t.Fatalf("fallback scope chain = %v, want [user]", chain)
	}
}

func TestEffectiveLayerScopesMalformedGrant(t *testing.T) {
	layers := []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{"authority_grants": "not-an-object"}},
	}
	if _, err := effectiveLayerScopes(layers); err == nil {
		t.Fatal("expected a malformed authority_grants block to fail closed")
	}
}

func TestProfileSetFromSnapshotReadsPolicy(t *testing.T) {
	layers := []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"layering_policy": map[string]any{
				"override_permissions": map[string]any{"repo": []any{"tools.allow"}},
			},
		}},
	}
	snap := mustResolveLayers(t, layers)
	set, err := ProfileSetFromSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Policies) != 1 || set.Policies[0].Scope != AuthRepo {
		t.Fatalf("expected one repo-scoped policy, got %+v", set.Policies)
	}
}
