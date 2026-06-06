package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeOverlay writes a project-local overlay manifest (.agentsrc.local.json)
// into dir. It mirrors writeManifest (resolver_test.go) but targets the
// gitignored overlay file rather than the committed manifest.
func writeOverlay(t *testing.T, dir, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, AgentsRCLocalFile), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestProjectLocalOverlayPath(t *testing.T) {
	got := projectLocalOverlayPath("/some/repo")
	want := filepath.Join("/some/repo", AgentsRCLocalFile)
	if got != want {
		t.Errorf("projectLocalOverlayPath = %q, want %q", got, want)
	}
}

func TestLoadProjectLocalOverlayLayerAbsent(t *testing.T) {
	// No overlay file: the overlay is a conditional scope, so it reports ok=false
	// and contributes no layer — projects without one resolve unchanged.
	layer, ok, err := loadProjectLocalOverlayLayer(t.TempDir())
	if err != nil {
		t.Fatalf("absent overlay should not error: %v", err)
	}
	if ok {
		t.Error("absent overlay should report ok=false")
	}
	if layer.ID != "" || layer.Present || layer.Raw != nil {
		t.Errorf("absent overlay should yield the zero layer, got %+v", layer)
	}
}

func TestLoadProjectLocalOverlayLayerPresent(t *testing.T) {
	repo := t.TempDir()
	writeOverlay(t, repo, `{"version":2,"skills":["overlay-skill"]}`)
	layer, ok, err := loadProjectLocalOverlayLayer(repo)
	if err != nil {
		t.Fatalf("present overlay: %v", err)
	}
	if !ok {
		t.Fatal("present overlay should report ok=true")
	}
	if layer.ID != LayerProjectLocal {
		t.Errorf("layer id = %q, want %q", layer.ID, LayerProjectLocal)
	}
	if !layer.Present {
		t.Error("present overlay should report Present=true")
	}
	if got, ok := layer.Raw["skills"]; !ok || !reflect.DeepEqual(got, []any{"overlay-skill"}) {
		t.Errorf("overlay raw skills = %#v, want [overlay-skill]", got)
	}
}

func TestLoadProjectLocalOverlayLayerInvalidFatal(t *testing.T) {
	repo := t.TempDir()
	writeOverlay(t, repo, `{bad json`)
	if _, _, err := loadProjectLocalOverlayLayer(repo); err == nil {
		t.Fatal("an existing-but-unparseable overlay must be fatal")
	}
}

// --- FlatResolver overlay integration --------------------------------------

func TestFlatResolverOverlayMergesAboveRepoLocal(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version":2,
		"project":"demo",
		"skills":["repo-skill"],
		"features":{"a":"repo","b":"repo"}
	}`)
	// Overlay sets b -> overlay (last writer among local scopes wins) and adds a
	// skill (set-union appends overlay entries after repo entries).
	writeOverlay(t, repo, `{
		"version":2,
		"skills":["overlay-skill"],
		"features":{"b":"overlay","c":"overlay"}
	}`)

	snap, err := NewFlatResolver().Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}

	// Layer stack now carries 4 layers: product, user(absent), repo, overlay.
	ids := layerIDs(snap.Layers)
	want := []string{LayerProductDefaults, LayerUserLocal, LayerRepoLocal, LayerProjectLocal}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("layer order = %v, want %v", ids, want)
	}

	// Set-union: repo entries first, then overlay entries.
	if got := []string{"repo-skill", "overlay-skill"}; !reflect.DeepEqual(snap.Effective.Skills, got) {
		t.Errorf("skills = %v, want %v", snap.Effective.Skills, got)
	}
	// Map-merge with overlay as the highest-precedence local writer.
	wantFeatures := map[string]string{"a": "repo", "b": "overlay", "c": "overlay"}
	if !reflect.DeepEqual(snap.Effective.Features, wantFeatures) {
		t.Errorf("features = %v, want %v", snap.Effective.Features, wantFeatures)
	}
	// Provenance: features.b active in the overlay; features.a still repo-local.
	if got := snap.FieldAt("features.b").ActiveLayer; got != LayerProjectLocal {
		t.Errorf("features.b active = %q, want %q", got, LayerProjectLocal)
	}
	if got := snap.FieldAt("features.a").ActiveLayer; got != LayerRepoLocal {
		t.Errorf("features.a active = %q, want %q", got, LayerRepoLocal)
	}
}

func TestFlatResolverNoOverlayOmittedFromStack(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"project":"solo"}`)

	snap, err := NewFlatResolver().Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}
	// The overlay is a conditional scope: with no .agentsrc.local.json present it
	// must not appear in the layer stack at all, so a project without one is
	// resolved exactly as before this scope existed.
	for _, l := range snap.Layers {
		if l.ID == LayerProjectLocal {
			t.Fatalf("project-local overlay must be omitted when absent, found %+v", l)
		}
	}
	wantIDs := []string{LayerProductDefaults, LayerUserLocal, LayerRepoLocal}
	if got := layerIDs(snap.Layers); !reflect.DeepEqual(got, wantIDs) {
		t.Errorf("layer ids = %v, want %v (no overlay slot)", got, wantIDs)
	}
}

