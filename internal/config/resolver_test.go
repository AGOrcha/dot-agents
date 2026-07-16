package config

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-billy/v6"
	"github.com/go-git/go-billy/v6/memfs"
	gogit "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/go-git/go-git/v6/storage/memory"

	"github.com/AGOrcha/dot-agents/internal/agentslock"
)

// FlatResolver must satisfy the Resolver interface.
var _ Resolver = (*FlatResolver)(nil)

// LayeredResolver must satisfy the Resolver interface.
var _ Resolver = (*LayeredResolver)(nil)

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
			name: "reviewer_profiles merges by lens like verifier_profiles",
			key:  "reviewer_profiles",
			prev: map[string]any{"architecture-standards": map[string]any{"label": "Arch", "prompt_files": []any{"base.md"}}},
			next: map[string]any{"architecture-standards": map[string]any{"prompt_files": []any{"base.md", "arch.md"}}, "adversarial": map[string]any{"label": "Adv"}},
			want: map[string]any{"architecture-standards": map[string]any{"label": "Arch", "prompt_files": []any{"base.md", "arch.md"}}, "adversarial": map[string]any{"label": "Adv"}},
		},
		{
			name: "stage_profiles deep-merges per (stage, slug), leaf prompt_files replaced",
			key:  "stage_profiles",
			prev: map[string]any{
				"verifier": map[string]any{"unit": map[string]any{"label": "Unit", "prompt_files": []any{"base.md"}}},
			},
			next: map[string]any{
				"verifier": map[string]any{
					"unit":       map[string]any{"prompt_files": []any{"base.md", "unit.md"}},
					"cli-runner": map[string]any{"label": "CLI"},
				},
				"reviewer": map[string]any{"adversarial": map[string]any{"label": "Adv"}},
			},
			want: map[string]any{
				"verifier": map[string]any{
					"unit":       map[string]any{"label": "Unit", "prompt_files": []any{"base.md", "unit.md"}},
					"cli-runner": map[string]any{"label": "CLI"},
				},
				"reviewer": map[string]any{"adversarial": map[string]any{"label": "Adv"}},
			},
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

// --- LayeredResolver tests --------------------------------------------------

// fakeFetcher is the test seam standing in for the git fetcher: it serves
// canned bytes per layer path with no network or git binary.
type fakeFetcher struct {
	files    map[string]string // layer-path -> JSON body
	sha      string
	fetchErr error
	calls    int
}

func (f *fakeFetcher) Fetch(_ Source, parts LayerRefParts, cacheDir string) (FetchedLayer, error) {
	f.calls++
	if f.fetchErr != nil {
		return FetchedLayer{}, f.fetchErr
	}
	body, ok := f.files[parts.LayerPath]
	if !ok {
		return FetchedLayer{}, errors.New("fake: no such layer " + parts.LayerPath)
	}
	sha := f.sha
	if sha == "" {
		sha = contentHash([]byte(body))
	}
	if err := writeCachedLayer(cacheDir, sha, []byte(body)); err != nil {
		return FetchedLayer{}, err
	}
	return FetchedLayer{Data: []byte(body), ResolvedSHA: sha}, nil
}

// localLayerSourcePath returns the absolute testdata layers dir.
func localLayerSourcePath(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("testdata", "layers"))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func TestLayeredResolverLocalTwoLayerEndToEnd(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"repo_id": "github.com/acme/app",
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`", "cache_ttl": "4h"}],
		"extends": ["acme:org/base.json", "acme:team/frontend.json"],
		"skills": ["repo-skill"]
	}`)

	snap, err := NewLayeredResolver().Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// skills are set-union across org -> team -> repo, in precedence order.
	wantSkills := []string{"org-base-skill", "frontend-skill", "repo-skill"}
	if !reflect.DeepEqual(snap.Effective.Skills, wantSkills) {
		t.Errorf("skills = %v, want %v", snap.Effective.Skills, wantSkills)
	}
	// features map-merges across layers.
	gotFeatures := snap.Effective.Features
	if gotFeatures["tasks"] != "on" || gotFeatures["history"] != "on" {
		t.Errorf("features = %v, want tasks+history on", gotFeatures)
	}
	// kg from org layer survives.
	if snap.Effective.KG == nil || snap.Effective.KG.Backend != "sqlite" {
		t.Errorf("kg backend = %+v, want sqlite", snap.Effective.KG)
	}
	// Layer stack: product-defaults, then the two imports (by ref id), then repo-local.
	ids := layerIDs(snap.Layers)
	want := []string{LayerProductDefaults, "acme:org/base.json", "acme:team/frontend.json", LayerRepoLocal}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("layer ids = %v, want %v", ids, want)
	}

	// Lockfile round-trip: both config + empty packages stub written.
	assertLockfileSections(t, repo, []string{"acme:org/base.json", "acme:team/frontend.json"})
}

func TestLayeredResolverGitMockedViaFakeFetcher(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git", "ref": "main"}],
		"extends": ["acme:org/base.json"]
	}`)

	fake := &fakeFetcher{
		files: map[string]string{"org/base.json": `{"skills":["from-git"]}`},
		sha:   "deadbeefcafe0000000000000000000000000000",
	}
	snap, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if fake.calls != 1 {
		t.Errorf("fetcher called %d times, want 1", fake.calls)
	}
	if got := activeValue(findProvenance(snap, "skills")); !reflect.DeepEqual(got, []any{"from-git"}) {
		t.Errorf("skills = %v, want [from-git]", got)
	}
	// The lockfile records the git-resolved SHA.
	locked, err := readLockedLayersFromUnits(repo)
	if err != nil {
		t.Fatal(err)
	}
	if locked["acme:org/base.json"].ResolvedSHA != fake.sha {
		t.Errorf("locked sha = %q, want %q", locked["acme:org/base.json"].ResolvedSHA, fake.sha)
	}
	if locked["acme:org/base.json"].TTLExpiresAt != "" {
		t.Errorf("git source has no cache_ttl, expected empty TTL, got %q", locked["acme:org/base.json"].TTLExpiresAt)
	}
}

func TestLayeredResolverProtectedFieldDroppedFromImportedLayer(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"repo_id": "github.com/acme/real",
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git"}],
		"extends": ["acme:org/base.json"]
	}`)
	fake := &fakeFetcher{files: map[string]string{
		"org/base.json": `{"repo_id":"github.com/evil/override","skills":["x"]}`,
	}}
	snap, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// The imported repo_id must be dropped; repo-local value wins.
	if snap.Effective.RepoID != "github.com/acme/real" {
		t.Errorf("repo_id = %q, want repo-local value", snap.Effective.RepoID)
	}
	if !hasWarning(snap.Warnings, "repo_id", "dropped") {
		t.Errorf("expected dropped protected-field warning, got %+v", snap.Warnings)
	}
}

