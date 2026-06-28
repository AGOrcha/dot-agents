package config

import (
	"encoding/json"
	"strings"
	"testing"
)

// Shared literals — kept as consts so the same ref is not duplicated across cases
// (avoids the S1192 string-duplication smell).
const (
	mfSrcBase  = "acme:base@v1"
	mfSrcTools = "acme:tools@v2"
	mfBindRef  = "acme:go-cli-profile"
	mfPSRef    = "acme:team-repos"
	mfRefTeam  = "acme:team-base"
	mfProfRef  = "acme:p"
)

func sampleManifest() ConfigManifest {
	return ConfigManifest{Ref: mfRefTeam, Scope: AuthRepo, Spec: ManifestSpec{
		Sources:    []string{mfSrcTools, mfSrcBase}, // intentionally unsorted
		Binds:      []string{mfBindRef},
		ProjectSet: mfPSRef,
	}}
}

func mustResolveManifest(t *testing.T, m ConfigManifest, set ProfileSet, ctx ProfileContext) ResolvedManifest {
	t.Helper()
	got, err := ResolveManifest(m, set, ctx)
	if err != nil {
		t.Fatalf("ResolveManifest: %v", err)
	}
	return got
}

// A manifest unit resolves to its source-set + bound policy + project-set ref.
func TestResolveManifestYieldsSourceSetPolicyProjectSet(t *testing.T) {
	got := mustResolveManifest(t, sampleManifest(), ProfileSet{}, ProfileContext{ScopeChain: []AuthorityScope{AuthRepo}})
	if got.Ref != mfRefTeam || got.Scope != AuthRepo {
		t.Fatalf("identity/scope wrong: %+v", got)
	}
	if len(got.Sources) != 2 || got.Sources[0] != mfSrcBase || got.Sources[1] != mfSrcTools {
		t.Fatalf("source-set not sorted/carried: %v", got.Sources)
	}
	if !got.HasProjectSet || got.ProjectSet != mfPSRef {
		t.Fatalf("project-set ref not carried: %+v", got)
	}
	if got.Digest == "" || got.Policy.Digest == "" {
		t.Fatalf("expected non-empty manifest + policy digests: %+v", got)
	}
}

// A manifest with no project-set still resolves (item 3).
func TestResolveManifestWithoutProjectSet(t *testing.T) {
	m := ConfigManifest{Ref: mfRefTeam, Scope: AuthRepo, Spec: ManifestSpec{Sources: []string{mfSrcBase}}}
	got := mustResolveManifest(t, m, ProfileSet{}, ProfileContext{ScopeChain: []AuthorityScope{AuthRepo}})
	if got.HasProjectSet || got.ProjectSet != "" {
		t.Fatalf("expected no project-set, got %+v", got)
	}
	if got.Digest == "" {
		t.Fatal("a project-set-less manifest must still produce a digest")
	}
}

// Transitive-pin digest (F5): stable for identical inputs, moves when any
// referenced ref changes.
func TestManifestTransitivePinDigestOnRefChange(t *testing.T) {
	ctx := ProfileContext{ScopeChain: []AuthorityScope{AuthRepo}}
	base := sampleManifest()
	d1 := mustResolveManifest(t, base, ProfileSet{}, ctx).Digest
	if d1 != mustResolveManifest(t, base, ProfileSet{}, ctx).Digest {
		t.Fatal("digest must be stable for identical inputs (order-independent)")
	}

	movedSource := base
	movedSource.Spec.Sources = []string{mfSrcBase, "acme:tools@v3"}
	if mustResolveManifest(t, movedSource, ProfileSet{}, ctx).Digest == d1 {
		t.Fatal("digest must move when a referenced source ref changes (F5)")
	}

	movedPS := base
	movedPS.Spec.ProjectSet = "acme:other-repos"
	if mustResolveManifest(t, movedPS, ProfileSet{}, ctx).Digest == d1 {
		t.Fatal("digest must move when the project-set ref changes (F5)")
	}

	movedBind := base
	movedBind.Spec.Binds = []string{"acme:other-profile"}
	if mustResolveManifest(t, movedBind, ProfileSet{}, ctx).Digest == d1 {
		t.Fatal("digest must move when a bound ref changes (F5)")
	}
}

