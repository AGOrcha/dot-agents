package platform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

func TestListResolveMCP(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "proj"

	testutil.WriteScopeFile(t, agentsHome, "mcp", scope, "mcp.json", []byte("{}"))

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

	testutil.WriteScopeFile(t, agentsHome, "settings", scope, "cursorignore", []byte("*.log\n"))
	testutil.WriteScopeFile(t, agentsHome, "settings", scope, "cursor.json", []byte("{}"))

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

	testutil.WriteScopeFile(t, agentsHome, "mcp", scope, "x.json", []byte("{}"))
	f := filepath.Join(agentsHome, "mcp", scope, "x.json")

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

// TestResolveCanonicalSettingsFile_NotFound exercises the error branch
// (relocated from coverage_gap3_test.go).
func TestResolveCanonicalSettingsFile_NotFound(t *testing.T) {
	if _, err := ResolveCanonicalSettingsFile(t.TempDir(), "proj", "missing"); err == nil {
		t.Error("expected error for missing file")
	}
}