// TestLayeredResolverOCILayerResolvesEndToEnd asserts an OCI-sourced config
// layer resolves through extends and merges like any other layer
// (config-distribution-model §15 D13: full source/kind orthogonality). An
// ociLayerFetcher with a fake puller serves the layer blob with the config-layer
// media type; the resolver merges it with no special-casing.
func TestLayeredResolverOCILayerResolvesEndToEnd(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "reg", "type": "oci", "url": "oci://example/reg"}],
		"extends": ["reg:org/base.json"]
	}`)
	body := []byte(`{"skills":["from-oci"]}`)
	digest := "sha256:" + sha256Hex(body)
	fetcher := &ociLayerFetcher{puller: func(_ context.Context, _ ociRef, _ []byte) (ociBlob, error) {
		return ociBlob{Data: body, Digest: digest, MediaType: ociLayerMediaType}, nil
	}}
	snap, err := NewLayeredResolver().WithFetcher("oci", fetcher).Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := activeValue(findProvenance(snap, "skills")); !reflect.DeepEqual(got, []any{"from-oci"}) {
		t.Errorf("skills = %v, want [from-oci]", got)
	}
	// The lockfile records the OCI-resolved digest as the layer's resolved SHA.
	locked, err := readLockedLayersFromUnits(repo)
	if err != nil {
		t.Fatal(err)
	}
	if locked["reg:org/base.json"].ResolvedSHA != digest {
		t.Errorf("locked sha = %q, want %q", locked["reg:org/base.json"].ResolvedSHA, digest)
	}
}

// TestLayeredResolverOCILayerRejectsArtifactMediaType asserts the kind guard
// flows through the resolver: an extends ref whose OCI blob carries the
// artifact-bundle media type fails as a schema error (the layer fetcher's guard).
func TestLayeredResolverOCILayerRejectsArtifactMediaType(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "reg", "type": "oci", "url": "oci://example/reg"}],
		"extends": ["reg:org/base.json"]
	}`)
	body := []byte("artifact-bundle")
	fetcher := &ociLayerFetcher{puller: func(_ context.Context, _ ociRef, _ []byte) (ociBlob, error) {
		return ociBlob{Data: body, Digest: "sha256:" + sha256Hex(body), MediaType: ociArtifactMediaType}, nil
	}}
	_, err := NewLayeredResolver().WithFetcher("oci", fetcher).Resolve(repo)
	var ie *ImportError
	if !errors.As(err, &ie) {
		t.Fatalf("expected *ImportError, got %v", err)
	}
	if ie.Reason != ReasonSchema {
		t.Errorf("reason = %q, want schema", ie.Reason)
	}
}

func TestLayeredResolverUnknownSourceFails(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"extends": ["ghost:org/base.json"]
	}`)
	_, err := NewLayeredResolver().Resolve(repo)
	var ie *ImportError
	if !errors.As(err, &ie) {
		t.Fatalf("expected *ImportError, got %v", err)
	}
	if ie.Reason != ReasonNotFound {
		t.Errorf("reason = %q, want not_found", ie.Reason)
	}
}

func TestLayeredResolverOptionalEntrySkippedOnFailure(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git"}],
		"extends": [{"ref": "acme:experimental.json", "optional": true}],
		"skills": ["repo-skill"]
	}`)
	fake := &fakeFetcher{fetchErr: errors.New("boom")}
	snap, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo)
	if err != nil {
		t.Fatalf("optional failure should not be fatal: %v", err)
	}
	if got := activeValue(findProvenance(snap, "skills")); !reflect.DeepEqual(got, []any{"repo-skill"}) {
		t.Errorf("skills = %v, want repo-only", got)
	}
	if !hasWarningPrefix(snap.Warnings, "acme:experimental.json", "optional_skipped") {
		t.Errorf("expected optional_skipped warning, got %+v", snap.Warnings)
	}
}

func TestLayeredResolverRequiredEntryFailHalts(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git"}],
		"extends": ["acme:required.json"]
	}`)
	fake := &fakeFetcher{fetchErr: errors.New("boom")}
	_, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo)
	var ie *ImportError
	if !errors.As(err, &ie) {
		t.Fatalf("expected *ImportError, got %v", err)
	}
	if ie.Reason != ReasonTransport {
		t.Errorf("reason = %q, want transport", ie.Reason)
	}
}

func TestLayeredResolverOfflineUsesLockSHA(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git", "ref": "main", "cache_ttl": "1h"}],
		"extends": ["acme:org/base.json"]
	}`)
	fake := &fakeFetcher{
		files: map[string]string{"org/base.json": `{"skills":["online"]}`},
		sha:   "feedface000000000000000000000000000000aa",
	}
	// First, online resolve to populate lock + cache.
	if _, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo); err != nil {
		t.Fatalf("online Resolve: %v", err)
	}

	// Now offline: must not call the fetcher, must reuse the cached SHA, and
	// must emit a cache_hit_offline warning.
	offlineFake := &fakeFetcher{fetchErr: errors.New("network down")}
	snap, err := NewLayeredResolver().WithFetcher("git", offlineFake).WithOffline(true).Resolve(repo)
	if err != nil {
		t.Fatalf("offline Resolve: %v", err)
	}
	if offlineFake.calls != 0 {
		t.Errorf("offline mode called fetcher %d times, want 0", offlineFake.calls)
	}
	if got := activeValue(findProvenance(snap, "skills")); !reflect.DeepEqual(got, []any{"online"}) {
		t.Errorf("offline skills = %v, want cached [online]", got)
	}
	if !hasWarning(snap.Warnings, "acme:org/base.json", "cache_hit_offline") {
		t.Errorf("expected cache_hit_offline warning, got %+v", snap.Warnings)
	}
}