// Transitive over a referenced unit's RESOLVED version: the same manifest spec
// resolves to a different digest when a bound unit's content changes (F5).
func TestManifestDigestMovesWhenBoundUnitContentChanges(t *testing.T) {
	ctx := ProfileContext{AppType: "go-cli", ScopeChain: []AuthorityScope{AuthRepo}}
	m := ConfigManifest{Ref: mfRefTeam, Scope: AuthRepo, Spec: ManifestSpec{Binds: []string{mfProfRef}}}
	mk := func(v string) ProfileSet {
		return ProfileSet{Profiles: []ConfigProfile{{
			Ref: mfProfRef, Kind: ProfileKindAppType, Scope: AuthRepo,
			Selector: ProfileSelector{AppType: "go-cli"}, Bundle: map[string]any{"x": v},
		}}}
	}
	if mustResolveManifest(t, m, mk("1"), ctx).Digest == mustResolveManifest(t, m, mk("2"), ctx).Digest {
		t.Fatal("manifest digest must move when a bound unit's resolved content changes (F5 transitive)")
	}
}

// bindProfileSet filters profiles by ref but ALWAYS keeps policies — a member
// cannot escape an org/team lock by binding only its own profiles (D4/R6).
func TestBindProfileSetFiltersProfilesKeepsPolicies(t *testing.T) {
	set := ProfileSet{
		Profiles: []ConfigProfile{{Ref: "acme:keep"}, {Ref: "acme:drop"}},
		Policies: []LayeringPolicy{{Scope: AuthOrg}},
	}
	if all := bindProfileSet(set, nil); len(all.Profiles) != 2 {
		t.Fatalf("empty bind list must bind all profiles, got %d", len(all.Profiles))
	}
	bound := bindProfileSet(set, []string{"acme:keep"})
	if len(bound.Profiles) != 1 || bound.Profiles[0].Ref != "acme:keep" {
		t.Fatalf("bind filter wrong: %+v", bound.Profiles)
	}
	if len(bound.Policies) != 1 {
		t.Fatal("policies must always be kept (authority binds by scope, not by bind list)")
	}
}

// A fatal L1 authority violation (here: overlapping value-locks owned by an
// authoritative scope) propagates out of ResolveManifest fail-closed.
func TestResolveManifestPropagatesAuthorityError(t *testing.T) {
	val := json.RawMessage(`"x"`)
	policy := LayeringPolicy{Scope: AuthRepo, Locks: []ProfileLock{
		{Field: "features", Value: val, Owner: AuthRepo},
		{Field: "features.graph", Value: val, Owner: AuthRepo},
	}}
	set := ProfileSet{Policies: []LayeringPolicy{policy}}
	m := ConfigManifest{Ref: mfRefTeam, Scope: AuthRepo}
	if _, err := ResolveManifest(m, set, ProfileContext{ScopeChain: []AuthorityScope{AuthRepo}}); err == nil {
		t.Fatal("expected a fatal §15 authority violation to propagate")
	}
}

