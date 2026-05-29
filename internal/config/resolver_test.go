package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// FlatResolver must satisfy the Resolver interface.
var _ Resolver = (*FlatResolver)(nil)

func writeManifest(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, AgentsRCFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func findProvenance(snap *Snapshot, field string) FieldProvenance {
	return snap.Provenance[field]
}

// activeValue returns the value of the active layer in a provenance stack, or
// nil when no layer is active.
func activeValue(fp FieldProvenance) any {
	for _, lv := range fp.Layers {
		if lv.Active {
			return lv.Value
		}
	}
	return nil
}

func TestMergeFieldCategories(t *testing.T) {
	cases := []struct {
		name string
		key  string
		prev any
		next any
		want any
	}{
		{
			name: "scalar last writer wins",
			key:  "version",
			prev: float64(1),
			next: float64(2),
			want: float64(2),
		},
		{
			name: "set union dedups and preserves order",
			key:  "skills",
			prev: []any{"a", "b"},
			next: []any{"b", "c"},
			want: []any{"a", "b", "c"},
		},
		{
			name: "set union with nil prev",
			key:  "agents",
			prev: nil,
			next: []any{"x", "x"},
			want: []any{"x"},
		},
		{
			name: "map merge by key",
			key:  "features",
			prev: map[string]any{"a": "enabled", "b": "disabled"},
			next: map[string]any{"b": "preview", "c": "enabled"},
			want: map[string]any{"a": "enabled", "b": "preview", "c": "enabled"},
		},
		{
			name: "map merge recurses nested objects",
			key:  "verifier_profiles",
			prev: map[string]any{"unit": map[string]any{"label": "Unit", "kind": "go"}},
			next: map[string]any{"unit": map[string]any{"kind": "go-race"}},
			want: map[string]any{"unit": map[string]any{"label": "Unit", "kind": "go-race"}},
		},
		{
			name: "app_type_verifier_map merges by app type, replaces sequence",
			key:  "app_type_verifier_map",
			prev: map[string]any{"go-cli": []any{"unit"}, "api": []any{"unit"}},
			next: map[string]any{"api": []any{"unit", "api", "integration"}},
			want: map[string]any{"go-cli": []any{"unit"}, "api": []any{"unit", "api", "integration"}},
		},
		{
			name: "ordered replace sources",
			key:  "sources",
			prev: []any{map[string]any{"type": "local"}},
			next: []any{map[string]any{"type": "git", "url": "u"}},
			want: []any{map[string]any{"type": "git", "url": "u"}},
		},
		{
			name: "uncategorized field defaults to scalar replace",
			key:  "some_unknown_field",
			prev: "old",
			next: "new",
			want: "new",
		},
		{
			name: "set union falls back to replace when next is not array",
			key:  "skills",
			prev: []any{"a"},
			next: "scalar",
			want: "scalar",
		},
		{
			name: "map merge falls back to replace when next is not object",
			key:  "features",
			prev: map[string]any{"a": "x"},
			next: "scalar",
			want: "scalar",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeField(tc.key, tc.prev, tc.next)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("mergeField(%q) = %#v, want %#v", tc.key, got, tc.want)
			}
		})
	}
}

func TestUnionSlicesNonArrayPrev(t *testing.T) {
	// prev not an array, next is — replace per the !prevOK branch.
	got := unionSlices("scalar", []any{"a"})
	if !reflect.DeepEqual(got, []any{"a"}) {
		t.Errorf("got %#v", got)
	}
}

func TestUnionSlicesDedupNonStringElements(t *testing.T) {
	got := unionSlices([]any{float64(1), float64(2)}, []any{float64(2), float64(3)})
	want := []any{float64(1), float64(2), float64(3)}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
}

func TestMergeMapsNonObjectPrev(t *testing.T) {
	got := mergeMaps("scalar", map[string]any{"a": "x"})
	if !reflect.DeepEqual(got, map[string]any{"a": "x"}) {
		t.Errorf("got %#v", got)
	}
}

func TestResolveSnapshotSetUnionAcrossLayers(t *testing.T) {
	layers := []ResolvedLayer{
		newLayer(LayerProductDefaults, map[string]any{"skills": []any{"base-skill"}}),
		newLayer(LayerUserLocal, map[string]any{"skills": []any{"user-skill", "base-skill"}}),
		newLayer(LayerRepoLocal, map[string]any{"skills": []any{"repo-skill"}}),
	}
	snap, err := resolveSnapshot(layers)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"base-skill", "user-skill", "repo-skill"}
	if !reflect.DeepEqual(snap.Effective.Skills, want) {
		t.Errorf("Skills = %v, want %v", snap.Effective.Skills, want)
	}
	// All three layers contributed -> active is the highest precedence writer.
	fp := findProvenance(snap, "skills")
	if fp.ActiveLayer != LayerRepoLocal {
		t.Errorf("skills active layer = %q, want %q", fp.ActiveLayer, LayerRepoLocal)
	}
}

