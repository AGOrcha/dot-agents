package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeOverlay writes a project-local overlay (.agentsrc.local.json) into dir.
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
	repo := "/tmp/project"
	got := projectLocalOverlayPath(repo)
	want := filepath.Join(repo, AgentsRCLocalFile)
	if got != want {
		t.Errorf("projectLocalOverlayPath = %q, want %q", got, want)
	}
	if filepath.Base(got) != ".agentsrc.local.json" {
		t.Errorf("overlay file = %q, want .agentsrc.local.json", filepath.Base(got))
	}
}

func TestLoadProjectLocalOverlayPresent(t *testing.T) {
	repo := t.TempDir()
	writeOverlay(t, repo, `{"version":2,"skills":["local-only"]}`)

	layer, ok, err := loadProjectLocalOverlay(repo)
	if err != nil {
		t.Fatalf("loadProjectLocalOverlay: %v", err)
	}
	if !ok {
		t.Fatal("overlay present but ok=false")
	}
	if layer.ID != LayerProjectLocal {
		t.Errorf("layer.ID = %q, want %q", layer.ID, LayerProjectLocal)
	}
	if !layer.Present {
		t.Error("layer.Present = false, want true")
	}
	if got, _ := layer.Raw["skills"].([]any); len(got) != 1 || got[0] != "local-only" {
		t.Errorf("overlay skills = %v, want [local-only]", layer.Raw["skills"])
	}
}

func TestLoadProjectLocalOverlayAbsent(t *testing.T) {
	repo := t.TempDir() // no overlay file
	layer, ok, err := loadProjectLocalOverlay(repo)
	if err != nil {
		t.Fatalf("loadProjectLocalOverlay: %v", err)
	}
	if ok {
		t.Fatal("absent overlay reported ok=true")
	}
	if layer.Present || layer.Raw != nil {
		t.Errorf("absent overlay returned non-zero layer: %+v", layer)
	}
}

func TestLoadProjectLocalOverlayMalformed(t *testing.T) {
	repo := t.TempDir()
	writeOverlay(t, repo, `{not valid json`)
	if _, ok, err := loadProjectLocalOverlay(repo); err == nil {
		t.Fatalf("expected error for malformed overlay (ok=%v)", ok)
	}
}

// TestFlatResolverProjectLocalOverlayMergesAboveRepo proves the overlay is an
// 8th scope merging ABOVE repo-local committed: a scalar set in both layers
// resolves to the overlay's value, and set-union arrays append the overlay last.
func TestFlatResolverProjectLocalOverlayMergesAboveRepo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	writeManifest(t, home, `{"version":2,"skills":["user-skill"]}`)

	repo := t.TempDir()
	writeManifest(t, repo, `{
		"version":2,
		"project":"demo",
		"skills":["repo-skill"],
		"features":{"graph_bridge":"preview","staged_fanout":"committed"}
	}`)
	writeOverlay(t, repo, `{
		"version":2,
		"skills":["local-skill"],
		"features":{"staged_fanout":"local-override"}
	}`)

	snap, err := NewFlatResolver().Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}

	// Layer stack now carries the overlay on top of repo-local.
	ids := layerIDs(snap.Layers)
	wantIDs := []string{LayerProductDefaults, LayerUserLocal, LayerRepoLocal, LayerProjectLocal}
	if !reflect.DeepEqual(ids, wantIDs) {
		t.Errorf("layer order = %v, want %v", ids, wantIDs)
	}

	// Set-union: user -> repo -> overlay, in precedence order.
	wantSkills := []string{"user-skill", "repo-skill", "local-skill"}
	if !reflect.DeepEqual(snap.Effective.Skills, wantSkills) {
		t.Errorf("skills = %v, want %v", snap.Effective.Skills, wantSkills)
	}

	// Map-merge scalar: the overlay wins over repo-local for the same key.
	if got := snap.Effective.Features["staged_fanout"]; got != "local-override" {
		t.Errorf("features.staged_fanout = %q, want local-override (overlay wins)", got)
	}
	// Untouched repo-local key survives the merge.
	if got := snap.Effective.Features["graph_bridge"]; got != "preview" {
		t.Errorf("features.graph_bridge = %q, want preview (repo-local preserved)", got)
	}

	// Provenance: the overlay is the active layer for the field it overrides.
	sf := snap.FieldAt("features.staged_fanout")
	if sf.ActiveLayer != LayerProjectLocal {
		t.Errorf("features.staged_fanout active = %q, want %q", sf.ActiveLayer, LayerProjectLocal)
	}
}