func TestLayeredResolverOfflineMissingLockFails(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git"}],
		"extends": ["acme:org/base.json"]
	}`)
	fake := &fakeFetcher{fetchErr: errors.New("should not be called")}
	_, err := NewLayeredResolver().WithFetcher("git", fake).WithOffline(true).Resolve(repo)
	if err == nil {
		t.Fatal("offline with no lock entry should fail")
	}
}

func TestLayeredResolverNoExtendsBehavesLikeFlat(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version": 2, "skills": ["only-repo"]}`)
	snap, err := NewLayeredResolver().Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := activeValue(findProvenance(snap, "skills")); !reflect.DeepEqual(got, []any{"only-repo"}) {
		t.Errorf("skills = %v", got)
	}
	// Even with no extends, the lockfile is written with an empty config section.
	assertLockfileSections(t, repo, nil)
}

// TestLayeredResolverUnitsLockDropsClockTTL pins the §7A clean-cutover decision:
// the units lock is content-hash driven and does NOT persist the clock-based
// cache-TTL (ttl_expires_at). A source with cache_ttl still records its
// fetched_at and content cache_key in the units lock, but no TTL — staleness is
// driven by the inputs_digest/cache_key content axes, not a wall clock.
func TestLayeredResolverUnitsLockDropsClockTTL(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`", "cache_ttl": "4h"}],
		"extends": ["acme:org/base.json"]
	}`)
	fixed := time.Date(2026, 4, 19, 14, 0, 0, 0, time.UTC)
	r := NewLayeredResolver().WithClock(func() time.Time { return fixed })
	if _, err := r.Resolve(repo); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	locked, err := readLockedLayersFromUnits(repo)
	if err != nil {
		t.Fatal(err)
	}
	got := locked["acme:org/base.json"]
	if got.FetchedAt != "2026-04-19T14:00:00Z" {
		t.Errorf("fetched_at = %q", got.FetchedAt)
	}
	if got.TTLExpiresAt != "" {
		t.Errorf("ttl_expires_at = %q, want empty (units lock drops the clock TTL)", got.TTLExpiresAt)
	}
}

func TestLayeredResolverInvalidLayerJSONIsSchemaError(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git"}],
		"extends": ["acme:bad.json"]
	}`)
	fake := &fakeFetcher{files: map[string]string{"acme:bad.json": "{not json", "bad.json": "{not json"}}
	_, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo)
	var ie *ImportError
	if !errors.As(err, &ie) || ie.Reason != ReasonSchema {
		t.Fatalf("expected schema ImportError, got %v", err)
	}
}

func TestWriteConfigLockPreservesSiblingSections(t *testing.T) {
	repo := t.TempDir()
	// A prior writer (e.g. the graph adapter) already wrote an "adapters" section.
	lf, err := agentslock.Open(AgentsLockPath(repo))
	if err != nil {
		t.Fatal(err)
	}
	if err := lf.SetSection("adapters", map[string]any{"kuzu": map[string]any{"activated_at": "x"}}); err != nil {
		t.Fatal(err)
	}
	if err := lf.Flush(); err != nil {
		t.Fatal(err)
	}

	// Config resolver writes its section.
	layers := map[string]LockedLayer{"acme:org/base": {ResolvedSHA: "abc", FetchedAt: "t"}}
	if err := WriteConfigLock(repo, layers); err != nil {
		t.Fatal(err)
	}

	// Reopen: adapters preserved, config present, packages stub present.
	reopened, err := agentslock.Open(AgentsLockPath(repo))
	if err != nil {
		t.Fatal(err)
	}
	var adapters map[string]any
	if ok, _ := reopened.Section("adapters", &adapters); !ok {
		t.Error("adapters section was not preserved")
	}
	var gotLayers map[string]LockedLayer
	if ok, _ := reopened.Section(LockSectionConfig, &gotLayers); !ok || gotLayers["acme:org/base"].ResolvedSHA != "abc" {
		t.Errorf("config section not round-tripped: %+v", gotLayers)
	}
	var pkgs map[string]json.RawMessage
	if ok, _ := reopened.Section(LockSectionPackages, &pkgs); !ok {
		t.Error("empty packages stub not written")
	}
}

func TestWriteConfigLockLeavesExistingPackagesSection(t *testing.T) {
	repo := t.TempDir()
	lf, err := agentslock.Open(AgentsLockPath(repo))
	if err != nil {
		t.Fatal(err)
	}
	if err := lf.SetSection(LockSectionPackages, map[string]any{"reg:skill/x": map[string]any{"digest": "sha256:zz"}}); err != nil {
		t.Fatal(err)
	}
	if err := lf.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfigLock(repo, map[string]LockedLayer{}); err != nil {
		t.Fatal(err)
	}
	reopened, err := agentslock.Open(AgentsLockPath(repo))
	if err != nil {
		t.Fatal(err)
	}
	var pkgs map[string]map[string]any
	reopened.Section(LockSectionPackages, &pkgs)
	if pkgs["reg:skill/x"]["digest"] != "sha256:zz" {
		t.Errorf("existing packages section was clobbered: %+v", pkgs)
	}
}

func TestLayeredResolverBuilderSeams(t *testing.T) {
	r := NewLayeredResolver().
		WithProductDefaults(map[string]any{"skills": []any{"product"}}).
		WithUserLocalPath("/tmp/user.json")
	if r.flat.ProductDefaults["skills"] == nil {
		t.Error("WithProductDefaults did not set product defaults")
	}
	if r.flat.userLocalPath != "/tmp/user.json" {
		t.Error("WithUserLocalPath did not set the path")
	}
}

func TestLayeredResolverProductDefaultsLayer(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"skills":["repo"]}`)
	snap, err := NewLayeredResolver().
		WithProductDefaults(map[string]any{"agents": []any{"claude"}}).
		Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snap.Effective.Agents, []string{"claude"}) {
		t.Errorf("product-defaults agents = %v, want [claude]", snap.Effective.Agents)
	}
}