func TestFlatResolverOverlayProtectedFieldDropped(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"repo_id":"github.com/acme/real","project":"real"}`)
	// Overlay attempts to override protected fields: must be dropped (it is not
	// the repo-local layer) with a non-fatal warning.
	writeOverlay(t, repo, `{"version":2,"repo_id":"github.com/evil/override","project":"hijack"}`)

	snap, err := NewFlatResolver().Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Effective.RepoID != "github.com/acme/real" {
		t.Errorf("repo_id = %q, want repo-local value (overlay must not override)", snap.Effective.RepoID)
	}
	if snap.Effective.Project != "real" {
		t.Errorf("project = %q, want repo-local value (overlay must not override)", snap.Effective.Project)
	}
	if !hasWarning(snap.Warnings, "repo_id", "dropped") || !hasWarning(snap.Warnings, "project", "dropped") {
		t.Errorf("expected dropped warnings for both protected fields, got %+v", snap.Warnings)
	}
	for _, w := range snap.Warnings {
		if w.AttemptedByLayer != LayerProjectLocal {
			t.Errorf("protected-field violation should be attributed to the overlay, got %q", w.AttemptedByLayer)
		}
	}
	// repo_id provenance still credits only the repo-local layer.
	if got := findProvenance(snap, "repo_id").ActiveLayer; got != LayerRepoLocal {
		t.Errorf("repo_id active layer = %q, want %q", got, LayerRepoLocal)
	}
}

func TestFlatResolverInvalidOverlayFatal(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2}`)
	writeOverlay(t, repo, `{not json`)
	if _, err := NewFlatResolver().Resolve(repo); err == nil {
		t.Fatal("expected fatal error on unparseable project-local overlay")
	}
}

func TestFlatResolverOverlayWinsOverUserLocal(t *testing.T) {
	// The overlay is the highest-precedence local scope, so for a scalar field it
	// beats both user-local and repo-local.
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"repo_id":"github.com/acme/demo"}`)
	writeOverlay(t, repo, `{"version":2,"features":{"flag":"from-overlay"}}`)
	userDir := t.TempDir()
	userPath := filepath.Join(userDir, AgentsRCFile)
	if err := os.WriteFile(userPath, []byte(`{"version":2,"features":{"flag":"from-user"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := NewFlatResolver().WithUserLocalPath(userPath).Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Effective.Features["flag"] != "from-overlay" {
		t.Errorf("features.flag = %q, want from-overlay (highest local precedence)", snap.Effective.Features["flag"])
	}
	if got := snap.FieldAt("features.flag").ActiveLayer; got != LayerProjectLocal {
		t.Errorf("features.flag active = %q, want %q", got, LayerProjectLocal)
	}
}

// --- LayeredResolver overlay integration -----------------------------------

func TestLayeredResolverOverlayMergesAboveImportsAndRepo(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	src := localLayerSourcePath(t)
	writeManifest(t, repo, `{
		"version": 2,
		"repo_id": "github.com/acme/app",
		"sources": [{"id": "acme", "type": "local", "path": "`+jsonPath(src)+`"}],
		"extends": ["acme:org/base.json"],
		"skills": ["repo-skill"]
	}`)
	writeOverlay(t, repo, `{"version":2,"skills":["overlay-skill"]}`)

	snap, err := NewLayeredResolver().Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Stack order: product, import, repo, overlay (overlay last = highest local).
	ids := layerIDs(snap.Layers)
	want := []string{LayerProductDefaults, "acme:org/base.json", LayerRepoLocal, LayerProjectLocal}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("layer ids = %v, want %v", ids, want)
	}
	// Set-union across import -> repo -> overlay in precedence order.
	wantSkills := []string{"org-base-skill", "repo-skill", "overlay-skill"}
	if !reflect.DeepEqual(snap.Effective.Skills, wantSkills) {
		t.Errorf("skills = %v, want %v", snap.Effective.Skills, wantSkills)
	}
}

func TestLayeredResolverNoExtendsStillMergesOverlay(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"skills":["repo-skill"]}`)
	writeOverlay(t, repo, `{"version":2,"skills":["overlay-skill"]}`)

	snap, err := NewLayeredResolver().Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	wantSkills := []string{"repo-skill", "overlay-skill"}
	if !reflect.DeepEqual(snap.Effective.Skills, wantSkills) {
		t.Errorf("skills = %v, want %v", snap.Effective.Skills, wantSkills)
	}
	if got := layerIDs(snap.Layers); got[len(got)-1] != LayerProjectLocal {
		t.Errorf("last layer = %q, want %q", got[len(got)-1], LayerProjectLocal)
	}
}

func TestLayeredResolverOverlayProtectedFieldDropped(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"repo_id":"github.com/acme/real"}`)
	writeOverlay(t, repo, `{"version":2,"repo_id":"github.com/evil/override"}`)

	snap, err := NewLayeredResolver().Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if snap.Effective.RepoID != "github.com/acme/real" {
		t.Errorf("repo_id = %q, want repo-local value", snap.Effective.RepoID)
	}
	if !hasWarning(snap.Warnings, "repo_id", "dropped") {
		t.Errorf("expected dropped protected-field warning from overlay, got %+v", snap.Warnings)
	}
}

func TestLayeredResolverInvalidOverlayFatal(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"skills":["repo"]}`)
	writeOverlay(t, repo, `{bad`)
	if _, err := NewLayeredResolver().Resolve(repo); err == nil {
		t.Fatal("expected fatal error on unparseable overlay in the layered resolver")
	}
}