// decodeManifest fails closed on a manifest->manifest edge, a self-declared
// authority claim, force-allow, an unknown field, malformed JSON, and a malformed
// ref (R10) — authority is source-derived, never self-granted.
func TestDecodeManifestRejectsForbiddenAndMalformed(t *testing.T) {
	cases := []struct{ name, raw string }{
		{"extends", `{"extends":["a:b"]}`},
		{"inherits", `{"inherits":"x"}`},
		{"composes", `{"composes":["y"]}`},
		{"authority", `{"authority":"org"}`},
		{"scope", `{"scope":"org"}`},
		{"authority_grants", `{"authority_grants":{"acme":"org"}}`},
		{"force_allow", `{"force_allow":["x"]}`},
		{"unknown_field", `{"bogus":1}`},
		{"malformed_json", `{`},
		{"bad_source_ref", `{"sources":["nocolon"]}`},
		{"bad_bind_ref", `{"binds":[":noname"]}`},
		{"bad_projectset_ref", `{"project_set":"trailing:"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeManifest([]byte(tc.raw), "acme:m", AuthRepo); err == nil {
				t.Fatalf("expected a validation error for %s", tc.name)
			}
		})
	}
}

func TestDecodeManifestValid(t *testing.T) {
	m, err := decodeManifest([]byte(`{"sources":["acme:base@v1"],"binds":["acme:p"],"project_set":"acme:repos"}`), "acme:team", AuthOrg)
	if err != nil {
		t.Fatal(err)
	}
	if m.Ref != "acme:team" || m.Scope != AuthOrg {
		t.Fatalf("identity/scope not loader-stamped: %+v", m)
	}
	if len(m.Spec.Sources) != 1 || m.Spec.ProjectSet != "acme:repos" {
		t.Fatalf("spec mis-decoded: %+v", m.Spec)
	}
}

func TestManifestsFromLayer(t *testing.T) {
	if out, err := manifestsFromLayer(map[string]any{}, AuthRepo, "repo"); err != nil || out != nil {
		t.Fatalf("a layer with no manifests must be a no-op: %v %v", out, err)
	}
	if _, err := manifestsFromLayer(map[string]any{"manifests": "x"}, AuthRepo, "repo"); err == nil {
		t.Fatal("a non-object manifests value must fail closed")
	}
	bad := map[string]any{"manifests": map[string]any{"m": map[string]any{"extends": []any{"a:b"}}}}
	if _, err := manifestsFromLayer(bad, AuthRepo, "repo"); err == nil {
		t.Fatal("a manifest validation error must propagate")
	}
	good := map[string]any{"manifests": map[string]any{
		"beta":  map[string]any{"sources": []any{mfSrcBase}},
		"alpha": map[string]any{"sources": []any{mfSrcBase}},
	}}
	ms, err := manifestsFromLayer(good, AuthRepo, "repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 || ms[0].Ref != "repo:alpha" || ms[1].Ref != "repo:beta" {
		t.Fatalf("manifests not name-sorted / source-ref'd: %+v", ms)
	}
	if ms[0].Scope != AuthRepo {
		t.Fatalf("scope not stamped from layer: %+v", ms[0])
	}
}

// A public-source manifest is AuthPublic (source-derived); a repo manifest is
// AuthRepo. A nil-Raw layer is skipped.
func TestManifestSetFromSnapshotSourceDerivedScope(t *testing.T) {
	repoRef := manifestRef(LayerRepoLocal, "team")
	snap := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerProductDefaults, Present: true, Raw: nil},
		{ID: "acme:org/base", Present: true, Raw: map[string]any{
			"manifests": map[string]any{"pub": map[string]any{"sources": []any{mfSrcBase}}},
		}},
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"manifests": map[string]any{"team": map[string]any{"sources": []any{mfSrcBase}}},
		}},
	}}
	ms, err := ManifestSetFromSnapshot(snap)
	if err != nil {
		t.Fatal(err)
	}
	byRef := map[string]ConfigManifest{}
	for _, m := range ms {
		byRef[m.Ref] = m
	}
	if byRef["acme:org/base:pub"].Scope != AuthPublic {
		t.Fatalf("ungranted public-source manifest must be AuthPublic, got %q", byRef["acme:org/base:pub"].Scope)
	}
	if byRef[repoRef].Scope != AuthRepo {
		t.Fatalf("repo manifest must be AuthRepo, got %q", byRef[repoRef].Scope)
	}
}

// A self-blessing authority grant fails the resolve through the manifest bridge —
// a public/untrusted manifest cannot self-grant authority (reused §15 invariant).
func TestManifestSetFromSnapshotSelfBlessingFails(t *testing.T) {
	snap := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerUserLocal, Present: true, Raw: map[string]any{
			"authority_grants": map[string]any{"acme": "org"},
			"manifests":        map[string]any{"m": map[string]any{"sources": []any{mfSrcBase}}},
		}},
	}}
	if _, err := ManifestSetFromSnapshot(snap); err == nil {
		t.Fatal("a self-blessing authority_grant must fail closed")
	}
}

func TestManifestSetFromSnapshotLayerError(t *testing.T) {
	snap := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"manifests": map[string]any{"m": map[string]any{"extends": []any{"a:b"}}},
		}},
	}}
	if _, err := ManifestSetFromSnapshot(snap); err == nil {
		t.Fatal("a per-layer manifest validation error must propagate")
	}
}

func TestResolveManifestFromSnapshot(t *testing.T) {
	ref := manifestRef(LayerRepoLocal, "team")
	snap := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"manifests": map[string]any{"team": map[string]any{
				"sources": []any{mfSrcBase}, "project_set": "acme:repos",
			}},
		}},
	}}
	got, err := ResolveManifestFromSnapshot(snap, ref, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Ref != ref || !got.HasProjectSet || got.Digest == "" {
		t.Fatalf("unexpected resolve: %+v", got)
	}
	if _, err := ResolveManifestFromSnapshot(snap, "acme:missing", "", "", "", ""); err == nil {
		t.Fatal("a ref naming no declared manifest must be a loud not-found error")
	}
}

func TestResolveManifestFromSnapshotManifestSetError(t *testing.T) {
	snap := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerUserLocal, Present: true, Raw: map[string]any{
			"authority_grants": map[string]any{"acme": "org"},
		}},
	}}
	if _, err := ResolveManifestFromSnapshot(snap, "x:y", "", "", "", ""); err == nil {
		t.Fatal("a ManifestSetFromSnapshot error must propagate")
	}
}

func TestResolveManifestFromSnapshotProfileSetError(t *testing.T) {
	ref := manifestRef(LayerRepoLocal, "team")
	snap := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"manifests":       map[string]any{"team": map[string]any{"sources": []any{mfSrcBase}}},
			"layering_policy": map[string]any{"mode": "bogus"},
		}},
	}}
	if _, err := ResolveManifestFromSnapshot(snap, ref, "", "", "", ""); err == nil {
		t.Fatal("a malformed layering_policy must fail the profile-set derivation closed")
	}
}

func TestManifestRefHelpers(t *testing.T) {
	if manifestRef("", "n") != "n" {
		t.Fatal("empty source must yield the bare name")
	}
	if manifestRef("s", "n") != "s:n" {
		t.Fatal("source+name must yield <source>:<name>")
	}
	for _, bad := range []string{"", "nocolon", ":x", "x:"} {
		if validManifestRef(bad) {
			t.Fatalf("invalid ref %q accepted", bad)
		}
	}
	if !validManifestRef("a:b") {
		t.Fatal("a valid <source>:<name> ref was rejected")
	}
}

// AgentsRC lifecycle: the typed manifests field marshals, round-trips, and does
// NOT leak into ExtraFields (agentsRCKnown sync).
func TestAgentsRCManifestsRoundTrip(t *testing.T) {
	rc := AgentsRC{Version: 1, Manifests: map[string]ManifestSpec{
		"team": {Sources: []string{mfSrcBase}, Binds: []string{mfProfRef}, ProjectSet: "acme:repos"},
	}}
	data, err := json.Marshal(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"manifests"`) {
		t.Fatalf("manifests not emitted: %s", data)
	}
	var back AgentsRC
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.Manifests["team"].ProjectSet != "acme:repos" {
		t.Fatalf("round-trip lost the manifest: %+v", back.Manifests)
	}
	if _, leaked := back.ExtraFields["manifests"]; leaked {
		t.Fatal("manifests leaked into ExtraFields — agentsRCKnown out of sync")
	}
}

// FIX D.1: the resolved scope is part of the pinned setup, so manifestDigest must
// include it — two manifests identical but for resolved scope must differ.
func TestManifestDigestIncludesResolvedScope(t *testing.T) {
	ctx := ProfileContext{ScopeChain: []AuthorityScope{AuthRepo}}
	m1 := sampleManifest() // AuthRepo
	m2 := m1
	m2.Scope = AuthOrg
	if mustResolveManifest(t, m1, ProfileSet{}, ctx).Digest == mustResolveManifest(t, m2, ProfileSet{}, ctx).Digest {
		t.Fatal("manifestDigest must include the resolved authority scope (FIX D)")
	}
}

// FIX D.1 end-to-end: a §15 authority GRANT that elevates the manifest's imported
// source changes its resolved scope — and the pin must move with it.
func TestManifestDigestMovesWhenGrantChangesResolvedScope(t *testing.T) {
	mref := manifestRef("acme:base", "m")
	importLayer := ResolvedLayer{ID: "acme:base", Present: true, Raw: map[string]any{
		"manifests": map[string]any{"m": map[string]any{"sources": []any{mfSrcBase}}},
	}}
	noGrant := &Snapshot{Layers: []ResolvedLayer{importLayer}}
	d1, err := ResolveManifestFromSnapshot(noGrant, mref, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	// A repo-local layer (rank 2) may grant the imported "acme" source the user
	// scope (rank 1) — a strictly-lower conferred rank, so it is honored.
	granted := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"authority_grants": map[string]any{"acme": "user"},
		}},
		importLayer,
	}}
	d2, err := ResolveManifestFromSnapshot(granted, mref, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if d1.Scope != AuthPublic || d2.Scope != AuthUser {
		t.Fatalf("grant must change resolved scope: %q -> %q (want public -> user)", d1.Scope, d2.Scope)
	}
	if d1.Digest == d2.Digest {
		t.Fatal("a grant change that alters the resolved scope must move the digest (FIX D)")
	}
}

