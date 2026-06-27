package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

// profile_migration_test.go is L1's empirical gate (spec §5.2): re-express the
// EXISTING execution_profile + stage_profiles config through the new kind:profile
// engine and PROVE ZERO behavioral diff against the REAL legacy CategoryMapMerge
// (resolveSnapshot). The stack is widened to THREE layers — product → an imported
// (extends) source → repo-local — so the merge crosses scopes AND exercises an
// imported layer that sits BELOW repo for VALUES. That imported-middle layer is
// the case that exposes the round-1 soundness bug: ordering the value-merge by
// AUTHORITY-rank (or colliding source-less refs) reordered product vs the import
// and diverged from the legacy layer-order merge. The widened test fails on that
// code and passes on the value-precedence/Order + source-ref fix.

// migrationLayers builds a THREE-scope stack: product defaults → an imported
// (extends) source "acme:org/base" → repo-local. The import overlaps product on
// fields repo does NOT set, so the product-vs-import order is observable (and must
// match the legacy layer order, import after product).
func migrationLayers() []ResolvedLayer {
	product := map[string]any{
		"execution_profile": map[string]any{
			"default_class": "situational",
			"by_app_type": map[string]any{
				"go-cli": map[string]any{
					"relevance": map[string]any{
						"verify": map[string]any{"core": []any{"go-test"}, "noise": []any{"chatty-skill"}},
						"review": map[string]any{"core": []any{"review-lens"}},
					},
					"topology": map[string]any{"executors": 1, "verifiers_per_executor": 1},
				},
				"ideation": map[string]any{
					"relevance": map[string]any{"orchestrate": map[string]any{"core": []any{"brainstorm"}}},
				},
			},
		},
		"stage_profiles": map[string]any{
			"verify": map[string]any{
				"default": map[string]any{"label": "Default verify", "precondition_policy": "default"},
			},
		},
	}
	imported := map[string]any{
		"execution_profile": map[string]any{
			"by_app_type": map[string]any{
				"go-cli": map[string]any{
					// Overrides product's review.core (repo does NOT touch it), so the
					// product-vs-import order is decisive: legacy applies import AFTER
					// product, so the import value must win.
					"relevance": map[string]any{
						"review": map[string]any{"core": []any{"imported-review-lens"}},
					},
					"topology":      map[string]any{"executors": 5},
					"graph_backend": "imported-backend",
				},
			},
		},
		"stage_profiles": map[string]any{
			"review": map[string]any{
				"imported": map[string]any{"label": "Imported review"},
			},
		},
	}
	repo := map[string]any{
		"execution_profile": map[string]any{
			"by_app_type": map[string]any{
				"go-cli": map[string]any{
					"relevance": map[string]any{
						"verify": map[string]any{"situational": []any{"linters"}},
					},
					"topology": map[string]any{"executors": 2, "verifier_sequence": []any{"strict"}},
				},
			},
		},
		"stage_profiles": map[string]any{
			"verify": map[string]any{
				"strict":  map[string]any{"label": "Strict verify"},
				"default": map[string]any{"precondition_policy": "strict"}, // overrides product
			},
			"review": map[string]any{
				"adversarial": map[string]any{"label": "Adversarial"},
			},
		},
	}
	return []ResolvedLayer{
		{ID: LayerProductDefaults, Present: true, Raw: product},
		{ID: "acme:org/base", Present: true, Raw: imported}, // imported (extends) — value-merges below repo
		{ID: LayerRepoLocal, Present: true, Raw: repo},
	}
}

func canonicalJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	out, err := json.Marshal(generic)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	return string(out)
}

func TestZeroDiffMigrationAppType(t *testing.T) {
	legacy := mustResolveLayers(t, migrationLayers())
	set, err := ProfileSetFromSnapshot(legacy)
	if err != nil {
		t.Fatalf("derive profile set: %v", err)
	}
	chain := SnapshotScopeChain(legacy)

	appTypes := []string{"go-cli", "ideation"}
	stages := []string{"verify", "review", "orchestrate"}
	units := []string{"go-test", "chatty-skill", "linters", "review-lens", "brainstorm", "imported-review-lens", "unlisted-x"}

	for _, appType := range appTypes {
		ctx := ProfileContext{AppType: appType, ScopeChain: chain}
		engineProfile := decodeAppType(t, mustResolveProfile(t, set, ctx).Bundle, appType)
		legacyProfile := legacy.Effective.ExecutionProfile.ByAppType[appType]

		if !reflect.DeepEqual(engineProfile, legacyProfile) {
			t.Fatalf("STRUCTURAL DIFF for app_type %q:\n engine=%+v\n legacy=%+v", appType, engineProfile, legacyProfile)
		}
		if got, want := canonicalJSON(t, engineProfile), canonicalJSON(t, legacyProfile); got != want {
			t.Fatalf("BYTE DIFF for app_type %q:\n engine=%s\n legacy=%s", appType, got, want)
		}
		for _, stage := range stages {
			for _, unit := range units {
				engineClass := classOfBundle(&engineProfile, legacy.Effective.ExecutionProfile.EffectiveDefaultClass(), stage, unit)
				legacyClass := legacy.Effective.ExecutionProfile.ClassOf(appType, stage, unit)
				if engineClass != legacyClass {
					t.Fatalf("ClassOf diff app_type=%q stage=%q unit=%q: engine=%q legacy=%q",
						appType, stage, unit, engineClass, legacyClass)
				}
			}
		}
	}
}

