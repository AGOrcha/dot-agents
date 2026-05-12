package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeClaudeProjectJSONL creates ~/.claude/projects/<slug>/<sessionID>.jsonl
// under the given fake home directory, populated with the supplied raw lines.
func writeClaudeProjectJSONL(t *testing.T, home, projectPath, sessionID string, lines []string) {
	t.Helper()
	slug := strings.ReplaceAll(projectPath, "/", "-")
	dir := filepath.Join(home, ".claude", "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir projects dir: %v", err)
	}
	path := filepath.Join(dir, sessionID+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}
}

// Defense-in-depth: the pre-filter is a cheap substring match, but the
// authoritative answer comes from decoding gitBranch and verifying it equals
// the requested branch. This test uses a line with duplicate top-level
// gitBranch keys: the FIRST is the marker target (so the substring pre-filter
// matches), but Go's encoding/json takes the LAST occurrence, so the decoded
// value is "main". Without the post-decode check, the function would
// incorrectly accept this entry as a "feature/real-branch" session.
func TestClaudeScanJSONLForBranch_RejectsWhenDecodedBranchDiffers(t *testing.T) {
	home := t.TempDir()
	project := "/repo/example"
	sess := "11111111-1111-1111-1111-111111111111"
	target := "feature/real-branch"

	craftedLine := `{"sessionId":"` + sess + `","timestamp":"2026-05-11T10:00:00Z","gitBranch":"feature/real-branch","gitBranch":"main"}`
	writeClaudeProjectJSONL(t, home, project, sess, []string{craftedLine})

	slug := strings.ReplaceAll(project, "/", "-")
	path := filepath.Join(home, ".claude", "projects", slug, sess+".jsonl")
	marker := `"gitBranch":"` + target + `"`

	got := claudeScanJSONLForBranch(path, marker, target)
	if got != nil {
		t.Fatalf("expected nil — decoded gitBranch is 'main' (json last-key-wins), got %+v", got)
	}
}

func TestClaudeScanJSONLForBranch_AcceptsRealMatch(t *testing.T) {
	home := t.TempDir()
	project := "/repo/example"
	sess := "22222222-2222-2222-2222-222222222222"
	target := "feature/real-branch"

	good := `{"type":"assistant","sessionId":"22222222-2222-2222-2222-222222222222","timestamp":"2026-05-11T11:30:00Z","gitBranch":"feature/real-branch","message":{"content":[{"type":"text","text":"hello"}]}}`
	writeClaudeProjectJSONL(t, home, project, sess, []string{good})

	slug := strings.ReplaceAll(project, "/", "-")
	path := filepath.Join(home, ".claude", "projects", slug, sess+".jsonl")
	marker := `"gitBranch":"` + target + `"`

	got := claudeScanJSONLForBranch(path, marker, target)
	if got == nil {
		t.Fatalf("expected match, got nil")
	}
	if got.SessionID != sess {
		t.Errorf("SessionID = %q, want %q", got.SessionID, sess)
	}
	if got.Timestamp != "2026-05-11T11:30Z" {
		t.Errorf("Timestamp = %q, want %q (minute-precision trim)", got.Timestamp, "2026-05-11T11:30Z")
	}
}

func TestClaudeScanSessionTokens_SumsUsageWithinTimeWindow(t *testing.T) {
	home := t.TempDir()
	project := "/repo/example"
	sess := "33333333-3333-3333-3333-333333333333"

	// Three assistant entries: one before the cutoff, two after.
	// Only the latter two should be summed.
	lines := []string{
		`{"type":"assistant","timestamp":"2026-05-11T10:00:00Z","message":{"usage":{"input_tokens":100,"output_tokens":200,"cache_read_input_tokens":300,"cache_creation_input_tokens":50}}}`,
		`{"type":"assistant","timestamp":"2026-05-11T12:00:00Z","message":{"usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":900,"cache_creation_input_tokens":100}}}`,
		`{"type":"assistant","timestamp":"2026-05-11T13:00:00Z","message":{"usage":{"input_tokens":5,"output_tokens":7,"cache_read_input_tokens":50,"cache_creation_input_tokens":50}}}`,
	}
	writeClaudeProjectJSONL(t, home, project, sess, lines)

	got := claudeScanSessionTokens(home, project, sess, "2026-05-11T11:00:00Z")

	if got.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", got.MessageCount)
	}
	if got.InputTokens != 15 {
		t.Errorf("InputTokens = %d, want 15", got.InputTokens)
	}
	if got.OutputTokens != 27 {
		t.Errorf("OutputTokens = %d, want 27", got.OutputTokens)
	}
	if got.CacheReadTokens != 950 {
		t.Errorf("CacheReadTokens = %d, want 950", got.CacheReadTokens)
	}
	if got.CacheCreationTokens != 150 {
		t.Errorf("CacheCreationTokens = %d, want 150", got.CacheCreationTokens)
	}
	// hit rate = 950 / (950 + 150) = 0.8636...
	want := 950.0 / 1100.0
	if got.CacheHitRate < want-1e-9 || got.CacheHitRate > want+1e-9 {
		t.Errorf("CacheHitRate = %v, want %v", got.CacheHitRate, want)
	}
}
