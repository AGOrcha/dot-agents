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
	// Every derived profile must carry a source-derived authority scope, never an
	// empty/self-declared one.
	for _, p := range set.Profiles {
		if p.Scope != AuthProduct && p.Scope != AuthRepo {
			t.Fatalf("profile %q has unexpected scope %q", p.Ref, p.Scope)
		}
	}
}

func TestSnapshotScopeChainOrder(t *testing.T) {
	snap := mustResolveLayers(t, migrationLayers())
	chain := SnapshotScopeChain(snap)
	// product (rank 0) must come before repo (rank 2): low→high.
	if len(chain) != 2 || chain[0] != AuthProduct || chain[1] != AuthRepo {
		t.Fatalf("scope chain = %v, want [product repo]", chain)
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