// TestZeroDiffImportedLayerOrdering pins the FIX-1 behavior directly: a field set
// by BOTH product and the imported source (and NOT by repo) must resolve to the
// IMPORTED value — because legacy resolveSnapshot applies the import AFTER product.
// Authority-rank ordering (the round-1 bug) would let product win this cell.
func TestZeroDiffImportedLayerOrdering(t *testing.T) {
	legacy := mustResolveLayers(t, migrationLayers())
	set, err := ProfileSetFromSnapshot(legacy)
	if err != nil {
		t.Fatal(err)
	}
	engine := decodeAppType(t, mustResolveProfile(t, set, ProfileContext{AppType: "go-cli", ScopeChain: SnapshotScopeChain(legacy)}).Bundle, "go-cli")
	legacyGo := legacy.Effective.ExecutionProfile.ByAppType["go-cli"]

	if !reflect.DeepEqual(engine.Relevance["review"].Core, []string{"imported-review-lens"}) {
		t.Fatalf("review.core = %v, want [imported-review-lens] (import applied after product)", engine.Relevance["review"].Core)
	}
	if engine.GraphBackend != "imported-backend" {
		t.Fatalf("graph_backend = %q, want imported-backend", engine.GraphBackend)
	}
	if engine.Topology.Executors != 2 {
		t.Fatalf("executors = %d, want 2 (repo is the highest layer)", engine.Topology.Executors)
	}
	if !reflect.DeepEqual(engine, legacyGo) {
		t.Fatalf("engine != legacy on the imported-layer case:\n engine=%+v\n legacy=%+v", engine, legacyGo)
	}
}

func decodeAppType(t *testing.T, bundle map[string]any, appType string) AppTypeProfile {
	t.Helper()
	var p AppTypeProfile
	data, _ := json.Marshal(bundle)
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatalf("decode engine bundle for %q: %v", appType, err)
	}
	return p
}

// classOfBundle reproduces ExecutionProfile.ClassOf over a single decoded
// AppTypeProfile, so the engine's resolved bundle is exercised through the exact
// classification logic the consumers use.
func classOfBundle(prof *AppTypeProfile, def, stage, unit string) string {
	ep := &ExecutionProfile{DefaultClass: def, ByAppType: map[string]AppTypeProfile{"x": *prof}}
	return ep.ClassOf("x", stage, unit)
}

func TestZeroDiffMigrationStage(t *testing.T) {
	legacy := mustResolveLayers(t, migrationLayers())
	set, err := ProfileSetFromSnapshot(legacy)
	if err != nil {
		t.Fatalf("derive profile set: %v", err)
	}
	chain := SnapshotScopeChain(legacy)

	for _, stage := range []string{"verify", "review"} {
		ctx := ProfileContext{Stage: stage, ScopeChain: chain}
		var engineStage map[string]StageProfile
		data, _ := json.Marshal(mustResolveProfile(t, set, ctx).Bundle)
		if err := json.Unmarshal(data, &engineStage); err != nil {
			t.Fatalf("decode engine stage bundle for %q: %v", stage, err)
		}
		legacyStage := legacy.Effective.StageProfiles[stage]

		if !reflect.DeepEqual(engineStage, legacyStage) {
			t.Fatalf("STRUCTURAL DIFF for stage %q:\n engine=%+v\n legacy=%+v", stage, engineStage, legacyStage)
		}
		if got, want := canonicalJSON(t, engineStage), canonicalJSON(t, legacyStage); got != want {
			t.Fatalf("BYTE DIFF for stage %q:\n engine=%s\n legacy=%s", stage, got, want)
		}
	}
}

// TestZeroDiffReproducibleDigest is the H7 proof ported to the migration: two
// independent parses of the same layers yield an identical effective digest.
func TestZeroDiffReproducibleDigest(t *testing.T) {
	ctx := ProfileContext{AppType: "go-cli", ScopeChain: SnapshotScopeChain(mustResolveLayers(t, migrationLayers()))}
	d1 := digestForLayers(t, migrationLayers(), ctx)
	d2 := digestForLayers(t, migrationLayers(), ctx)
	if d1 == "" || d1 != d2 {
		t.Fatalf("digest not reproducible across independent parses: %q != %q (H7)", d1, d2)
	}
}

// TestProfileGrantUpgradesImportedScope is the FIX-3 proof: a §15 source-authority
// grant flows through ProfileSetFromSnapshot (resolveAuthorityGrants +
// applyGrantsToScopes), so an imported source is carried at its CONFERRED scope,
// not the AuthPublic default of an ungranted import.
func TestProfileGrantUpgradesImportedScope(t *testing.T) {
	layers := []ResolvedLayer{
		{ID: LayerProductDefaults, Present: true, Raw: map[string]any{}},
		{ID: "acme:base", Present: true, Raw: map[string]any{}},
		// repo (rank 2) may confer user (rank 1) on source "acme" — a strictly-lower
		// scope, honored by the §15 write-guard.
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{"authority_grants": map[string]any{"acme": "user"}}},
	}
	scopes, err := effectiveLayerScopes(layers)
	if err != nil {
		t.Fatalf("effectiveLayerScopes: %v", err)
	}
	if scopes["acme:base"] != AuthUser {
		t.Fatalf("imported scope = %q, want user (granted via §15 applyGrantsToScopes)", scopes["acme:base"])
	}
}

func mustResolveLayers(t *testing.T, layers []ResolvedLayer) *Snapshot {
	t.Helper()
	snap, err := resolveSnapshot(layers)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func digestForLayers(t *testing.T, layers []ResolvedLayer, ctx ProfileContext) string {
	t.Helper()
	snap := mustResolveLayers(t, layers)
	set, err := ProfileSetFromSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	return mustResolveProfile(t, set, ctx).Digest
}