// FIX D.2: an unpinned/mutable source ref is rejected; a pinned ref resolves.
func TestManifestRejectsUnpinnedSource(t *testing.T) {
	unpinned := []string{
		`{"sources":["acme:base"]}`,                    // no @version
		`{"sources":["acme:base@"]}`,                   // empty version
		`{"sources":["acme:@v1"]}`,                     // empty path
		`{"sources":[":base@v1"]}`,                     // empty source
		`{"sources":["acme:tools@v2","plain-no-pin"]}`, // one good, one unpinned
	}
	for _, raw := range unpinned {
		if _, err := decodeManifest([]byte(raw), "acme:m", AuthRepo); err == nil {
			t.Fatalf("expected an unpinned source ref to be rejected: %s", raw)
		}
	}
	if _, err := decodeManifest([]byte(`{"sources":["acme:base@v1"]}`), "acme:m", AuthRepo); err != nil {
		t.Fatalf("a pinned source ref must resolve: %v", err)
	}
}

// FIX C: the fail-closed gate holds on the TYPED AgentsRC load path too — a
// forbidden field or an unpinned source on the typed manifests map is rejected at
// load (json.Unmarshal), never silently dropped before Save re-emits it.
func TestManifestTypedPathFailsClosed(t *testing.T) {
	cases := map[string]string{
		"forbidden_extends": `{"version":1,"manifests":{"m":{"extends":["a:b"]}}}`,
		"forbidden_grant":   `{"version":1,"manifests":{"m":{"authority_grants":{"acme":"org"}}}}`,
		"unknown_field":     `{"version":1,"manifests":{"m":{"bogus":1}}}`,
		"unpinned_source":   `{"version":1,"manifests":{"m":{"sources":["acme:base"]}}}`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			var rc AgentsRC
			if err := json.Unmarshal([]byte(raw), &rc); err == nil {
				t.Fatalf("typed path must reject %s at load, not silently drop it", name)
			}
		})
	}
	// A valid typed manifest still loads.
	var ok AgentsRC
	if err := json.Unmarshal([]byte(`{"version":1,"manifests":{"m":{"sources":["acme:base@v1"]}}}`), &ok); err != nil {
		t.Fatalf("a valid typed manifest must load: %v", err)
	}
	if len(ok.Manifests["m"].Sources) != 1 {
		t.Fatalf("typed manifest not decoded: %+v", ok.Manifests)
	}
}

