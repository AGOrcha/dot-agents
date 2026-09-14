package config

// Contract coverage for the provenance-preserving resource-source plan
// (source_plan.go) — the surface `da install` resolves project resource roots
// from instead of the raw repo `sources` or the ordered-replace
// Effective.Sources.

import (
	"reflect"
	"testing"
)

// planLayers builds a Snapshot carrying only the layer stack, which is all
// ResourceSourcePlan reads.
func planLayers(layers ...ResolvedLayer) *Snapshot {
	return &Snapshot{Layers: layers}
}

// rawLayer is a ResolvedLayer whose sources are given as decoded JSON, matching
// what the resolver stores in ResolvedLayer.Raw.
func rawLayer(id string, sources ...map[string]any) ResolvedLayer {
	list := make([]any, 0, len(sources))
	for _, s := range sources {
		list = append(list, s)
	}
	return ResolvedLayer{ID: id, Present: true, Raw: map[string]any{"sources": list}}
}

func planEntry(t *testing.T, plan ResourceSourcePlan, layer string) ResourceSource {
	t.Helper()
	for _, rs := range plan.Sources {
		if rs.Layer == layer {
			return rs
		}
	}
	t.Fatalf("plan has no entry declared by layer %q: %+v", layer, plan.Sources)
	return ResourceSource{}
}

func planPaths(plan ResourceSourcePlan) []string {
	out := make([]string, 0, len(plan.Sources))
	for _, src := range plan.ProjectSources() {
		out = append(out, src.Path)
	}
	return out
}

// TestSourceIsDefaultHomeLocal pins the classification boundary the whole plan
// rests on: only the BARE `{"type":"local"}` sentinel names the user's home.
// Any authored detail on a local source makes it a real project declaration.
func TestSourceIsDefaultHomeLocal(t *testing.T) {
	if !(Source{Type: "local"}).IsDefaultHomeLocal() {
		t.Error("the bare local sentinel must be recognized as the default user-home root")
	}
	notSentinel := []Source{
		{Type: "local", Path: "./vendor/agents"},
		{Type: "local", ID: "org"},
		{Type: "local", CacheTTL: "4h"},
		{Type: "local", Scope: SourceScopeRepo},
		{Type: "local", Owner: "acme"},
		{Type: "git", URL: "https://example/x.git"},
		{Type: ""},
	}
	for _, src := range notSentinel {
		if src.IsDefaultHomeLocal() {
			t.Errorf("%+v must stay an authored project source", src)
		}
	}
}

// TestResourceSourcePlan_ProvenanceAndEligibility is the core classification
// contract: inherited layer sources are admitted, user-scope layers and the
// synthesized home sentinel are not, and every entry carries the provenance
// that justifies the decision.
func TestResourceSourcePlan_ProvenanceAndEligibility(t *testing.T) {
	snap := planLayers(
		rawLayer(LayerProductDefaults, map[string]any{"id": "builtin", "type": "local", "path": "/opt/da"}),
		rawLayer(LayerUserLocal, map[string]any{"id": "mine", "type": "local", "path": "/home/u/scratch"}),
		rawLayer("org:org/base.json", map[string]any{"id": "org", "type": "local", "path": "/srv/org"}),
		rawLayer("team:team/base.json", map[string]any{"id": "team", "type": "local", "path": "/srv/team"}),
		rawLayer(LayerRepoLocal,
			map[string]any{"type": "local"},
			map[string]any{"id": "repo", "type": "local", "path": "./vendor/agents"},
		),
	)

	plan, err := snap.ResourceSourcePlan()
	if err != nil {
		t.Fatalf("ResourceSourcePlan: %v", err)
	}

	// Search order is highest precedence first: repo-local, then the nearest
	// imported layer, then its ancestor. Neither user-scope root appears, and
	// the repo's synthesized home sentinel is filtered out.
	wantPaths := []string{"./vendor/agents", "/srv/team", "/srv/org"}
	if got := planPaths(plan); !reflect.DeepEqual(got, wantPaths) {
		t.Errorf("project source paths = %v, want %v", got, wantPaths)
	}

	assertUserScopeLayersIneligible(t, plan)
	assertInheritanceProvenance(t, plan)
	assertHomeSentinelRecorded(t, plan)
}

