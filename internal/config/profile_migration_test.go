package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

// profile_migration_test.go is L1's empirical gate (spec §5.2): re-express the
// EXISTING execution_profile + stage_profiles config through the new kind:profile
// engine and PROVE ZERO behavioral diff. For a representative matrix of
// (app_type, stage) contexts the effective config resolved via the new engine
// must equal the legacy CategoryMapMerge output — structurally and byte-for-byte.

// migrationLayers builds a two-scope layer stack (product defaults + repo-local)
// carrying overlapping execution_profile + stage_profiles, so the merge actually
// crosses scopes — the case the engine must reproduce exactly.
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
	repo := map[string]any{
		"execution_profile": map[string]any{
			"by_app_type": map[string]any{
				"go-cli": map[string]any{
					// Overlays the product go-cli profile: adds a stage, overrides topology.
					"relevance": map[string]any{
						"verify": map[string]any{"situational": []any{"linters"}},
					},
					"topology":      map[string]any{"executors": 2, "verifier_sequence": []any{"strict"}},
					"graph_backend": "crg",
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
		{ID: LayerRepoLocal, Present: true, Raw: repo},
	}
}

func canonicalJSON(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// Round-trip through a generic decode so key order is canonicalized.
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
	layers := migrationLayers()
	legacy, err := resolveSnapshot(layers)
	if err != nil {
		t.Fatalf("legacy resolve: %v", err)
	}
	set, err := ProfileSetFromSnapshot(legacy)
	if err != nil {
		t.Fatalf("derive profile set: %v", err)
	}
	chain := SnapshotScopeChain(legacy)

	// Matrix: every app_type the legacy execution_profile handles.
	appTypes := []string{"go-cli", "ideation"}
	stages := []string{"verify", "review", "orchestrate"}
	units := []string{"go-test", "chatty-skill", "linters", "review-lens", "brainstorm", "unlisted-x"}

	for _, appType := range appTypes {
		ctx := ProfileContext{AppType: appType, ScopeChain: chain}
		engineBundle := ResolveProfile(set, ctx).Bundle

		// Decode the engine bundle back through the SAME typed lens the consumers
		// read, and compare to the legacy merged AppTypeProfile.
		var engineProfile AppTypeProfile
		data, _ := json.Marshal(engineBundle)
		if err := json.Unmarshal(data, &engineProfile); err != nil {
			t.Fatalf("decode engine bundle for %q: %v", appType, err)
		}
		legacyProfile := legacy.Effective.ExecutionProfile.ByAppType[appType]

		if !reflect.DeepEqual(engineProfile, legacyProfile) {
			t.Fatalf("STRUCTURAL DIFF for app_type %q:\n engine=%+v\n legacy=%+v", appType, engineProfile, legacyProfile)
		}
		if got, want := canonicalJSON(t, engineProfile), canonicalJSON(t, legacyProfile); got != want {
			t.Fatalf("BYTE DIFF for app_type %q:\n engine=%s\n legacy=%s", appType, got, want)
		}

		// ClassOf matrix: every (stage, unit) must classify identically.
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

// classOfBundle reproduces ExecutionProfile.ClassOf over a single decoded
// AppTypeProfile, so the engine's resolved bundle is exercised through the exact
// classification logic the consumers use.
func classOfBundle(prof *AppTypeProfile, def, stage, unit string) string {
	ep := &ExecutionProfile{DefaultClass: def, ByAppType: map[string]AppTypeProfile{"x": *prof}}
	return ep.ClassOf("x", stage, unit)
}

func TestZeroDiffMigrationStage(t *testing.T) {
	layers := migrationLayers()
	legacy, err := resolveSnapshot(layers)
	if err != nil {
		t.Fatalf("legacy resolve: %v", err)
	}
	set, err := ProfileSetFromSnapshot(legacy)
	if err != nil {
		t.Fatalf("derive profile set: %v", err)
	}
	chain := SnapshotScopeChain(legacy)

	for _, stage := range []string{"verify", "review"} {
		ctx := ProfileContext{Stage: stage, ScopeChain: chain}
		engineBundle := ResolveProfile(set, ctx).Bundle

		var engineStage map[string]StageProfile
		data, _ := json.Marshal(engineBundle)
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
	return ResolveProfile(set, ctx).Digest
}