func TestResolveSnapshotProtectedFieldDroppedFromImportedLayer(t *testing.T) {
	// repo_id set by a lower-precedence layer is dropped with a warning; the
	// repo-local value (if any) is the only eligible winner.
	layers := []ResolvedLayer{
		newLayer(LayerProductDefaults, map[string]any{"repo_id": "github.com/evil/inject"}),
		newLayer(LayerUserLocal, map[string]any{"project": "user-attempt"}),
		newLayer(LayerRepoLocal, map[string]any{"repo_id": "github.com/acme/real", "project": "real"}),
	}
	snap, err := resolveSnapshot(layers)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Effective.RepoID != "github.com/acme/real" {
		t.Errorf("RepoID = %q, want repo-local value", snap.Effective.RepoID)
	}
	if snap.Effective.Project != "real" {
		t.Errorf("Project = %q, want repo-local value", snap.Effective.Project)
	}
	if len(snap.Warnings) != 2 {
		t.Fatalf("want 2 protection-violation warnings, got %d: %+v", len(snap.Warnings), snap.Warnings)
	}
	for _, w := range snap.Warnings {
		if w.Outcome != "dropped" {
			t.Errorf("warning outcome = %q, want dropped", w.Outcome)
		}
		if w.AttemptedByLayer == LayerRepoLocal {
			t.Errorf("repo-local must not produce a protection warning")
		}
	}
	// Provenance for repo_id credits only the repo-local layer as active.
	fp := findProvenance(snap, "repo_id")
	if fp.ActiveLayer != LayerRepoLocal {
		t.Errorf("repo_id active layer = %q, want %q", fp.ActiveLayer, LayerRepoLocal)
	}
}

func TestResolveSnapshotProtectedFieldUnsetByRepo(t *testing.T) {
	// Protected field attempted only by a lower layer, repo-local silent: it is
	// dropped and resolves to unset (no active layer).
	layers := []ResolvedLayer{
		newLayer(LayerUserLocal, map[string]any{"repo_id": "github.com/x/y"}),
		newLayer(LayerRepoLocal, map[string]any{"version": float64(2)}),
	}
	snap, err := resolveSnapshot(layers)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Effective.RepoID != "" {
		t.Errorf("RepoID = %q, want empty (dropped)", snap.Effective.RepoID)
	}
	fp := findProvenance(snap, "repo_id")
	if fp.ActiveLayer != "" {
		t.Errorf("repo_id active layer = %q, want empty", fp.ActiveLayer)
	}
	if len(snap.Warnings) != 1 {
		t.Errorf("want 1 warning, got %d", len(snap.Warnings))
	}
}

