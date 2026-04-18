package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListCanonicalRuleFiles(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "g"
	dir := filepath.Join(agentsHome, "rules", scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.mdc"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.md"), []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "skipdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "binary.bin"), []byte("z"), 0644); err != nil {
		t.Fatal(err)
	}

	got, err := ListCanonicalRuleFiles(agentsHome, scope)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got))
	}
	if got[0].BaseName != "a.mdc" || got[1].BaseName != "b.md" {
		t.Fatalf("unexpected order/names: %#v", got)
	}
}

func TestEnsureUnderRulesScopeTree(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "global"
	dir := filepath.Join(agentsHome, "rules", scope)
	f := filepath.Join(dir, "x.mdc")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureUnderRulesScopeTree(agentsHome, scope, f); err != nil {
		t.Fatalf("expected ok: %v", err)
	}
	outside := filepath.Join(tmp, "outside")
	if err := EnsureUnderRulesScopeTree(agentsHome, scope, outside); err == nil {
		t.Fatal("expected refusal for path outside rules tree")
	}
}

func TestResolveCanonicalRuleFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "p"
	dir := filepath.Join(agentsHome, "rules", scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agents.mdc")
	if err := os.WriteFile(path, []byte("---\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveCanonicalRuleFile(agentsHome, scope, "agents")
	if err != nil {
		t.Fatalf("resolve stem: %v", err)
	}
	if got.BaseName != "agents.mdc" {
		t.Fatalf("want agents.mdc, got %q", got.BaseName)
	}
	got2, err := ResolveCanonicalRuleFile(agentsHome, scope, "agents.mdc")
	if err != nil {
		t.Fatalf("resolve full: %v", err)
	}
	if got2.SourcePath != path {
		t.Fatalf("path mismatch")
	}
	if _, err := ResolveCanonicalRuleFile(agentsHome, scope, "missing"); err == nil {
		t.Fatal("expected error")
	}
}
