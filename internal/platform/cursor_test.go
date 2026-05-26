package platform

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