func TestResolveSnapshotWarningsAlwaysNonNil(t *testing.T) {
	snap, err := resolveSnapshot([]ResolvedLayer{
		newLayer(LayerRepoLocal, map[string]any{"version": float64(2)}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Warnings == nil {
		t.Error("Warnings must be non-nil ([]ProvenanceWarning{})")
	}
}

func TestFlatResolverEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	writeManifest(t, home, `{"version":2,"skills":["user-skill"],"features":{"graph_bridge":"preview"}}`)

	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version":2,
		"project":"demo",
		"repo_id":"github.com/acme/demo",
		"skills":["repo-skill"],
		"features":{"staged_fanout":"enabled"},
		"app_type_verifier_map":{"go-cli":["unit"]}
	}`)

	r := &FlatResolver{ProductDefaults: map[string]any{"version": float64(2)}}
	snap, err := r.Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}

	if got := []string{"user-skill", "repo-skill"}; !reflect.DeepEqual(snap.Effective.Skills, got) {
		t.Errorf("Skills = %v, want %v", snap.Effective.Skills, got)
	}
	if snap.Effective.RepoID != "github.com/acme/demo" {
		t.Errorf("RepoID = %q", snap.Effective.RepoID)
	}
	wantFeatures := map[string]string{"graph_bridge": "preview", "staged_fanout": "enabled"}
	if !reflect.DeepEqual(snap.Effective.Features, wantFeatures) {
		t.Errorf("Features = %v, want %v", snap.Effective.Features, wantFeatures)
	}

	// Three layers in precedence order.
	if len(snap.Layers) != 3 {
		t.Fatalf("want 3 layers, got %d", len(snap.Layers))
	}
	ids := []string{snap.Layers[0].ID, snap.Layers[1].ID, snap.Layers[2].ID}
	wantIDs := []string{LayerProductDefaults, LayerUserLocal, LayerRepoLocal}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Errorf("layer order = %v, want %v", ids, wantIDs)
	}

	// Provenance: features.graph_bridge active in user-local; staged_fanout in repo-local.
	gb := snap.FieldAt("features.graph_bridge")
	if gb.ActiveLayer != LayerUserLocal {
		t.Errorf("features.graph_bridge active = %q, want %q", gb.ActiveLayer, LayerUserLocal)
	}
	sf := snap.FieldAt("features.staged_fanout")
	if sf.ActiveLayer != LayerRepoLocal {
		t.Errorf("features.staged_fanout active = %q, want %q", sf.ActiveLayer, LayerRepoLocal)
	}
	avm := snap.FieldAt("app_type_verifier_map.go-cli")
	if avm.ActiveLayer != LayerRepoLocal {
		t.Errorf("app_type_verifier_map.go-cli active = %q, want %q", avm.ActiveLayer, LayerRepoLocal)
	}
	if v := activeValue(avm); !reflect.DeepEqual(v, []any{"unit"}) {
		t.Errorf("app_type_verifier_map.go-cli active value = %#v, want [unit]", v)
	}
}

func TestFlatResolverNoUserLocal(t *testing.T) {
	home := t.TempDir() // empty, no manifest
	t.Setenv("AGENTS_HOME", home)
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"project":"solo"}`)

	snap, err := NewFlatResolver().Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}
	// user-local layer present in stack but absent (Raw nil).
	var ul ResolvedLayer
	for _, l := range snap.Layers {
		if l.ID == LayerUserLocal {
			ul = l
		}
	}
	if ul.Present || ul.Raw != nil {
		t.Errorf("user-local should be absent: %+v", ul)
	}
	if snap.Effective.Project != "solo" {
		t.Errorf("Project = %q", snap.Effective.Project)
	}
}

func TestFlatResolverMissingRepoManifestFatal(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir() // no .agentsrc.json
	if _, err := NewFlatResolver().Resolve(repo); err == nil {
		t.Fatal("expected fatal error on missing repo-local manifest")
	}
}

func TestFlatResolverInvalidRepoManifestFatal(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{not json`)
	if _, err := NewFlatResolver().Resolve(repo); err == nil {
		t.Fatal("expected fatal error on unparseable repo-local manifest")
	}
}

func TestFlatResolverInvalidUserLocalFatal(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2}`)
	bad := filepath.Join(t.TempDir(), "user.json")
	if err := os.WriteFile(bad, []byte(`{bad`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewFlatResolver().WithUserLocalPath(bad)
	if _, err := r.Resolve(repo); err == nil {
		t.Fatal("expected fatal error on unparseable user-local manifest")
	}
}

func TestFlatResolverWithUserLocalPathSeam(t *testing.T) {
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"skills":["repo"]}`)
	userDir := t.TempDir()
	userPath := filepath.Join(userDir, "custom-agentsrc.json")
	if err := os.WriteFile(userPath, []byte(`{"version":2,"skills":["user"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewFlatResolver().WithUserLocalPath(userPath)
	snap, err := r.Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"user", "repo"}
	if !reflect.DeepEqual(snap.Effective.Skills, want) {
		t.Errorf("Skills = %v, want %v", snap.Effective.Skills, want)
	}
}

func TestNewFlatResolverDefaults(t *testing.T) {
	r := NewFlatResolver()
	if r.ProductDefaults == nil {
		t.Error("ProductDefaults should default to empty map, not nil")
	}
}

func TestDecodeObjectFileAbsent(t *testing.T) {
	raw, ok, err := decodeObjectFile(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil || ok || raw != nil {
		t.Errorf("absent file = (%v,%v,%v), want (nil,false,nil)", raw, ok, err)
	}
}

func TestLoadLayersNilProductDefaults(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2}`)
	r := &FlatResolver{} // ProductDefaults nil
	snap, err := r.Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Layers[0].ID != LayerProductDefaults || !snap.Layers[0].Present {
		t.Errorf("product-defaults layer should always be present: %+v", snap.Layers[0])
	}
}
