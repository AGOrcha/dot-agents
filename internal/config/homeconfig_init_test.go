package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeUserLocal writes a user-local .agentsrc.json into a temp AgentsHome and
// points AGENTS_HOME at it. Returns the home path.
func writeUserLocal(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("AGENTS_HOME", home)
	if body != "" {
		if err := os.WriteFile(filepath.Join(home, AgentsRCFile), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// TestUserScopeSnapshot_WithUserLocal resolves the user scope when a user-local
// layer is present — the cloned-home case init --from resolves.
func TestUserScopeSnapshot_WithUserLocal(t *testing.T) {
	writeUserLocal(t, `{"defaults":{"agent":"claude"}}`)
	snap, err := UserScopeSnapshot()
	if err != nil {
		t.Fatalf("UserScopeSnapshot: %v", err)
	}
	if snap == nil {
		t.Fatal("nil snapshot")
	}
}

// TestUserScopeSnapshot_NoUserLocal resolves a home with no user-local layer:
// the snapshot carries only product defaults and is not an error.
func TestUserScopeSnapshot_NoUserLocal(t *testing.T) {
	writeUserLocal(t, "")
	if _, err := UserScopeSnapshot(); err != nil {
		t.Fatalf("missing user-local must not error: %v", err)
	}
}

// TestUserScopeSnapshot_MalformedUserLocal covers the decode-error branch.
func TestUserScopeSnapshot_MalformedUserLocal(t *testing.T) {
	writeUserLocal(t, "{not json")
	if _, err := UserScopeSnapshot(); err == nil {
		t.Error("expected parse error for malformed user-local layer")
	}
}

// TestResolveUserScopeManifests_WithManifest resolves a home whose user-local
// layer declares a kind:manifest unit — the inputs init --from reproduces:
// referenced sources + the optional project-set ref.
func TestResolveUserScopeManifests_WithManifest(t *testing.T) {
	writeUserLocal(t, `{
  "manifests": {
    "home": {
      "sources": ["team:base@v1.0.0"],
      "project_set": "team:projects"
    }
  }
}`)
	got, err := ResolveUserScopeManifests()
	if err != nil {
		t.Fatalf("ResolveUserScopeManifests: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 resolved manifest, got %d", len(got))
	}
	m := got[0]
	if len(m.Sources) != 1 || m.Sources[0] != "team:base@v1.0.0" {
		t.Errorf("manifest sources = %v", m.Sources)
	}
	if !m.HasProjectSet || m.ProjectSet != "team:projects" {
		t.Errorf("project-set ref = %q (has=%v)", m.ProjectSet, m.HasProjectSet)
	}
}

// TestResolveUserScopeManifests_NoManifest: a home with no manifest is valid and
// resolves to an empty set (sources/policy resolve directly).
func TestResolveUserScopeManifests_NoManifest(t *testing.T) {
	writeUserLocal(t, `{"defaults":{"agent":"codex"}}`)
	got, err := ResolveUserScopeManifests()
	if err != nil {
		t.Fatalf("ResolveUserScopeManifests: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty manifest set, got %d", len(got))
	}
}

// TestResolveUserScopeManifests_SnapshotError propagates a malformed user-local
// layer as a resolve error rather than silently resolving nothing.
func TestResolveUserScopeManifests_SnapshotError(t *testing.T) {
	writeUserLocal(t, "{bad")
	if _, err := ResolveUserScopeManifests(); err == nil {
		t.Error("expected error from malformed user-local layer")
	}
}

// TestResolveUserScopeManifests_ForbiddenField covers a manifest carrying a
// forbidden field (self-declared authority): UserScopeSnapshot's typed decode
// rejects it before resolution even begins (the fail-closed gate).
func TestResolveUserScopeManifests_ForbiddenField(t *testing.T) {
	writeUserLocal(t, `{"manifests": {"home": {"authority": "team"}}}`)
	if _, err := ResolveUserScopeManifests(); err == nil {
		t.Error("expected forbidden-field rejection")
	}
}

// TestResolveManifestsInSnapshot_ManifestSetError covers the ManifestSetFromSnapshot
// fail-closed branch directly: a self-blessing authority_grants makes the
// source-derived authority pass fail, which must propagate.
func TestResolveManifestsInSnapshot_ManifestSetError(t *testing.T) {
	snap := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerUserLocal, Present: true, Raw: map[string]any{
			"authority_grants": map[string]any{"acme": "org"},
		}},
	}}
	if _, err := resolveManifestsInSnapshot(snap); err == nil {
		t.Error("a ManifestSetFromSnapshot authority error must propagate")
	}
}

// TestResolveManifestsInSnapshot_ProfileSetError covers the ProfileSetFromSnapshot
// fail-closed branch: a malformed layering_policy (invalid mode) aborts the
// resolve rather than binding a half-decoded policy.
func TestResolveManifestsInSnapshot_ProfileSetError(t *testing.T) {
	snap := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"layering_policy": map[string]any{"mode": "bogus"},
		}},
	}}
	if _, err := resolveManifestsInSnapshot(snap); err == nil {
		t.Error("a malformed layering_policy must fail the profile-set derivation closed")
	}
}

// TestResolveManifestsInSnapshot_ResolveError covers the per-manifest
// ResolveManifest fail-closed branch: a valid manifest bound to a policy with
// overlapping value-locks owned by an authoritative scope propagates the §15
// authority violation out of the resolve loop.
func TestResolveManifestsInSnapshot_ResolveError(t *testing.T) {
	snap := &Snapshot{Layers: []ResolvedLayer{
		{ID: LayerRepoLocal, Present: true, Raw: map[string]any{
			"manifests": map[string]any{"home": map[string]any{"sources": []any{"team:base@v1.0.0"}}},
			"layering_policy": map[string]any{"locks": []any{
				map[string]any{"field": "features", "value": "x"},
				map[string]any{"field": "features.graph", "value": "x"},
			}},
		}},
	}}
	if _, err := resolveManifestsInSnapshot(snap); err == nil {
		t.Error("a fatal §15 authority violation must propagate from ResolveManifest")
	}
}