func TestLayeredResolverUserLocalLayerParticipates(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	userDir := t.TempDir()
	userPath := filepath.Join(userDir, AgentsRCFile)
	if err := os.WriteFile(userPath, []byte(`{"version":2,"rules":["user-rule"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, repo, `{"version":2,"rules":["repo-rule"]}`)
	snap, err := NewLayeredResolver().WithUserLocalPath(userPath).Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snap.Effective.Rules, []string{"user-rule", "repo-rule"}) {
		t.Errorf("rules = %v, want union of user+repo", snap.Effective.Rules)
	}
}

func TestLayeredResolverMissingRepoManifestFatal(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	_, err := NewLayeredResolver().Resolve(t.TempDir())
	if err == nil {
		t.Fatal("expected fatal error for missing repo manifest")
	}
}

func TestLayeredResolverInvalidRepoManifestFatal(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{not json`)
	if _, err := NewLayeredResolver().Resolve(repo); err == nil {
		t.Fatal("expected fatal error for invalid repo manifest")
	}
}

func TestLayeredResolverInvalidUserLocalFatal(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2}`)
	bad := filepath.Join(t.TempDir(), "user.json")
	if err := os.WriteFile(bad, []byte(`{bad`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewLayeredResolver().WithUserLocalPath(bad).Resolve(repo); err == nil {
		t.Fatal("expected fatal error for invalid user-local manifest")
	}
}

func TestImportErrorString(t *testing.T) {
	bare := (&ImportError{Ref: "acme:x", SourceID: "acme", Reason: ReasonSchema}).Error()
	if bare == "" || !strings.Contains(bare, "reason=schema") {
		t.Errorf("bare error = %q", bare)
	}
	wrapped := &ImportError{Ref: "acme:x", Reason: ReasonTransport, Err: errors.New("cause")}
	if !strings.Contains(wrapped.Error(), "cause") {
		t.Errorf("wrapped error = %q", wrapped.Error())
	}
	if !errors.Is(wrapped, wrapped.Err) {
		t.Error("Unwrap did not expose the underlying cause")
	}
}

func TestValidateLayerNilIsError(t *testing.T) {
	if _, _, err := validateLayer("acme:x", nil); err == nil {
		t.Fatal("expected error for nil layer")
	}
}

func TestValidateLayerDropsStructuralFields(t *testing.T) {
	raw := map[string]any{"version": 2.0, "$schema": "x", "skills": []any{"keep"}}
	out, warns, err := validateLayer("acme:x", raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out["version"]; ok {
		t.Error("version should be dropped")
	}
	if _, ok := out["$schema"]; ok {
		t.Error("$schema should be dropped")
	}
	if _, ok := out["skills"]; !ok {
		t.Error("skills should be kept")
	}
	// Structural-only drops are silent (no warning).
	if len(warns) != 0 {
		t.Errorf("structural drops should be silent, got %+v", warns)
	}
}

func TestDecodeLayerBytesNull(t *testing.T) {
	if _, err := decodeLayerBytes("acme:x", []byte("null")); err == nil {
		t.Fatal("expected error for null layer payload")
	}
	if _, err := decodeLayerBytes("acme:x", []byte("{bad")); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestReadAllLimitedOverLimit(t *testing.T) {
	big := make([]byte, maxLayerBytes+10)
	if _, err := readAllLimited(bytes.NewReader(big)); err == nil {
		t.Fatal("expected over-limit error")
	}
}

func TestWriteConfigLockFlushError(t *testing.T) {
	// Flush now MkdirAll's the lock's parent, so a merely-absent project dir is
	// created on demand (fixing the Windows mkdir-lock failure). To still cover
	// the Flush error-return wiring, place the project under a regular FILE so
	// the parent cannot be made a directory — portable across OSes.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(blocker, "dir")
	if err := WriteConfigLock(project, map[string]LockedLayer{}); err == nil {
		t.Fatal("expected error writing lock when the lock parent cannot be created")
	}
}

func TestReadLockedLayersCorrupt(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(AgentsLockPath(repo), []byte(`{"config": "not-an-object"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readLockedLayersFromUnits(repo); err == nil {
		t.Fatal("expected decode error for corrupt config section")
	}
}

func TestProductDefaultsNilFallback(t *testing.T) {
	r := &LayeredResolver{flat: &FlatResolver{}, fetchers: map[string]Fetcher{}}
	if got := r.productDefaults(); got == nil || len(got) != 0 {
		t.Errorf("nil product defaults should yield empty map, got %v", got)
	}
	r.flat.ProductDefaults = map[string]any{"x": 1}
	if got := r.productDefaults(); len(got) != 1 {
		t.Errorf("non-nil product defaults should pass through, got %v", got)
	}
}

func TestAgentsLockPath(t *testing.T) {
	got := AgentsLockPath("/some/repo")
	want := filepath.Join("/some/repo", AgentsLockFile)
	if got != want {
		t.Errorf("AgentsLockPath = %q, want %q", got, want)
	}
}

// TestLayeredResolverSnapshotAPIConsumer sanity-calls the snapshot API the way
// `config explain` (config-v2 p4, not wired here) would consume it.
func TestLayeredResolverSnapshotAPIConsumer(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`"}],
		"extends": ["acme:org/base.json"],
		"skills": ["repo-skill"]
	}`)
	snap, err := NewLayeredResolver().Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// EffectiveRaw round-trips.
	raw, err := snap.EffectiveRaw()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["skills"]; !ok {
		t.Error("EffectiveRaw missing skills")
	}
	// FieldAt resolves a dot-path against the layer stack.
	fp := snap.FieldAt("kg.backend")
	if fp.ActiveLayer != "acme:org/base.json" {
		t.Errorf("kg.backend active layer = %q, want the org import", fp.ActiveLayer)
	}
}

// --- cache-key consumption tests --------------------------------------------

// TestLayeredResolverRefreshForcesRevalidationOffline covers the --refresh force
// escape threaded through WithRefresh -> effectiveCacheKey -> EffectiveCacheKey:
// offline cannot revalidate, so the offline serve fails loudly when the effective
// key is the AlwaysRevalidate sentinel (fetchLayer §7A.4 gate). The control case
// (offline, no --refresh) still serves the cached SHA.
func TestLayeredResolverRefreshForcesRevalidationOffline(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git", "ref": "main", "cache_ttl": "1h"}],
		"extends": ["acme:org/base.json"]
	}`)
	fake := &fakeFetcher{
		files: map[string]string{"org/base.json": `{"skills":["online"]}`},
		sha:   "abc0000000000000000000000000000000000001",
	}
	// Online resolve to populate lock + cache.
	if _, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo); err != nil {
		t.Fatalf("online Resolve: %v", err)
	}

	// Offline WITHOUT --refresh: cache key is the kind default, not stale, so the
	// cached SHA is served.
	offlineFake := &fakeFetcher{fetchErr: errors.New("network down")}
	if _, err := NewLayeredResolver().WithFetcher("git", offlineFake).WithOffline(true).Resolve(repo); err != nil {
		t.Fatalf("offline (no refresh) Resolve: %v", err)
	}

	// Offline WITH --refresh: effective key becomes AlwaysRevalidate, so the
	// offline serve must fail rather than serve possibly-stale content.
	refreshFake := &fakeFetcher{fetchErr: errors.New("network down")}
	_, err := NewLayeredResolver().
		WithFetcher("git", refreshFake).
		WithOffline(true).
		WithRefresh(true).
		Resolve(repo)
	if err == nil {
		t.Fatal("offline + --refresh should fail: revalidation required but offline")
	}
	if refreshFake.calls != 0 {
		t.Errorf("offline must not contact fetcher, got %d calls", refreshFake.calls)
	}
	if !strings.Contains(err.Error(), "revalidation required") {
		t.Errorf("error = %q, want revalidation-required message", err)
	}
}

// TestLayeredResolverAlwaysRevalidateSourceForcesRevalidationOffline covers the
// config-declared always_revalidate force escape (the CacheKeys.AlwaysRevalidate
// half of R6) reaching the same offline gate as --refresh.
func TestLayeredResolverAlwaysRevalidateSourceForcesRevalidationOffline(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{
			"id": "acme",
			"type": "git",
			"url": "https://example/repo.git",
			"ref": "main",
			"cache_ttl": "1h",
			"cache_keys": {"always_revalidate": true}
		}],
		"extends": ["acme:org/base.json"]
	}`)
	fake := &fakeFetcher{
		files: map[string]string{"org/base.json": `{"skills":["online"]}`},
		sha:   "abc0000000000000000000000000000000000002",
	}
	if _, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo); err != nil {
		t.Fatalf("online Resolve: %v", err)
	}

	offlineFake := &fakeFetcher{fetchErr: errors.New("network down")}
	_, err := NewLayeredResolver().WithFetcher("git", offlineFake).WithOffline(true).Resolve(repo)
	if err == nil {
		t.Fatal("offline + always_revalidate source should fail: revalidation required")
	}
	if !strings.Contains(err.Error(), "revalidation required") {
		t.Errorf("error = %q, want revalidation-required message", err)
	}
}

