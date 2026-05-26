package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

func TestListCanonicalRuleFiles(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "g"

	testutil.WriteScopeFile(t, agentsHome, "rules", scope, "a.mdc", []byte("x"))
	testutil.WriteScopeFile(t, agentsHome, "rules", scope, "b.md", []byte("y"))
	testutil.WriteScopeFile(t, agentsHome, "rules", scope, "binary.bin", []byte("z"))
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", scope, "skipdir"), 0755); err != nil {
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

	testutil.WriteScopeFile(t, agentsHome, "rules", scope, "x.mdc", []byte("1"))
	f := filepath.Join(agentsHome, "rules", scope, "x.mdc")

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

	testutil.WriteScopeFile(t, agentsHome, "rules", scope, "agents.mdc", []byte("---\n---\n"))
	path := filepath.Join(agentsHome, "rules", scope, "agents.mdc")

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

// TestEnsureUnderRulesScopeTree_DotPath exercises filepath.Rel with same dir
// (relocated from coverage_gap3_test.go).
func TestEnsureUnderRulesScopeTree_RootItself(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "rules", "global")
	// Same path == root → rel is "." → not "..", so accepted.
	if err := EnsureUnderRulesScopeTree(tmp, "global", root); err != nil {
		t.Errorf("expected root itself to validate, got %v", err)
	}
}

// TestEnsureUnderRulesScopeTreeRejectsOutside covers the negative branch of
// EnsureUnderRulesScopeTree (relocated from coverage_gap_test.go).
func TestEnsureUnderRulesScopeTreeRejectsOutside(t *testing.T) {
	tmp := t.TempDir()
	good := filepath.Join(tmp, "rules", "global", "x.md")
	if err := EnsureUnderRulesScopeTree(tmp, "global", good); err != nil {
		t.Errorf("expected path under tree to pass, got %v", err)
	}
	bad := filepath.Join(tmp, "elsewhere", "x.md")
	if err := EnsureUnderRulesScopeTree(tmp, "global", bad); err == nil {
		t.Error("expected error for path outside tree")
	}
}

// TestResolveCanonicalRuleFileNotFound exercises rules.go missing branches
// (relocated from coverage_gap_test.go).
func TestResolveCanonicalRuleFileMissing(t *testing.T) {
	tmp := t.TempDir()
	if _, err := ResolveCanonicalRuleFile(tmp, "global", "missing"); err == nil {
		t.Error("expected error for missing rule")
	}
	if _, err := ResolveCanonicalRuleFile(tmp, "global", ""); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestListCanonicalRuleFilesMissing(t *testing.T) {
	if _, err := ListCanonicalRuleFiles(t.TempDir(), "global"); err == nil {
		t.Error("expected error for missing scope dir")
	}
}
