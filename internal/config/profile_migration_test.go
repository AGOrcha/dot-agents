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

// grantedImportLayers is a stack where repo (rank 2) confers user (rank 1) on the
// imported source "acme" — a strictly-lower scope honored by the §15 write-guard.
// The imported layer carries an execution_profile so a real profile is DERIVED
// from the granted source.
func grantedImportLayers() []ResolvedLayer {
	return []ResolvedLayer{
		{ID: LayerProductDefaults, Present: true, Raw: map[string]any{}},
		{ID: "acme:base", Present: true, Raw: map[string]any{
			"execution_profile": map[string]any{
				"by_app_type": map[string]any{
					"go-cli": map[string]any{"graph_backend": "imported"},
				},
			},
		}},
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"authority_grants": map[string]any{"acme": "user"},
		}},
	}
}

// TestProfileGrantUpgradesImportedScope is the FIX-3 proof: a §15 source-authority
// grant flows through ProfileSetFromSnapshot (resolveAuthorityGrants +
// applyGrantsToScopes), so an imported source — and the PROFILE DERIVED from it —
// is carried at its CONFERRED scope, not the AuthPublic default of an ungranted
// import.
func TestProfileGrantUpgradesImportedScope(t *testing.T) {
	snap := mustResolveLayers(t, grantedImportLayers())

	// (a) the layer scope is upgraded.
	scopes, err := effectiveLayerScopes(snap.Layers)
	if err != nil {
		t.Fatalf("effectiveLayerScopes: %v", err)
	}
	if scopes["acme:base"] != AuthUser {
		t.Fatalf("imported layer scope = %q, want user", scopes["acme:base"])
	}

	// (b) the DERIVED profile from that granted source carries the conferred scope,
	// NOT AuthPublic — the assertion the round-3 review asked for.
	set, err := ProfileSetFromSnapshot(snap)
	if err != nil {
		t.Fatalf("derive profile set: %v", err)
	}
	found := false
	for _, p := range set.Profiles {
		if p.Ref == "acme:base:execution-profile:go-cli" {
			found = true
			if p.Scope != AuthUser {
				t.Fatalf("derived profile %q scope = %q, want user (granted authority, not AuthPublic)", p.Ref, p.Scope)
			}
		}
	}
	if !found {
		t.Fatal("expected a profile derived from the granted imported source")
	}
}

// TestDigestDistinguishesSourceSets is the FIX-4 proof: two DISTINCT source sets
// with byte-EQUAL resolved values produce DIFFERENT digests, because the digest
// covers the contributing absolute refs (which carry source provenance) — not the
// bundle values alone (Decision 7). A digest over values alone would call these
// identical and miss a provenance change.
func TestDigestDistinguishesSourceSets(t *testing.T) {
	ep := map[string]any{
		"by_app_type": map[string]any{
			"go-cli": map[string]any{"topology": map[string]any{"executors": 2}},
		},
	}
	importFrom := func(source string) ResolvedProfile {
		snap := mustResolveLayers(t, []ResolvedLayer{
			{ID: LayerProductDefaults, Present: true, Raw: map[string]any{}},
			{ID: source, Present: true, Raw: map[string]any{"execution_profile": ep}},
		})
		return mustResolveProfileFromSnapshot(t, snap, "go-cli")
	}
	a := importFrom("acme:base")
	b := importFrom("other:base")

	if !reflect.DeepEqual(a.Bundle, b.Bundle) {
		t.Fatalf("test setup: bundles must be byte-equal\n a=%+v\n b=%+v", a.Bundle, b.Bundle)
	}
	if a.Digest == b.Digest {
		t.Fatalf("digests must differ for distinct source sets with equal values (Decision 7 / FIX 4): both %s", a.Digest)
	}
	if !reflect.DeepEqual(a.Contributing, []string{"acme:base:execution-profile:go-cli"}) {
		t.Fatalf("contributing ref must carry source provenance: %v", a.Contributing)
	}
}

// TestZeroDiffGateWithCapabilityLocks consolidates the lock-divergence cases into
// the SAME widened migration fixture (round-3 ask #3): on the three-layer
// product→import→repo stack, it asserts BOTH (a) layering a capability profile +
// a team deny-lock + an org allow on top leaves the app_type zero-diff against
// legacy CategoryMapMerge UNCHANGED (locks on capability fields never touch
// execution_profile), AND (b) the higher org allow survives the lower team deny in
// that same multi-scope context — so a future regression in either path is caught
// by the gate fixture, not only by isolated unit tests.
func TestZeroDiffGateWithCapabilityLocks(t *testing.T) {
	legacy := mustResolveLayers(t, migrationLayers())
	set, err := ProfileSetFromSnapshot(legacy)
	if err != nil {
		t.Fatal(err)
	}
	// Layer a capability profile (org grants Edit) + a team deny-lock onto the
	// SAME derived set the gate uses.
	set.Profiles = append(set.Profiles, capProfile("org:cap", AuthOrg, ProfileSelector{Role: "reviewer"}, "Edit", "Read"))
	set.Policies = append(set.Policies, denyLockPolicy(AuthTeam, "tools_allow", "Edit"))
	chain := append(SnapshotScopeChain(legacy), AuthOrg, AuthTeam)

	// (a) the app_type bundle still equals legacy — the capability lock did not
	// perturb the execution_profile merge.
	engine := decodeAppType(t, mustResolveProfile(t, set, ProfileContext{AppType: "go-cli", ScopeChain: chain}).Bundle, "go-cli")
	if !reflect.DeepEqual(engine, legacy.Effective.ExecutionProfile.ByAppType["go-cli"]) {
		t.Fatalf("app_type zero-diff broke when a capability lock was layered on:\n engine=%+v", engine)
	}

	// (b) higher-allow-survives-lower-deny holds in the same multi-scope context:
	// org (rank 4) granted Edit; team (rank 3) deny cannot subtract it.
	cap := toolsAllow(mustResolveProfile(t, set, ProfileContext{Role: "reviewer", AppType: "go-cli", ScopeChain: chain}).Bundle)
	if !reflect.DeepEqual(cap, []string{"Edit", "Read"}) {
		t.Fatalf("tools_allow = %v, want [Edit Read] (org allow survives team deny in the gate fixture)", cap)
	}
}

// mustResolveProfileFromSnapshot resolves a single app_type context through the
// snapshot bridge.
func mustResolveProfileFromSnapshot(t *testing.T, snap *Snapshot, appType string) ResolvedProfile {
	t.Helper()
	got, err := ResolveProfileContext(snap, "", appType, "", "")
	if err != nil {
		t.Fatalf("ResolveProfileContext: %v", err)
	}
	return got
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