// TestLayeredResolverOfflineCacheMissFails covers the offline serve path where
// the lock records a SHA whose cached bytes are gone (fetchLayer readCachedLayer
// miss): a non-force-escape effective key passes the staleness gate, but the
// content is no longer on disk, so the resolve fails.
func TestLayeredResolverOfflineCacheMissFails(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "acme", "type": "git", "url": "https://example/repo.git", "ref": "main", "cache_ttl": "1h"}],
		"extends": ["acme:org/base.json"]
	}`)
	fake := &fakeFetcher{
		files: map[string]string{"org/base.json": `{"skills":["online"]}`},
		sha:   "abc0000000000000000000000000000000000003",
	}
	if _, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo); err != nil {
		t.Fatalf("online Resolve: %v", err)
	}

	// Evict the cached bytes while keeping the lock entry, so offline finds a
	// recorded SHA but no cache file.
	cacheDir := layerCacheDir("acme", "org/base.json")
	if err := os.RemoveAll(filepath.Join(cacheDir, fake.sha)); err != nil {
		t.Fatal(err)
	}

	offlineFake := &fakeFetcher{fetchErr: errors.New("network down")}
	_, err := NewLayeredResolver().WithFetcher("git", offlineFake).WithOffline(true).Resolve(repo)
	if err == nil {
		t.Fatal("offline with evicted cache should fail")
	}
	if !strings.Contains(err.Error(), "not in cache") {
		t.Errorf("error = %q, want not-in-cache message", err)
	}
}

// TestLayeredResolverGathersDirSelectorFacts covers gatherOverrideFacts' {dir}
// branch: a source whose cache_keys declares a dir selector must have the dir's
// presence folded into the effective key, so the lock records a composite
// override key (not the bare kind default).
func TestLayeredResolverGathersDirSelectorFacts(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	markerDir := filepath.Join(t.TempDir(), "marker")
	if err := os.MkdirAll(markerDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{
			"id": "acme",
			"type": "git",
			"url": "https://example/repo.git",
			"ref": "main",
			"cache_ttl": "1h",
			"cache_keys": {"dir": ["`+jsonPath(markerDir)+`"]}
		}],
		"extends": ["acme:org/base.json"]
	}`)
	fake := &fakeFetcher{
		files: map[string]string{"org/base.json": `{"skills":["online"]}`},
		sha:   "abc0000000000000000000000000000000000004",
	}
	if _, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo); err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	locked, err := readLockedLayersFromUnits(repo)
	if err != nil {
		t.Fatal(err)
	}
	gotKey := locked["acme:org/base.json"].CacheKey
	if gotKey == "" {
		t.Fatal("expected a recorded cache key")
	}
	// With a dir selector present, the key is the composite override, which must
	// differ from the bare git kind default over the same resolved SHA.
	defaultKey := DefaultCacheKey(SourceKindGit, CacheKeyInputs{ResolvedCommit: fake.sha})
	if gotKey == defaultKey {
		t.Errorf("dir selector ignored: cache key %q equals kind default", gotKey)
	}
}