// FIX F: a generate/refresh-style merge over an AgentsRC with authored manifests
// must PRESERVE them — they no longer ride in ExtraFields now that they are typed.
func TestMergeGenerateAgentsRCPreservesManifests(t *testing.T) {
	existing := &AgentsRC{Version: 1, Manifests: map[string]ManifestSpec{
		"team": {Sources: []string{mfSrcBase}, Binds: []string{mfProfRef}, ProjectSet: "acme:repos"},
	}}
	generated := &AgentsRC{Version: 1} // a fresh scan carries no manifests
	merged := MergeGenerateAgentsRC(existing, generated)
	got, ok := merged.Manifests["team"]
	if !ok {
		t.Fatal("merge dropped the authored manifest (typed-field/ExtraFields breakage, FIX F)")
	}
	if got.ProjectSet != "acme:repos" || len(got.Sources) != 1 {
		t.Fatalf("merge did not preserve the manifest payload: %+v", got)
	}
	// The clone must not alias the existing manifest's slices.
	got.Sources[0] = "mutated:x@v9"
	if existing.Manifests["team"].Sources[0] != mfSrcBase {
		t.Fatal("cloneManifests aliased the existing manifest's source slice")
	}
	if cloneManifests(nil) != nil {
		t.Fatal("cloneManifests(nil) must be nil")
	}
}
