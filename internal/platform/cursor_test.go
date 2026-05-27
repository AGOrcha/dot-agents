package platform

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/linktest"
)

// writeCursorAgentTool creates ~/.cursor/projects/<slug>/agent-tools/<name>.txt
// containing the supplied lines. Returns the absolute path to the file.
func writeCursorAgentTool(t *testing.T, home, projectPath, name string, lines []string) string {
	t.Helper()
	slug := strings.ReplaceAll(strings.TrimPrefix(projectPath, "/"), "/", "-")
	dir := filepath.Join(home, ".cursor", "projects", slug, "agent-tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir agent-tools dir: %v", err)
	}
	path := filepath.Join(dir, name+".txt")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	return path
}

func TestCursorScanSessionTokens_AggregatesResultLines(t *testing.T) {
	home := t.TempDir()
	project := "/repo/example"

	writeCursorAgentTool(t, home, project, "run-aaa", []string{
		`{"type":"system","content":"start"}`,
		`{"type":"result","usage":{"inputTokens":100,"outputTokens":200,"cacheReadTokens":300,"cacheWriteTokens":50}}`,
	})
	writeCursorAgentTool(t, home, project, "run-bbb", []string{
		`{"type":"system","content":"start"}`,
		`{"type":"result","usage":{"inputTokens":1,"outputTokens":2,"cacheReadTokens":3,"cacheWriteTokens":4}}`,
	})

	got := cursorScanSessionTokens(home, project, "")

	if got.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", got.MessageCount)
	}
	if got.InputTokens != 101 {
		t.Errorf("InputTokens = %d, want 101", got.InputTokens)
	}
	if got.OutputTokens != 202 {
		t.Errorf("OutputTokens = %d, want 202", got.OutputTokens)
	}
	if got.CacheReadTokens != 303 {
		t.Errorf("CacheReadTokens = %d, want 303", got.CacheReadTokens)
	}
	if got.CacheCreationTokens != 54 {
		t.Errorf("CacheCreationTokens = %d, want 54", got.CacheCreationTokens)
	}
}

func TestCursorScanSessionTokens_FiltersByMtime(t *testing.T) {
	home := t.TempDir()
	project := "/repo/example"

	oldPath := writeCursorAgentTool(t, home, project, "run-old", []string{
		`{"type":"result","usage":{"inputTokens":1000,"outputTokens":2000,"cacheReadTokens":0,"cacheWriteTokens":0}}`,
	})
	newPath := writeCursorAgentTool(t, home, project, "run-new", []string{
		`{"type":"result","usage":{"inputTokens":7,"outputTokens":11,"cacheReadTokens":0,"cacheWriteTokens":0}}`,
	})

	cutoff := time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC)
	older := cutoff.Add(-2 * time.Hour)
	newer := cutoff.Add(2 * time.Hour)
	if err := os.Chtimes(oldPath, older, older); err != nil {
		t.Fatalf("chtimes old: %v", err)
	}
	if err := os.Chtimes(newPath, newer, newer); err != nil {
		t.Fatalf("chtimes new: %v", err)
	}

	got := cursorScanSessionTokens(home, project, cutoff.Format(time.RFC3339))

	if got.MessageCount != 1 {
		t.Errorf("MessageCount = %d, want 1 (only new file should match)", got.MessageCount)
	}
	if got.InputTokens != 7 {
		t.Errorf("InputTokens = %d, want 7 (old file's 1000 must not contribute)", got.InputTokens)
	}
	if got.OutputTokens != 11 {
		t.Errorf("OutputTokens = %d, want 11", got.OutputTokens)
	}
}

// ---------------------------------------------------------------------------
// Cursor SQLite usage-stats reader tests (relocated from coverage_gap2).
// ---------------------------------------------------------------------------

