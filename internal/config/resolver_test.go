package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

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
	locked, err := readLockedLayers(repo)
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

func TestLayeredResolverTierConstraintOCIFails(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version": 2,
		"sources": [{"id": "reg", "type": "oci", "url": "oci://example/reg"}],
		"extends": ["reg:org/base.json"]
	}`)
	_, err := NewLayeredResolver().Resolve(repo)
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

func TestLayeredResolverTTLExpiryRecorded(t *testing.T) {
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
	locked, err := readLockedLayers(repo)
	if err != nil {
		t.Fatal(err)
	}
	got := locked["acme:org/base.json"]
	if got.FetchedAt != "2026-04-19T14:00:00Z" {
		t.Errorf("fetched_at = %q", got.FetchedAt)
	}
	if got.TTLExpiresAt != "2026-04-19T18:00:00Z" {
		t.Errorf("ttl_expires_at = %q, want +4h", got.TTLExpiresAt)
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
	// A non-existent project dir means Flush cannot write the lockfile.
	missing := filepath.Join(t.TempDir(), "no", "such", "dir")
	if err := WriteConfigLock(missing, map[string]LockedLayer{}); err == nil {
		t.Fatal("expected error writing lock into a non-existent dir")
	}
}

func TestReadLockedLayersCorrupt(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(AgentsLockPath(repo), []byte(`{"config": "not-an-object"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readLockedLayers(repo); err == nil {
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

func assertLockfileSections(t *testing.T, repo string, wantLayerRefs []string) {
	t.Helper()
	lf, err := agentslock.Open(AgentsLockPath(repo))
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]LockedLayer
	if ok, _ := lf.Section(LockSectionConfig, &cfg); !ok {
		t.Fatal("lockfile missing config section")
	}
	for _, ref := range wantLayerRefs {
		if cfg[ref].ResolvedSHA == "" {
			t.Errorf("lockfile config missing resolved_sha for %q", ref)
		}
	}
	var pkgs map[string]json.RawMessage
	if ok, _ := lf.Section(LockSectionPackages, &pkgs); !ok {
		t.Error("lockfile missing empty packages stub section")
	}
}