// TestLayeredResolverGathersEnvSelectorFacts covers the {env} branch of
// gatherOverrideFacts and confirms a changed env value yields a different
// recorded cache key (the consumption point that makes a cache_keys override
// stop being a silent no-op).
func TestLayeredResolverGathersEnvSelectorFacts(t *testing.T) {
	manifest := `{
		"version": 2,
		"sources": [{
			"id": "acme",
			"type": "git",
			"url": "https://example/repo.git",
			"ref": "main",
			"cache_ttl": "1h",
			"cache_keys": {"env": ["DA_CACHE_TEST_TOKEN"]}
		}],
		"extends": ["acme:org/base.json"]
	}`
	resolveKey := func(t *testing.T, envVal string) string {
		t.Helper()
		t.Setenv("AGENTS_HOME", t.TempDir())
		t.Setenv("DA_CACHE_TEST_TOKEN", envVal)
		repo := t.TempDir()
		writeManifest(t, repo, manifest)
		fake := &fakeFetcher{
			files: map[string]string{"org/base.json": `{"skills":["online"]}`},
			sha:   "abc0000000000000000000000000000000000005",
		}
		if _, err := NewLayeredResolver().WithFetcher("git", fake).Resolve(repo); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		locked, err := readLockedLayersFromUnits(repo)
		if err != nil {
			t.Fatal(err)
		}
		return locked["acme:org/base.json"].CacheKey
	}
	keyA := resolveKey(t, "tokenA")
	keyB := resolveKey(t, "tokenB")
	if keyA == "" || keyB == "" {
		t.Fatal("expected recorded cache keys for both env values")
	}
	if keyA == keyB {
		t.Errorf("env selector ignored: same key %q for differing env values", keyA)
	}
}

// TestWithResolvedSHA covers withResolvedSHA: an empty SHA returns inputs
// unchanged (early return), and a non-empty SHA backfills only the empty
// kind-primary facts while preserving already-populated ones.
func TestWithResolvedSHA(t *testing.T) {
	// Empty SHA: inputs unchanged, primary facts stay empty.
	got := withResolvedSHA(CacheKeyInputs{}, "")
	if got.ResolvedCommit != "" || got.ContentDigest != "" || got.OCIDigest != "" {
		t.Errorf("empty SHA must not backfill, got %+v", got)
	}

	// Non-empty SHA backfills empties but preserves a populated fact.
	in := CacheKeyInputs{ContentDigest: "precise-etag-hash"}
	got = withResolvedSHA(in, "sha-xyz")
	if got.ResolvedCommit != "sha-xyz" || got.OCIDigest != "sha-xyz" {
		t.Errorf("expected empty primary facts backfilled from SHA, got %+v", got)
	}
	if got.ContentDigest != "precise-etag-hash" {
		t.Errorf("populated fact must be preserved, got %q", got.ContentDigest)
	}
}

// TestCacheKeyStaleForLayer covers the resolver-side staleness consumer: a
// matching recorded key is not stale, a force escape (--refresh) is always stale,
// and a cache_keys override edit that reshapes the key is detected as stale from
// the same recorded SHA facts.
func TestCacheKeyStaleForLayer(t *testing.T) {
	src := Source{ID: "acme", Type: "git"}
	locked := LockedLayer{ResolvedSHA: "sha-abc"}
	locked.CacheKey = NewLayeredResolver().effectiveCacheKey(src, locked.cacheKeyInputs())

	// Same source + recorded key: not stale.
	if NewLayeredResolver().CacheKeyStaleForLayer(src, locked) {
		t.Error("matching recorded key should not be stale")
	}

	// --refresh force escape: AlwaysRevalidate never matches the recorded key.
	refreshed := NewLayeredResolver().WithRefresh(true)
	if !refreshed.CacheKeyStaleForLayer(src, locked) {
		t.Error("--refresh should always report stale")
	}

	// cache_keys override edit (adds an env selector): reshapes the key from the
	// same SHA facts, so the recorded default key no longer matches.
	edited := Source{ID: "acme", Type: "git", CacheKeys: &CacheKeys{Env: []string{"SOME_TOKEN"}}}
	if !NewLayeredResolver().CacheKeyStaleForLayer(edited, locked) {
		t.Error("a cache_keys override edit should be detected as stale")
	}
}

// --- t5: da-agc-shaped multi-resource tree end-to-end (package-artifact-install DOGFOOD) ---
//
// dot-agents' own .agentsrc.json wires a real "da-agc" git source
// (git@github.com:AGOrcha/da-agc.git) declaring three packages[] refs — one
// skill ("release-docs-refresh") and two agents ("platform-dirs-change-analyst",
// "promise-gap-analyst") — laid out exactly as external-agent-sources §3's
// tree content layout: "skill/<name>/" and "agent/<name>/" resource
// directories (confirmed against the live repo's tree). These tests drive the
// REAL gitArtifactFetcher + MaterializeToStore + VerifyStoreContentDigest
// chain (t1/t2/H7) against an in-memory git fixture shaped identically to
// that live tree, so the dogfood wiring is proven mechanically without any
// network dependency in CI (spec DC1/DC2, t5 write_scope).