// TestCursorReadUsageStats_DrivesSQLitePath seeds an in-tree SQLite database
// matching cursor's schema and verifies cursorReadUsageStats returns rows.
func TestCursorReadUsageStats_DrivesSQLitePath(t *testing.T) {
	home := t.TempDir()
	dbDir := filepath.Join(home, ".cursor", "ai-tracking")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dbDir, "ai-code-tracking.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE scored_commits (
		commitHash TEXT, branchName TEXT, scoredAt INTEGER,
		linesAdded INTEGER, linesDeleted INTEGER,
		composerLinesAdded INTEGER, composerLinesDeleted INTEGER,
		humanLinesAdded INTEGER, v2AiPercentage REAL
	)`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO scored_commits VALUES
		('hash1', 'main', 1715000000000, 100, 10, 60, 5, 40, 60.0),
		('hash2', 'feat', 1714000000000, 50, 5, 20, 0, 30, 40.0)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	stats := cursorReadUsageStats(home)
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.PlatformID != "cursor" {
		t.Errorf("PlatformID = %q", stats.PlatformID)
	}
	if len(stats.CommitAttribution) != 2 {
		t.Errorf("expected 2 commits, got %d", len(stats.CommitAttribution))
	}
}

func TestCursorReadUsageStats_NoDB(t *testing.T) {
	if stats := cursorReadUsageStats(t.TempDir()); stats != nil {
		t.Errorf("expected nil for missing DB, got %+v", stats)
	}
}

func TestCursorReadUsageStats_QueryError(t *testing.T) {
	home := t.TempDir()
	dbDir := filepath.Join(home, ".cursor", "ai-tracking")
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Empty file → sql.Open succeeds but Query errors (no table).
	if err := os.WriteFile(filepath.Join(dbDir, "ai-code-tracking.db"), []byte{}, 0644); err != nil {
		t.Fatal(err)
	}
	if stats := cursorReadUsageStats(home); stats != nil {
		t.Errorf("expected nil for missing table, got %+v", stats)
	}
}

// ---------------------------------------------------------------------------
// Cursor CreateLinks full-fixture test (relocated from coverage_gap2).
// ---------------------------------------------------------------------------

// setupCursorFullFixture provisions the full fixture for the cursor
// CreateLinks test: rules for both scopes, settings, cursorignore, mcp, hooks.
func setupCursorFullFixture(t *testing.T, agentsHome string) {
	t.Helper()
	for _, scope := range []string{"global", "proj"} {
		writeAgentsHomeFile(t, agentsHome, filepath.Join("rules", scope, "x.md"), "# rule\n")
	}
	writeAgentsHomeFile(t, agentsHome, filepath.Join("settings", "proj", "cursor.json"), "{}")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("settings", "proj", "cursorignore"), "node_modules\n")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("mcp", "proj", "cursor.json"), "{}")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("hooks", "proj", "cursor.json"), "{}")
}

func TestCursorCreateLinks_FullFixture(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	mustMkdirAllT(t, filepath.Join(tmp, "home"))

	setupCursorFullFixture(t, agentsHome)

	repo := filepath.Join(tmp, "repo")
	mustMkdirAllT(t, repo)
	if err := NewCursor().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	for _, expect := range []string{
		filepath.Join(repo, ".cursor", "rules"),
		filepath.Join(repo, ".cursor", "settings.json"),
		filepath.Join(repo, ".cursor", "mcp.json"),
		filepath.Join(repo, ".cursorignore"),
		filepath.Join(repo, ".cursor", "hooks.json"),
	} {
		if _, err := os.Stat(expect); err != nil {
			t.Errorf("expected %s: %v", expect, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Cursor rule/agent + deprecation detection coverage (relocated from
// coverage_gap3_test.go).
// ---------------------------------------------------------------------------

// TestCollectRuleEntry_NonRuleFile drives the isCursorRuleFile guard.
func TestCollectRuleEntry_NonRuleFile(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "ignore.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	desired := map[string]string{}
	c := NewCursor().(*cursor)
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		c.collectRuleEntry(entry, tmp, "prefix--", desired)
	}
	if len(desired) != 0 {
		t.Errorf("expected 0 entries, got %d (%+v)", len(desired), desired)
	}
}

// TestHasDeprecatedFormatAndDetails covers the matching branch of each
// platform's deprecation detector (the not-matching branch is exercised by the
// contract test). Spans both claude and cursor since both are
// per-platform detectors used identically.
func TestHasDeprecatedFormat_Detected(t *testing.T) {
	tmp := t.TempDir()
	// Claude deprecated marker.
	repoC := filepath.Join(tmp, "claude-repo")
	if err := os.MkdirAll(repoC, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoC, ".claude.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	cp := NewClaude()
	if !cp.HasDeprecatedFormat(repoC) {
		t.Error("expected claude deprecated detection")
	}
	if cp.DeprecatedDetails(repoC) == "" {
		t.Error("expected non-empty deprecated details")
	}

	// Cursor deprecated marker.
	repoCur := filepath.Join(tmp, "cursor-repo")
	if err := os.MkdirAll(repoCur, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repoCur, ".cursorrules"), []byte("rules"), 0644); err != nil {
		t.Fatal(err)
	}
	curp := NewCursor()
	if !curp.HasDeprecatedFormat(repoCur) {
		t.Error("expected cursor deprecated detection")
	}
	if curp.DeprecatedDetails(repoCur) == "" {
		t.Error("expected non-empty deprecated details")
	}
}

// TestRulePrune_MissingDir covers the err==nil return-nil branch.
func TestCursorPruneRuleLinks_MissingDir(t *testing.T) {
	c := NewCursor().(*cursor)
	if err := c.pruneRuleLinks(filepath.Join(t.TempDir(), "no-such"), "proj", nil); err != nil {
		t.Errorf("missing dir should no-op, got %v", err)
	}
}

// TestCursorRemoveAgentLinks_MissingDir covers the err==nil return branch.
func TestCursorRemoveAgentLinks_MissingDir(t *testing.T) {
	c := NewCursor().(*cursor)
	c.removeAgentLinks(filepath.Join(t.TempDir(), "no-such"), filepath.Join(t.TempDir(), ".agents"))
	// no panic = pass
}

// TestCursorCreateLinks_SecondRunIdempotent drives the second-pass execution
// of cursor CreateLinks where target files already exist (relocated from
// coverage_gap5_test.go).
func TestCursorCreateLinks_SecondRunIdempotent(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	if err := os.MkdirAll(filepath.Join(tmp, "home"), 0755); err != nil {
		t.Fatal(err)
	}
	// Rule.
	ruleSrc := filepath.Join(agentsHome, "rules", "proj", "x.md")
	if err := os.MkdirAll(filepath.Dir(ruleSrc), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ruleSrc, []byte("# rule"), 0644); err != nil {
		t.Fatal(err)
	}
	// Settings.
	if err := os.MkdirAll(filepath.Join(agentsHome, "settings", "proj"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentsHome, "settings", "proj", "cursor.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	cur := NewCursor()
	for i := 0; i < 2; i++ {
		if err := cur.CreateLinks("proj", repo); err != nil {
			t.Fatalf("CreateLinks pass %d: %v", i, err)
		}
	}
	if err := cur.RemoveLinks("proj", repo); err != nil {
		t.Errorf("RemoveLinks: %v", err)
	}
}

// TestCursorRemoveLinksWithExistingAgentLinks drives removeAgentLinks via a
// seeded `.cursor/agents/<name>` symlink (relocated from coverage_gap_test.go).
func TestCursorRemoveLinksWithExistingAgentLinks(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(agentsHome, "agents", "proj", "reviewer")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(repo, ".cursor", "agents", "reviewer")
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, src, dst)
	if err := NewCursor().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Error("cursor agent symlink should be removed")
	}
}

// ---------- BrokenLinkReporter implementation (P1) ----------

// TestCursorBrokenLinks_EmptyProject covers the "absent rules dir" branch:
// no managed surface yet, no diagnostics. Mirrors the lifecycle-side
// TestCollectBrokenLinks_EmptyProject contract that absent != broken.
func TestCursorBrokenLinks_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}

	c := &cursor{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("expected no broken links in empty project, got %d: %+v", len(got), got)
	}
}

// TestCursorBrokenLinks_BrokenGlobalHardlink is the central broken-hardlink
// positive case for the global-scope branch. A loose .mdc file under
// .cursor/rules with the global-- prefix that is NOT hard-linked to the
// canonical source must surface with PlatformID="cursor".
func TestCursorBrokenLinks_BrokenGlobalHardlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectPath := filepath.Join(tmp, "proj")
	rulesDir := filepath.Join(projectPath, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(rulesDir, "global--rule.mdc")
	if err := os.WriteFile(loose, []byte("rule"), 0644); err != nil {
		t.Fatal(err)
	}

	c := &cursor{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 {
		t.Fatalf("expected 1 broken link, got %d: %+v", len(got), got)
	}
	if got[0].PlatformID != "cursor" {
		t.Errorf("PlatformID = %q, want cursor", got[0].PlatformID)
	}
	if got[0].LinkPath != loose {
		t.Errorf("LinkPath = %q, want %q", got[0].LinkPath, loose)
	}
	if got[0].DisplayDest == "" {
		t.Error("DisplayDest should be populated")
	}
}

// TestCursorBrokenLinks_BrokenProjectHardlink mirrors the global-scope test
// for the project-scope branch (project-- prefix).
func TestCursorBrokenLinks_BrokenProjectHardlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectPath := filepath.Join(tmp, "proj")
	rulesDir := filepath.Join(projectPath, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "proj--rule.mdc"), []byte("rule"), 0644); err != nil {
		t.Fatal(err)
	}

	c := &cursor{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 || got[0].PlatformID != "cursor" {
		t.Fatalf("expected 1 cursor broken link, got %+v", got)
	}
}

// TestCursorBrokenLinks_HealthyHardlinkSkipped is the central negative case:
// a hard-linked rule whose inode matches the canonical source must NOT be
// reported as broken (the canonical contract is "shares an inode", not
// "exists at expected path").
func TestCursorBrokenLinks_HealthyHardlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectPath := filepath.Join(tmp, "proj")
	rulesDir := filepath.Join(projectPath, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(agentsHome, "rules", "global", "rule.mdc")
	if err := os.MkdirAll(filepath.Dir(canonical), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte("rule"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(canonical, filepath.Join(rulesDir, "global--rule.mdc")); err != nil {
		t.Fatal(err)
	}

	c := &cursor{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("healthy hardlink should not be broken, got %+v", got)
	}
}

// TestCursorBrokenLinks_UnmanagedEntriesIgnored guards the silent-skip
// contract for entries that aren't managed cursor rules: backup artifacts,
// non-.mdc files, and entries that don't carry the global-- or
// <project>-- prefix.
func TestCursorBrokenLinks_UnmanagedEntriesIgnored(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectPath := filepath.Join(tmp, "proj")
	rulesDir := filepath.Join(projectPath, ".cursor", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	for _, fname := range []string{
		"global--rule.mdc.dot-agents-backup",
		"loose.txt",
		"foreign-project--rule.mdc",
	} {
		if err := os.WriteFile(filepath.Join(rulesDir, fname), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	c := &cursor{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("unmanaged entries must be ignored, got %+v", got)
	}
}

// TestCursorBrokenLinks_InterfaceConformance pins that cursor satisfies
// BrokenLinkReporter at compile time, which is what doctor.collectBrokenLinks
// type-asserts on.
func TestCursorBrokenLinks_InterfaceConformance(t *testing.T) {
	var _ BrokenLinkReporter = (*cursor)(nil)
}