// TestFlatResolverNoOverlayUnchanged proves an absent overlay leaves the stack
// at the three FLAT layers — the overlay is purely additive.
func TestFlatResolverNoOverlayUnchanged(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"project":"solo"}`)

	snap, err := NewFlatResolver().Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range snap.Layers {
		if l.ID == LayerProjectLocal {
			t.Fatalf("unexpected project-local layer in stack with no overlay: %+v", snap.Layers)
		}
	}
	if len(snap.Layers) != 3 {
		t.Errorf("want 3 layers with no overlay, got %d", len(snap.Layers))
	}
}

// TestFlatResolverOverlayProtectedFieldDropped proves the overlay honors
// protected-field rules: repo_id/project set by the overlay are dropped (only
// LayerRepoLocal may own them) and recorded as a non-fatal warning.
func TestFlatResolverOverlayProtectedFieldDropped(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"project":"demo","repo_id":"github.com/acme/demo"}`)
	writeOverlay(t, repo, `{"version":2,"repo_id":"github.com/evil/hijack","project":"hijacked"}`)

	snap, err := NewFlatResolver().Resolve(repo)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Effective.RepoID != "github.com/acme/demo" {
		t.Errorf("RepoID = %q, want committed value (overlay drop)", snap.Effective.RepoID)
	}
	if snap.Effective.Project != "demo" {
		t.Errorf("Project = %q, want committed value (overlay drop)", snap.Effective.Project)
	}

	// The drop is recorded as a non-fatal warning attributed to the overlay layer.
	var droppedRepoID, droppedProject bool
	for _, w := range snap.Warnings {
		if w.AttemptedByLayer != LayerProjectLocal || w.Outcome != "dropped" {
			continue
		}
		switch w.FieldPath {
		case "repo_id":
			droppedRepoID = true
		case "project":
			droppedProject = true
		}
	}
	if !droppedRepoID || !droppedProject {
		t.Errorf("protected drops not recorded for overlay: warnings=%+v", snap.Warnings)
	}

	// Provenance: the protected field stays active in repo-local, never the overlay.
	pr := findProvenance(snap, "repo_id")
	if pr.ActiveLayer != LayerRepoLocal {
		t.Errorf("repo_id active layer = %q, want %q", pr.ActiveLayer, LayerRepoLocal)
	}
}

func TestFlatResolverMalformedOverlayFatal(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"project":"demo"}`)
	writeOverlay(t, repo, `{bad json`)
	if _, err := NewFlatResolver().Resolve(repo); err == nil {
		t.Fatal("expected fatal error on malformed project-local overlay")
	}
}

// TestLayeredResolverOverlayMergesAboveImportsAndRepo proves the overlay is the
// highest local-precedence layer in the layered (extends-present) stack: it sits
// above both the imported extends layers and repo-local committed.
func TestLayeredResolverOverlayMergesAboveImportsAndRepo(t *testing.T) {
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
	writeOverlay(t, repo, `{"version":2,"skills":["local-skill"]}`)

	snap, err := NewLayeredResolver().Resolve(repo)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	ids := layerIDs(snap.Layers)
	want := []string{
		LayerProductDefaults,
		"acme:org/base.json",
		"acme:team/frontend.json",
		LayerRepoLocal,
		LayerProjectLocal,
	}
	if !reflect.DeepEqual(ids, want) {
		t.Errorf("layer ids = %v, want %v", ids, want)
	}

	// set-union: org -> team -> repo -> overlay, overlay appended last.
	wantSkills := []string{"org-base-skill", "frontend-skill", "repo-skill", "local-skill"}
	if !reflect.DeepEqual(snap.Effective.Skills, wantSkills) {
		t.Errorf("skills = %v, want %v", snap.Effective.Skills, wantSkills)
	}
}

func TestLayeredResolverMalformedOverlayFatal(t *testing.T) {
	t.Setenv("AGENTS_HOME", t.TempDir())
	repo := t.TempDir()
	writeManifest(t, repo, `{"version":2,"project":"demo"}`)
	writeOverlay(t, repo, `{bad`)
	if _, err := NewLayeredResolver().Resolve(repo); err == nil {
		t.Fatal("expected fatal error on malformed overlay in layered resolve")
	}
}
