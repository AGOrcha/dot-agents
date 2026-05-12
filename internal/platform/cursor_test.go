package platform

import (
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
