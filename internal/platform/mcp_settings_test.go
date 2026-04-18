package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListResolveMCP(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "proj"
	dir := filepath.Join(agentsHome, "mcp", scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mcp.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	specs, err := ListCanonicalMCPFiles(agentsHome, scope)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].BaseName != "mcp.json" {
		t.Fatalf("list: %#v", specs)
	}
	got, err := ResolveCanonicalMCPFile(agentsHome, scope, "mcp")
	if err != nil || got.BaseName != "mcp.json" {
		t.Fatalf("resolve stem: %#v err=%v", got, err)
	}
	if _, err := ResolveCanonicalMCPFile(agentsHome, scope, "nope"); err == nil {
		t.Fatal("expected error")
	}
}

func TestListResolveSettingsIncludesCursorignore(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "g"
	dir := filepath.Join(agentsHome, "settings", scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cursorignore"), []byte("*.log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cursor.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	specs, err := ListCanonicalSettingsFiles(agentsHome, scope)
	if err != nil || len(specs) != 2 {
		t.Fatalf("list: %#v err=%v", specs, err)
	}
	got, err := ResolveCanonicalSettingsFile(agentsHome, scope, "cursorignore")
	if err != nil || got.BaseName != "cursorignore" {
		t.Fatalf("resolve cursorignore: %#v err=%v", got, err)
	}
}

func TestEnsureUnderMCPScopeTree(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "p"
	dir := filepath.Join(agentsHome, "mcp", scope)
	f := filepath.Join(dir, "x.json")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureUnderMCPScopeTree(agentsHome, scope, f); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(agentsHome, "other.json")
	if err := os.WriteFile(outside, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureUnderMCPScopeTree(agentsHome, scope, outside); err == nil {
		t.Fatal("expected refusal")
	}
}
