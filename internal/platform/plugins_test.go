package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// TestListPluginSpecs_WithMultipleScopes drives the no-scope-walk branch which
// recurses over each subdirectory in agentsHome/plugins/.
func TestListPluginSpecs_WithMultipleScopes(t *testing.T) {
	tmp := t.TempDir()
	mk := func(scope, name string) {
		dir := filepath.Join(tmp, "plugins", scope, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		manifest := `schema_version: 1
kind: native
name: ` + name + `
platforms:
  - claude
`
		if err := os.WriteFile(filepath.Join(dir, PluginManifestName), []byte(manifest), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mk("global", "alpha")
	mk("global", "beta")
	mk("proj", "gamma")

	specs, err := ListPluginSpecs(tmp, "")
	if err != nil {
		t.Fatalf("ListPluginSpecs: %v", err)
	}
	if len(specs) != 3 {
		t.Errorf("expected 3 specs, got %d", len(specs))
	}

	// Scope-scoped listing.
	specs, err = ListPluginSpecs(tmp, "global")
	if err != nil {
		t.Fatalf("scope listing: %v", err)
	}
	if len(specs) != 2 {
		t.Errorf("expected 2 specs in global, got %d", len(specs))
	}
	if specs[0].Scope != "global" {
		t.Errorf("Scope = %q, want global", specs[0].Scope)
	}
}

func TestListPluginSpecs_BadManifestPropagates(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "plugins", "global", "broken")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, PluginManifestName), []byte(":\n  -bad-yaml"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := ListPluginSpecs(tmp, ""); err == nil {
		t.Error("expected error to propagate from broken manifest")
	}
	if _, err := ListPluginSpecs(tmp, "global"); err == nil {
		t.Error("expected scoped error to propagate")
	}
}

func TestListPluginSpecs_SkipsNonDirInsideScope(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "plugins", "global")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	specs, err := ListPluginSpecs(tmp, "global")
	if err != nil {
		t.Fatalf("ListPluginSpecs: %v", err)
	}
	if len(specs) != 0 {
		t.Errorf("expected 0 specs, got %d", len(specs))
	}
}