// assertUserScopeLayersIneligible: neither machine-scope layer may supply a
// project resource root, and each entry carries the reason that says so.
func assertUserScopeLayersIneligible(t *testing.T, plan ResourceSourcePlan) {
	t.Helper()
	for _, layer := range []string{LayerUserLocal, LayerProductDefaults} {
		if e := planEntry(t, plan, layer); e.ProjectEligible || e.IneligibleReason != IneligibleUserLayer {
			t.Errorf("%s source = %+v, want ineligible with reason %q", layer, e, IneligibleUserLayer)
		}
	}
}

// assertInheritanceProvenance: a root that reached the project through
// `extends` is marked inherited and stays eligible; the project's own
// declaration is neither inherited nor attributed elsewhere.
func assertInheritanceProvenance(t *testing.T, plan ResourceSourcePlan) {
	t.Helper()
	if e := planEntry(t, plan, "org:org/base.json"); !e.ProjectEligible || !e.Inherited {
		t.Errorf("org layer source = %+v, want eligible and inherited", e)
	}
	if e := planEntry(t, plan, LayerRepoLocal); e.Inherited {
		t.Errorf("repo-local source must not be marked inherited: %+v", e)
	}
}

// assertHomeSentinelRecorded: the sentinel is retained in the plan (so a
// caller can explain the skip) but is never a project source.
func assertHomeSentinelRecorded(t *testing.T, plan ResourceSourcePlan) {
	t.Helper()
	for _, rs := range plan.Sources {
		if !rs.Source.IsDefaultHomeLocal() {
			continue
		}
		if rs.ProjectEligible || rs.IneligibleReason != IneligibleDefaultHome {
			t.Errorf("home sentinel = %+v, want ineligible with reason %q", rs, IneligibleDefaultHome)
		}
		return
	}
	t.Error("the plan must record the synthesized home sentinel, not drop it silently")
}

// TestResourceSourcePlan_HigherLayerShadowsSameID pins de-duplication: an id a
// repo re-declares resolves to the REPO's root, once, mirroring the
// layer-local shadowing the extends walk already performs.
func TestResourceSourcePlan_HigherLayerShadowsSameID(t *testing.T) {
	snap := planLayers(
		rawLayer("team:team/base.json", map[string]any{"id": "shared", "type": "local", "path": "/srv/team"}),
		rawLayer(LayerRepoLocal, map[string]any{"id": "shared", "type": "local", "path": "/srv/repo"}),
	)
	plan, err := snap.ResourceSourcePlan()
	if err != nil {
		t.Fatalf("ResourceSourcePlan: %v", err)
	}
	if got := planPaths(plan); !reflect.DeepEqual(got, []string{"/srv/repo"}) {
		t.Errorf("paths = %v, want the repo-local declaration only", got)
	}
}

// TestResourceSourcePlan_MalformedLayerSourcesFailLoud: a source list that
// cannot decode must abort the plan. Silently returning a shorter search path
// is how an inherited skill becomes an unexplained "not found in any source".
func TestResourceSourcePlan_MalformedLayerSourcesFailLoud(t *testing.T) {
	snap := planLayers(ResolvedLayer{
		ID:      LayerRepoLocal,
		Present: true,
		Raw:     map[string]any{"sources": "not-an-array"},
	})
	if _, err := snap.ResourceSourcePlan(); err == nil {
		t.Fatal("a malformed sources declaration must fail the plan")
	}
}

