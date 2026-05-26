package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// ---------------------------------------------------------------------------
// Claude CreateLinks full fixture + session branch-extraction coverage
// (relocated from coverage_gap2_test.go).
// ---------------------------------------------------------------------------

// setupClaudeFullFixture provisions the full fixture used by the Claude
// CreateLinks test: rules, mcp, legacy hook, agent dir, skill dir, global rule.
func setupClaudeFullFixture(t *testing.T, agentsHome string) {
	t.Helper()
	writeAgentsHomeFile(t, agentsHome, filepath.Join("rules", "proj", "rule.md"), "# rule\n")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("mcp", "proj", "claude.json"), "{}")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("hooks", "proj", "claude-code.json"), `{"hooks":{}}`)
	writeAgentsHomeFile(t, agentsHome, filepath.Join("agents", "proj", "reviewer", "AGENT.md"), "---\nname: reviewer\n---\nbody\n")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("skills", "global", "tidy", "SKILL.md"), "---\nname: tidy\n---\nbody\n")
	writeAgentsHomeFile(t, agentsHome, filepath.Join("rules", "global", "claude-code.md"), "# global\n")
}

func TestClaudeCreateLinks_FullFixture(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	mustMkdirAllT(t, home)

	setupClaudeFullFixture(t, agentsHome)

	repo := filepath.Join(tmp, "repo")
	mustMkdirAllT(t, repo)
	if err := NewClaude().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	// Project rule should be linked.
	if _, err := os.Lstat(filepath.Join(repo, ".claude", "rules", "proj--rule.md")); err != nil {
		t.Errorf("project rule symlink missing: %v", err)
	}
	// .mcp.json symlink.
	if _, err := os.Lstat(filepath.Join(repo, ".mcp.json")); err != nil {
		t.Errorf(".mcp.json missing: %v", err)
	}
	// Legacy hook settings.local.json symlink to legacy file.
	if _, err := os.Lstat(filepath.Join(repo, ".claude", "settings.local.json")); err != nil {
		t.Errorf("settings.local.json missing: %v", err)
	}
}

// TestClaudeExtractBranchSession_UUIDFallback hits the sid-from-uuid branch.
func TestClaudeExtractBranchSession_UUIDFallback(t *testing.T) {
	line := `{"uuid":"uuid-xyz","timestamp":"2026-05-11T10:00:00Z","gitBranch":"main"}`
	sid, ts := claudeExtractBranchSession(line, "main")
	if sid != "uuid-xyz" {
		t.Errorf("sid = %q, want uuid-xyz", sid)
	}
	if ts == "" {
		t.Error("expected non-empty timestamp")
	}
}

func TestClaudeExtractBranchSession_NoSessionOrUUID(t *testing.T) {
	line := `{"timestamp":"2026-05-11T10:00:00Z","gitBranch":"main"}`
	sid, _ := claudeExtractBranchSession(line, "main")
	if sid != "" {
		t.Errorf("expected empty sid, got %q", sid)
	}
}

func TestClaudeExtractBranchSession_BadJSON(t *testing.T) {
	sid, _ := claudeExtractBranchSession("not-json", "main")
	if sid != "" {
		t.Errorf("expected empty for bad json, got %q", sid)
	}
}

// TestClaudeScanJSONLForBranch_AssistantCountReflectsLineHits adds an
// assistant-counting test case to drive the assistantLines++ branch.
func TestClaudeScanJSONLForBranch_AssistantCount(t *testing.T) {
	home := t.TempDir()
	project := "/repo/example"
	sess := "count-test"
	target := "main"
	lines := []string{
		`{"type":"assistant","sessionId":"count-test","gitBranch":"main","timestamp":"2026-05-11T10:00:00Z","message":{"content":""}}`,
		`{"type":"assistant","sessionId":"count-test","gitBranch":"main","timestamp":"2026-05-11T11:00:00Z","message":{"content":""}}`,
	}
	writeClaudeProjectJSONL(t, home, project, sess, lines)
	slug := strings.ReplaceAll(project, "/", "-")
	path := filepath.Join(home, ".claude", "projects", slug, sess+".jsonl")
	marker := `"gitBranch":"` + target + `"`
	got := claudeScanJSONLForBranch(path, marker, target)
	if got == nil {
		t.Fatal("expected match")
	}
	if got.MessageCount < 2 {
		t.Errorf("MessageCount = %d, want >= 2", got.MessageCount)
	}
}

// ---------------------------------------------------------------------------
// Claude assistant-entry accumulator + MCP missing-source coverage
// (relocated from coverage_gap3_test.go).
// ---------------------------------------------------------------------------

// TestClaudeLinkProjectMCPMissing drives the missing-source branch (just to
// hit the early-return).
func TestClaudeLinkProjectMCP_MissingSource(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	c := NewClaude().(*claude)
	c.linkProjectMCP("proj", repo, agentsHome)
	// no symlink created
	if _, err := os.Lstat(filepath.Join(repo, ".mcp.json")); !os.IsNotExist(err) {
		t.Errorf("expected no .mcp.json link: %v", err)
	}
}

// TestClaudeAccumulateAssistantEntry_BadJSON drives the unmarshal-error branch.
func TestClaudeAccumulateAssistantEntry_BadJSON(t *testing.T) {
	var m SessionTokenMetrics
	claudeAccumulateAssistantEntry([]byte("garbage"), time.Time{}, &m)
	if m.InputTokens != 0 {
		t.Errorf("expected zero, got %+v", m)
	}
}

func TestClaudeAccumulateAssistantEntry_AfterCutoffSkipped(t *testing.T) {
	line := `{"type":"assistant","timestamp":"2026-05-11T10:00:00Z","message":{"usage":{"input_tokens":99}}}`
	var m SessionTokenMetrics
	claudeAccumulateAssistantEntry([]byte(line), time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC), &m)
	if m.InputTokens != 0 {
		t.Errorf("expected zero (before cutoff), got %+v", m)
	}
}