// gitTreeFixtureClonerMultiRoot is fetcher_test.go's gitTreeFixtureCloner
// generalized to more than one top-level artifact root. FetchArtifact's
// initial Lstat classification needs EACH artifact path's directory present
// in the worktree fs (content underneath is irrelevant — fetchTreeBundle
// reads the committed tree, not the worktree); every existing fixture pulls
// exactly one root, but the da-agc dogfood pulls three DIFFERENT artifact
// paths (one skill + two agents) from the SAME source.
func gitTreeFixtureClonerMultiRoot(t *testing.T, files map[string][]byte, artifactRoots []string) func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
	t.Helper()
	st := memory.NewStorage()
	rootHash := buildCommittedTree(t, st, files, nil)

	sig := object.Signature{Name: "t5-dogfood", Email: "t5-dogfood@example"}
	commit := &object.Commit{Author: sig, Committer: sig, Message: "da-agc mirror fixture", TreeHash: rootHash}
	commitObj := st.NewEncodedObject()
	if err := commit.Encode(commitObj); err != nil {
		t.Fatal(err)
	}
	commitHash, err := st.SetEncodedObject(commitObj)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetReference(plumbing.NewHashReference("refs/heads/main", commitHash)); err != nil {
		t.Fatal(err)
	}
	if err := st.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, "refs/heads/main")); err != nil {
		t.Fatal(err)
	}

	return func(_ context.Context, _, _ string) (*gogit.Repository, billy.Filesystem, error) {
		wfs := memfs.New()
		for _, root := range artifactRoots {
			if err := wfs.MkdirAll(root, 0o755); err != nil {
				return nil, nil, err
			}
		}
		repo, err := gogit.Open(st, wfs)
		if err != nil {
			return nil, nil, err
		}
		return repo, wfs, nil
	}
}

// daAgcMirrorRefs is the exact {artifact-path, bucket, marker-file, body}
// set dot-agents' own .agentsrc.json packages[] declares against the live
// da-agc source (confirmed via `gh api repos/AGOrcha/da-agc/git/trees/main`
// at authoring time: skill/release-docs-refresh/SKILL.md,
// agent/platform-dirs-change-analyst/AGENT.md, agent/promise-gap-analyst/AGENT.md).
var daAgcMirrorRefs = []struct {
	artifactPath string
	bucket       string // resource-plan bucket (packageFamilyBuckets in packages_pass2.go)
	marker       string
	body         string
}{
	{"skill/release-docs-refresh", "skills", "SKILL.md", "# release-docs-refresh\n"},
	{"agent/platform-dirs-change-analyst", "agents", "AGENT.md", "# platform-dirs-change-analyst\n"},
	{"agent/promise-gap-analyst", "agents", "AGENT.md", "# promise-gap-analyst\n"},
}

// daAgcMirrorFixtureCloner builds the in-memory git fixture for daAgcMirrorRefs.
func daAgcMirrorFixtureCloner(t *testing.T) func(context.Context, string, string) (*gogit.Repository, billy.Filesystem, error) {
	t.Helper()
	files := make(map[string][]byte, len(daAgcMirrorRefs))
	roots := make([]string, len(daAgcMirrorRefs))
	for i, ref := range daAgcMirrorRefs {
		files[ref.artifactPath+"/"+ref.marker] = []byte(ref.body)
		roots[i] = ref.artifactPath
	}
	return gitTreeFixtureClonerMultiRoot(t, files, roots)
}

// TestGitArtifactFetcher_DaAgcMirror_MaterializesAllThreeRefsAndVerifies is
// the package-artifact-install t5 DOGFOOD proof at the unit level: a git
// source shaped exactly like the live AGorcha/da-agc tree fetches each of the
// three real dot-agents packages[] refs, materializes each into the H2
// content-addressed store, and the offline H7 primitive
// (VerifyStoreContentDigest) confirms every materialized entry — the same
// check `da config verify` runs — reports present+matching. A second
// materialize of the SAME bundle is the R4 no-op: installed=false, the store
// path and digest are unchanged, and no byte is rewritten.
func TestGitArtifactFetcher_DaAgcMirror_MaterializesAllThreeRefsAndVerifies(t *testing.T) {
	withPackagesCache(t)
	agentsHome := AgentsHome()
	src := Source{Type: "git", ID: "da-agc", URL: "file:///da-agc", Ref: "main"}

	for _, ref := range daAgcMirrorRefs {
		t.Run(ref.artifactPath, func(t *testing.T) {
			f := &gitArtifactFetcher{cloner: daAgcMirrorFixtureCloner(t)}
			fetched, err := f.FetchArtifact(src, PackageRefParts{SourceID: "da-agc", ArtifactPath: ref.artifactPath, VersionSpec: "main"})
			if err != nil {
				t.Fatalf("FetchArtifact(%s): %v", ref.artifactPath, err)
			}
			if fetched.Bundle == nil {
				t.Fatalf("expected a tree Bundle for %s", ref.artifactPath)
			}

			storePath, digest, installed, err := MaterializeToStore(agentsHome, ref.bucket, *fetched.Bundle)
			if err != nil {
				t.Fatalf("MaterializeToStore(%s): %v", ref.artifactPath, err)
			}
			if !installed {
				t.Fatalf("expected the first materialize of %s to install", ref.artifactPath)
			}
			data, err := os.ReadFile(filepath.Join(storePath, ref.marker))
			if err != nil || string(data) != ref.body {
				t.Fatalf("materialized %s content = %q, err=%v, want %q", ref.marker, data, err, ref.body)
			}

			content := BundleContentDigest(*fetched.Bundle)
			present, matches := VerifyStoreContentDigest(agentsHome, ref.bucket, digest, content)
			if !present || !matches {
				t.Fatalf("VerifyStoreContentDigest(%s) = present=%v matches=%v, want true/true", ref.artifactPath, present, matches)
			}

			// R4: a second fetch+materialize of the byte-identical upstream is a
			// no-op — no rewrite, same digest, same store path.
			f2 := &gitArtifactFetcher{cloner: daAgcMirrorFixtureCloner(t)}
			fetched2, err := f2.FetchArtifact(src, PackageRefParts{SourceID: "da-agc", ArtifactPath: ref.artifactPath, VersionSpec: "main"})
			if err != nil {
				t.Fatalf("second FetchArtifact(%s): %v", ref.artifactPath, err)
			}
			storePath2, digest2, installed2, err := MaterializeToStore(agentsHome, ref.bucket, *fetched2.Bundle)
			if err != nil {
				t.Fatalf("second MaterializeToStore(%s): %v", ref.artifactPath, err)
			}
			if installed2 {
				t.Fatalf("expected the second materialize of %s to be a no-op (installed=false)", ref.artifactPath)
			}
			if storePath2 != storePath || digest2 != digest {
				t.Fatalf("second materialize of %s diverged: storePath %q->%q digest %q->%q", ref.artifactPath, storePath, storePath2, digest, digest2)
			}
		})
	}
}