// TestRepoResourceSourcePlan_FiltersSynthesizedHomeRoot covers the no-snapshot
// path (dry-run before the first resolve) against the exact shape LoadAgentsRC
// synthesizes for a manifest that declares no sources.
func TestRepoResourceSourcePlan_FiltersSynthesizedHomeRoot(t *testing.T) {
	rc := &AgentsRC{Sources: []Source{{Type: "local"}}}
	if got := RepoResourceSourcePlan(rc).ProjectSources(); len(got) != 0 {
		t.Errorf("synthesized sources = %v, want no project roots", got)
	}

	authored := &AgentsRC{Sources: []Source{{Type: "local"}, {Type: "local", Path: "./vendor/agents"}}}
	got := RepoResourceSourcePlan(authored).ProjectSources()
	if len(got) != 1 || got[0].Path != "./vendor/agents" {
		t.Errorf("authored sources = %+v, want only the path-bearing local source", got)
	}

	if got := RepoResourceSourcePlan(nil).ProjectSources(); len(got) != 0 {
		t.Errorf("nil manifest = %v, want no project roots", got)
	}
}

// TestResourceSourcePlan_SurvivesOrderedReplaceOfEffectiveSources is the
// regression this API exists for. `sources` is CategoryOrderedReplace, so a
// repo that declares its own sources REPLACES the inherited array outright —
// Effective.Sources cannot see the org source the team layer declared, even
// though the resolver used it to fetch the org layer. The plan must.
func TestResourceSourcePlan_SurvivesOrderedReplaceOfEffectiveSources(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "team", "type": "git", "url": "https://example/team.git", "ref": "main"}],
		"extends": ["team:team/base.json"]
	}`)
	fake := &fakeFetcher{files: map[string]string{
		"team/base.json": `{"sources":[{"id":"org","type":"git","url":"https://example/org.git","ref":"main"}],"extends":["org:org/base.json"],"skills":["team-skill"]}`,
		"org/base.json":  `{"skills":["org-skill"]}`,
	}}
	snap, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Precondition: the merged view really did discard the inherited source.
	if len(snap.Effective.Sources) != 1 || snap.Effective.Sources[0].ID != "team" {
		t.Fatalf("precondition: effective sources = %+v, want ordered-replace to the repo's [team]", snap.Effective.Sources)
	}

	plan, err := snap.ResourceSourcePlan()
	if err != nil {
		t.Fatalf("ResourceSourcePlan: %v", err)
	}
	ids := planSourceIDs(plan)
	if !reflect.DeepEqual(ids, []string{"team", "org"}) {
		t.Errorf("plan source ids = %v, want [team org] (repo first, then the transitively inherited org root)", ids)
	}
	if e := planEntry(t, plan, "team:team/base.json"); !e.Inherited || e.Source.ID != "org" {
		t.Errorf("org root = %+v, want it attributed to the team layer that declared it", e)
	}
}

// TestResourceSourcePlan_OnlineOfflineParity: the offline replay reconstructs
// the same transitive stack, so an offline/locked install resolves the same
// inherited roots as an online one.
func TestResourceSourcePlan_OnlineOfflineParity(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "team", "type": "git", "url": "https://example/team.git", "ref": "main"}],
		"extends": ["team:team/base.json"]
	}`)
	fake := &fakeFetcher{files: map[string]string{
		"team/base.json": `{"sources":[{"id":"org","type":"git","url":"https://example/org.git","ref":"main"}],"extends":["org:org/base.json"]}`,
		"org/base.json":  `{"skills":["org-skill"]}`,
	}}
	resolver := NewLayeredResolver().WithFetcher("git", fake)
	online, err := resolver.Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	offline, err := resolver.ResolveLocked(repo)
	if err != nil {
		t.Fatalf("ResolveLocked: %v", err)
	}

	onlinePlan, err := online.ResourceSourcePlan()
	if err != nil {
		t.Fatalf("online plan: %v", err)
	}
	offlinePlan, err := offline.ResourceSourcePlan()
	if err != nil {
		t.Fatalf("offline plan: %v", err)
	}
	if !reflect.DeepEqual(onlinePlan, offlinePlan) {
		t.Errorf("offline plan diverged:\nonline  %+v\noffline %+v", onlinePlan, offlinePlan)
	}
}

func planSourceIDs(plan ResourceSourcePlan) []string {
	out := make([]string, 0, len(plan.Sources))
	for _, src := range plan.ProjectSources() {
		out = append(out, src.ID)
	}
	return out
}
