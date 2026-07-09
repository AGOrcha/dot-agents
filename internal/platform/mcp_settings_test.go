package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/testutil"
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

// ---------------------------------------------------------------------------
// MCP/settings canonical resolver coverage (relocated from coverage_gap_test.go).
// ---------------------------------------------------------------------------

// TestEnsureUnderMCPScopeTreeRejectsOutside covers the negative branch.
func TestEnsureUnderMCPScopeTreeRejectsOutside(t *testing.T) {
	tmp := t.TempDir()
	good := filepath.Join(tmp, "mcp", "proj", "x.json")
	if err := EnsureUnderMCPScopeTree(tmp, "proj", good); err != nil {
		t.Errorf("expected ok, got %v", err)
	}
	bad := filepath.Join(tmp, "other", "x.json")
	if err := EnsureUnderMCPScopeTree(tmp, "proj", bad); err == nil {
		t.Error("expected error for outside path")
	}
}

// TestListCanonicalMCPFilesAndSettingsFiles covers the listing helpers and
// filename filters.
func TestListCanonicalMCPFilesAndSettings(t *testing.T) {
	tmp := t.TempDir()
	mkfile := func(p, s string) {
		full := filepath.Join(tmp, p)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(s), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mkfile("mcp/proj/a.json", "{}")
	mkfile("mcp/proj/.dotfile.json", "{}")
	mkfile("mcp/proj/c.txt", "skip")
	if err := os.MkdirAll(filepath.Join(tmp, "mcp", "proj", "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := ListCanonicalMCPFiles(tmp, "proj")
	if err != nil {
		t.Fatalf("list mcp: %v", err)
	}
	if len(got) != 1 || got[0].BaseName != "a.json" {
		t.Errorf("got %+v, want one entry [a.json]", got)
	}

	mkfile("settings/proj/cursor.json", "{}")
	mkfile("settings/proj/cursorignore", "ign")
	mkfile("settings/proj/random.md", "skip")
	mkfile("settings/proj/.dotfile", "skip")
	gotS, err := ListCanonicalSettingsFiles(tmp, "proj")
	if err != nil {
		t.Fatalf("list settings: %v", err)
	}
	if len(gotS) != 2 {
		t.Errorf("got %+v, want 2 (cursor.json + cursorignore)", gotS)
	}

	// Missing dir
	if _, err := ListCanonicalMCPFiles(tmp, "nonexistent"); err == nil {
		t.Error("expected error for missing scope")
	}
}

// TestResolveCanonicalMCPFile_NotFound exercises the error branch.
func TestResolveCanonicalMCPFile_NotFound(t *testing.T) {
	tmp := t.TempDir()
	if _, err := ResolveCanonicalMCPFile(tmp, "proj", "mcp"); err == nil {
		t.Error("expected error for missing file")
	}
	if _, err := ResolveCanonicalMCPFile(tmp, "proj", ""); err == nil {
		t.Error("expected error for empty name")
	}
}

func TestResolveCanonicalSettingsFile_Found(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "settings", "global")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "cursor.json")
	if err := os.WriteFile(src, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolveCanonicalSettingsFile(tmp, "global", "cursor")
	if err != nil {
		t.Fatalf("ResolveCanonicalSettingsFile: %v", err)
	}
	if got.SourcePath != src {
		t.Errorf("source %q, want %q", got.SourcePath, src)
	}
}

// TestResolveCanonicalMCPFileStatErrorSurfaced covers the real-error branch
// in resolveCanonicalFileByExt: a candidate whose immediate parent directory
// is unreadable must surface a distinct error (not the generic not-found
// message a legitimately-absent scope produces), so callers can't mistake a
// masked permission/I-O failure for "the file was never created".
func TestResolveCanonicalMCPFileStatErrorSurfaced(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "proj"
	testutil.WriteScopeFile(t, agentsHome, "mcp", scope, "mcp.json", []byte("{}"))
	testutil.MakeDirUnreadable(t, filepath.Join(agentsHome, "mcp", scope))

	_, err := ResolveCanonicalMCPFile(agentsHome, scope, "mcp")
	if err == nil {
		t.Fatal("expected an error for the unreadable scope dir")
	}
	if os.IsNotExist(err) {
		t.Fatalf("real stat error must not read as not-exist, got %v", err)
	}
	if !strings.Contains(err.Error(), "checking mcp candidate") {
		t.Errorf("expected the real-error message, got %q", err.Error())
	}
}