// TestVerifyStoreContentDigest_DaAgcMirror_DetectsCASTamper is the t5
// adversarial requirement ("tamper a projected ref's CAS content and confirm
// verify fails"): after a clean materialize of a real da-agc-shaped ref, a
// direct on-disk edit of the published CAS bytes (simulating a post-install
// tamper, bypassing the read-only bit exactly as a privileged attacker would)
// must flip VerifyStoreContentDigest from matches=true to matches=false —
// present stays true throughout (the entry still exists; its content no
// longer verifies). A SIBLING, untouched ref must be unaffected, proving the
// digest-keyed store isolates one tampered entry from the others.
func TestVerifyStoreContentDigest_DaAgcMirror_DetectsCASTamper(t *testing.T) {
	withPackagesCache(t)
	agentsHome := AgentsHome()
	src := Source{Type: "git", ID: "da-agc", URL: "file:///da-agc", Ref: "main"}

	skillRef := daAgcMirrorRefs[0]
	agentRef := daAgcMirrorRefs[1]

	materialize := func(ref struct {
		artifactPath string
		bucket       string
		marker       string
		body         string
	}) (storePath, digest, content string) {
		f := &gitArtifactFetcher{cloner: daAgcMirrorFixtureCloner(t)}
		fetched, err := f.FetchArtifact(src, PackageRefParts{SourceID: "da-agc", ArtifactPath: ref.artifactPath, VersionSpec: "main"})
		if err != nil {
			t.Fatalf("FetchArtifact(%s): %v", ref.artifactPath, err)
		}
		sp, d, _, err := MaterializeToStore(agentsHome, ref.bucket, *fetched.Bundle)
		if err != nil {
			t.Fatalf("MaterializeToStore(%s): %v", ref.artifactPath, err)
		}
		return sp, d, BundleContentDigest(*fetched.Bundle)
	}

	skillStorePath, skillDigest, skillContent := materialize(skillRef)
	_, agentDigest, agentContent := materialize(agentRef)

	// Clean: both verify.
	if present, matches := VerifyStoreContentDigest(agentsHome, skillRef.bucket, skillDigest, skillContent); !present || !matches {
		t.Fatalf("pre-tamper skill verify = present=%v matches=%v, want true/true", present, matches)
	}
	if present, matches := VerifyStoreContentDigest(agentsHome, agentRef.bucket, agentDigest, agentContent); !present || !matches {
		t.Fatalf("pre-tamper agent verify = present=%v matches=%v, want true/true", present, matches)
	}

	// Tamper ONLY the skill's published CAS file (restore the write bit t3's
	// read-only hardening drops, exactly as a privileged tamperer must).
	markerPath := filepath.Join(skillStorePath, skillRef.marker)
	if err := os.Chmod(markerPath, 0o644); err != nil {
		t.Fatalf("restore write bit: %v", err)
	}
	if err := os.WriteFile(markerPath, []byte("TAMPERED"), 0o644); err != nil {
		t.Fatalf("tamper CAS file: %v", err)
	}

	present, matches := VerifyStoreContentDigest(agentsHome, skillRef.bucket, skillDigest, skillContent)
	if !present {
		t.Fatal("expected the tampered entry to still be present (tamper is a content edit, not a deletion)")
	}
	if matches {
		t.Fatal("expected VerifyStoreContentDigest to fail closed on a tampered CAS file")
	}

	// The sibling agent ref, never touched, must still verify clean.
	if present, matches := VerifyStoreContentDigest(agentsHome, agentRef.bucket, agentDigest, agentContent); !present || !matches {
		t.Fatalf("post-tamper sibling agent verify = present=%v matches=%v, want true/true (tamper must not cross entries)", present, matches)
	}
}

// --- test helpers -----------------------------------------------------------

func layerIDs(layers []ResolvedLayer) []string {
	ids := make([]string, len(layers))
	for i, l := range layers {
		ids[i] = l.ID
	}
	return ids
}

func hasWarning(ws []ProvenanceWarning, field, outcome string) bool {
	for _, w := range ws {
		if w.FieldPath == field && w.Outcome == outcome {
			return true
		}
	}
	return false
}

func hasWarningPrefix(ws []ProvenanceWarning, field, outcomePrefix string) bool {
	for _, w := range ws {
		if w.FieldPath == field && len(w.Outcome) >= len(outcomePrefix) && w.Outcome[:len(outcomePrefix)] == outcomePrefix {
			return true
		}
	}
	return false
}

// jsonPath escapes a filesystem path for safe embedding in a JSON string literal
// (Windows backslashes must be escaped).
func jsonPath(p string) string {
	b, _ := json.Marshal(p)
	// strip the surrounding quotes json.Marshal adds.
	return string(b[1 : len(b)-1])
}

// assertLockfileSections asserts the AUTHORITATIVE §7A lock shape after the
// units-lock cutover (section-7a-units-lock-wiring): the lockfile carries the
// "units" section (with a UnitKindLayer + resolved SHA per wanted ref) and a
// top-level inputs_digest. The legacy "config"/"packages" sections are no longer
// written.
func assertLockfileSections(t *testing.T, repo string, wantLayerRefs []string) {
	t.Helper()
	lf, err := agentslock.Open(AgentsLockPath(repo))
	if err != nil {
		t.Fatal(err)
	}
	var units map[string]LockedUnit
	if ok, _ := lf.Section(LockSectionUnits, &units); !ok {
		t.Fatal("lockfile missing units section")
	}
	for _, ref := range wantLayerRefs {
		u, ok := units[ref]
		if !ok || u.Digest == "" {
			t.Errorf("lockfile units missing resolved digest for %q: %+v", ref, units[ref])
		}
		if u.Kind != UnitKindLayer {
			t.Errorf("unit %q kind = %q, want %q", ref, u.Kind, UnitKindLayer)
		}
	}
	if digest, ok := lf.InputsDigest(); !ok || digest == "" {
		t.Error("lockfile missing inputs_digest")
	}
}
