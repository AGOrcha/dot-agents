package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/linktest"
	"github.com/AGOrcha/dot-agents/internal/testutil"
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

// ---------------------------------------------------------------------------
// Claude ensureUser*, linkUserAgent, pruneProjectRuleLinks, mkdir-blocker,
// scan-session-tokens, scan-jsonl-for-branch coverage (relocated from
// coverage_gap5_test.go).
// ---------------------------------------------------------------------------

// TestClaudeEnsureUserRules_PreExistingSymlinkSkipped drives the "already a
// symlink → continue" branch.
func TestClaudeEnsureUserRules_PreExistingSymlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed source.
	src := filepath.Join(agentsHome, "rules", "global", "rules.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("# rules"), 0644); err != nil {
		t.Fatal(err)
	}
	// Pre-existing symlink target.
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	pretend := filepath.Join(tmp, "pretend.md")
	if err := os.WriteFile(pretend, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, pretend, filepath.Join(home, ".claude", "CLAUDE.md"))

	c := NewClaude().(*claude)
	if err := c.ensureUserRules(agentsHome); err != nil {
		t.Fatalf("ensureUserRules: %v", err)
	}
	// Managed link should not have been changed.
	if !links.IsManagedLink(filepath.Join(home, ".claude", "CLAUDE.md"), pretend) {
		t.Errorf("managed link to %q was not preserved", pretend)
	}
}

// TestClaudeEnsureUserSettings_LegacyPathWithExistingSymlink covers the
// settings.json continue-on-existing-symlink branch.
func TestClaudeEnsureUserSettings_PreExistingSymlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	// Legacy spec file under settings/global.
	legacy := filepath.Join(agentsHome, "settings", "global", "claude-code.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	// Pre-existing symlink in user home.
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	pretend := filepath.Join(tmp, "pretend.json")
	if err := os.WriteFile(pretend, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, pretend, filepath.Join(home, ".claude", "settings.json"))

	c := NewClaude().(*claude)
	if err := c.ensureUserSettings(agentsHome); err != nil {
		t.Fatalf("ensureUserSettings: %v", err)
	}
}

// TestClaudeEnsureUserSettings_NoSpecRemovesStale drives the
// "spec == nil → removeManagedFileIf" branch.
func TestClaudeEnsureUserSettings_NoSpecRemoves(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0755); err != nil {
		t.Fatal(err)
	}
	// Pre-write a managed-looking rendered settings file.
	content, err := renderClaudeHookSettings([]HookSpec{{Name: "x", When: "pre_tool_use", Command: "/bin/true"}})
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".claude", "settings.json")
	if err := os.WriteFile(target, content, 0644); err != nil {
		t.Fatal(err)
	}
	c := NewClaude().(*claude)
	if err := c.ensureUserSettings(agentsHome); err != nil {
		t.Fatalf("ensureUserSettings: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Error("expected managed settings.json removed")
	}
}

// TestClaudeLinkUserAgent_PreExistingSymlinkSkipped drives the symlink-skip branch.
func TestClaudeLinkUserAgent_SymlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	agentDir := filepath.Join(tmp, "agents", "global", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte("body"), 0644); err != nil {
		t.Fatal(err)
	}
	userAgentsDir := filepath.Join(tmp, "userhome", ".claude", "agents")
	if err := os.MkdirAll(userAgentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	pretend := filepath.Join(tmp, "x")
	if err := os.MkdirAll(pretend, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, pretend, filepath.Join(userAgentsDir, "reviewer"))

	c := NewClaude().(*claude)
	entries, err := os.ReadDir(filepath.Join(tmp, "agents", "global"))
	if err != nil {
		t.Fatal(err)
	}
	c.linkUserAgent(filepath.Join(tmp, "agents", "global"), userAgentsDir, entries[0])
	// Managed link should remain pointing at pretend.
	if !links.IsManagedLink(filepath.Join(userAgentsDir, "reviewer"), pretend) {
		t.Errorf("link changed, expected preserved link to %q", pretend)
	}
}

// TestClaudeLinkUserAgent_NonAgentDirSkipped drives the !isClaudeAgentDir branch.
func TestClaudeLinkUserAgent_NonAgentDirSkipped(t *testing.T) {
	tmp := t.TempDir()
	// Directory without AGENT.md
	dir := filepath.Join(tmp, "agents", "global", "no-agent")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	userAgentsDir := filepath.Join(tmp, "userhome", ".claude", "agents")
	if err := os.MkdirAll(userAgentsDir, 0755); err != nil {
		t.Fatal(err)
	}
	c := NewClaude().(*claude)
	entries, err := os.ReadDir(filepath.Join(tmp, "agents", "global"))
	if err != nil {
		t.Fatal(err)
	}
	c.linkUserAgent(filepath.Join(tmp, "agents", "global"), userAgentsDir, entries[0])
	// No symlink should be created.
	if _, err := os.Lstat(filepath.Join(userAgentsDir, "no-agent")); !os.IsNotExist(err) {
		t.Error("non-agent dir should be skipped")
	}
}

// TestClaudePruneProjectRuleLinks_NonMatchingPreserved drives the non-prefix
// continue branch.
func TestClaudePruneProjectRuleLinks_NonMatchingPreserved(t *testing.T) {
	tmp := t.TempDir()
	rulesDir := filepath.Join(tmp, "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	// File with different prefix; should be left alone.
	other := filepath.Join(rulesDir, "global--keep.md")
	if err := os.WriteFile(other, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// File that has subdir name (directory entry skipped).
	sub := filepath.Join(rulesDir, "subdir")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	// File with project prefix that's in keep map.
	keepP := filepath.Join(rulesDir, "proj--keep.md")
	if err := os.WriteFile(keepP, []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	// File with project prefix that should be removed.
	stale := filepath.Join(rulesDir, "proj--stale.md")
	if err := os.WriteFile(stale, []byte("z"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewClaude().(*claude)
	wanted := map[string]string{"proj--keep.md": "/no/where"}
	if err := c.pruneProjectRuleLinks(rulesDir, "proj", wanted); err != nil {
		t.Fatalf("pruneProjectRuleLinks: %v", err)
	}
	for _, expect := range []string{other, keepP} {
		if _, err := os.Stat(expect); err != nil {
			t.Errorf("expected preserved: %s (%v)", expect, err)
		}
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale entry should be removed")
	}
}

// TestResolveClaudeCodeModelFromJSONL_NonAssistant lines drives the
// extract-function-returns-empty branch.
func TestResolveClaudeCodeModelFromJSONL_NonAssistantLine(t *testing.T) {
	home := t.TempDir()
	project := "/repo/x"
	sess := "no-asst"
	lines := []string{
		`{"type":"user","message":{"content":"hi"}}`,
		// substring-matched but with non-assistant type:
		`{"type":"user","gitBranch":"x","message":{"content":"assistant somewhere in body"}}`,
	}
	writeClaudeProjectJSONL(t, home, project, sess, lines)
	got := resolveClaudeCodeModelFromJSONL(home, project, sess)
	if got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestClaudeCreateLinks_ErrorPropagationFromMkdir uses a blocker file to
// force an mkdir error inside prepareLinks (rulesDir path).
func TestClaudeCreateLinks_RulesMkdirBlocked(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// Place a regular file where the rules dir should live.
	claudeDir := filepath.Join(repo, ".claude")
	if err := os.WriteFile(claudeDir, []byte("blocker"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := NewClaude().CreateLinks("proj", repo); err == nil {
		t.Error("expected mkdir error")
	}
}

// TestClaudeScanSessionTokens_SkipsNonAssistantLines exercises the
// substring-prefilter "continue" branch in claudeScanSessionTokens
// (lines without `"assistant"` are short-circuited before JSON parse).
func TestClaudeScanSessionTokens_SkipsNonAssistantLines(t *testing.T) {
	home := t.TempDir()
	project := "/repo/skip"
	sess := "44444444-4444-4444-4444-444444444444"
	lines := []string{
		// No "assistant" substring — hits the continue branch.
		`{"type":"user","timestamp":"2026-05-11T12:00:00Z","message":{"content":"hi"}}`,
		// One real assistant line so the function does some work.
		`{"type":"assistant","timestamp":"2026-05-11T12:30:00Z","message":{"usage":{"input_tokens":7,"output_tokens":3}}}`,
	}
	writeClaudeProjectJSONL(t, home, project, sess, lines)

	got := claudeScanSessionTokens(home, project, sess, "")
	if got.MessageCount != 1 {
		t.Errorf("MessageCount = %d, want 1 (non-assistant line skipped)", got.MessageCount)
	}
	if got.InputTokens != 7 {
		t.Errorf("InputTokens = %d, want 7", got.InputTokens)
	}
	if got.OutputTokens != 3 {
		t.Errorf("OutputTokens = %d, want 3", got.OutputTokens)
	}
}

// TestClaudeScanJSONLForBranch_MissingFile drives the os.Open failure
// short-circuit (path does not exist → nil).
func TestClaudeScanJSONLForBranch_MissingFile(t *testing.T) {
	got := claudeScanJSONLForBranch(filepath.Join(t.TempDir(), "missing.jsonl"), `"gitBranch":"main"`, "main")
	if got != nil {
		t.Errorf("expected nil for missing file, got %+v", got)
	}
}

// TestClaudeScanJSONLForBranch_LineCap50 drives the >50-line break branch.
// We write 60 matching lines; only the first 50 should be visited.
func TestClaudeScanJSONLForBranch_LineCap50(t *testing.T) {
	home := t.TempDir()
	project := "/repo/cap"
	sess := "55555555-5555-5555-5555-555555555555"
	target := "main"
	line := `{"type":"assistant","sessionId":"` + sess + `","timestamp":"2026-05-11T11:30:00Z","gitBranch":"` + target + `","message":{"content":[{"type":"text","text":"x"}]}}`
	var lines []string
	for i := 0; i < 60; i++ {
		lines = append(lines, line)
	}
	writeClaudeProjectJSONL(t, home, project, sess, lines)

	slug := strings.ReplaceAll(project, "/", "-")
	path := filepath.Join(home, ".claude", "projects", slug, sess+".jsonl")
	marker := `"gitBranch":"` + target + `"`
	got := claudeScanJSONLForBranch(path, marker, target)
	if got == nil {
		t.Fatalf("expected match, got nil")
	}
	// Cap is 50 — anything more means the break branch did not engage.
	if got.MessageCount > 50 {
		t.Errorf("MessageCount = %d, want <= 50 (cap)", got.MessageCount)
	}
}

// ---------------------------------------------------------------------------
// Claude session resolvers + model resolution + usage stats + rule prune +
// remove-links sweep + claude agent-dir detection coverage (relocated from
// coverage_gap_test.go).
// ---------------------------------------------------------------------------

// TestClaudeFindSessionsOnBranch_MatchesMostRecent seeds two JSONL session
// files under ~/.claude/projects/<slug>/ and checks the resolver returns the
// matching session.
func TestClaudeFindSessionsOnBranch_MatchesMostRecent(t *testing.T) {
	home := t.TempDir()
	project := "/repo/example"
	branch := "feature/branch-x"

	good := `{"type":"assistant","sessionId":"sess-good","timestamp":"2026-05-11T11:30:00Z","gitBranch":"feature/branch-x"}`
	writeClaudeProjectJSONL(t, home, project, "sess-good", []string{good})

	stale := `{"type":"assistant","sessionId":"sess-stale","timestamp":"2026-05-09T11:30:00Z","gitBranch":"other"}`
	writeClaudeProjectJSONL(t, home, project, "sess-stale", []string{stale})

	got := claudeFindSessionsOnBranch(home, project, branch, 5)
	if len(got) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(got))
	}
	if got[0].SessionID != "sess-good" {
		t.Errorf("SessionID = %q, want sess-good", got[0].SessionID)
	}
	// Same call via the platform method.
	got2 := NewClaude().(interface {
		FindSessionsOnBranch(string, string, string, int) []BranchSessionInfo
	}).FindSessionsOnBranch(home, project, branch, 5)
	if len(got2) != 1 || got2[0].SessionID != "sess-good" {
		t.Errorf("Platform.FindSessionsOnBranch returned %+v", got2)
	}
}

func TestClaudeFindSessionsOnBranch_NoProjectsDir(t *testing.T) {
	if got := claudeFindSessionsOnBranch(t.TempDir(), "/no/where", "main", 5); got != nil {
		t.Errorf("expected nil for missing projects dir, got %+v", got)
	}
}

// TestResolveClaudeCodeModelFromJSONL parses a synthetic claude session JSONL.
func TestResolveClaudeCodeModelFromJSONL(t *testing.T) {
	home := t.TempDir()
	project := "/repo/example"
	sess := "claude-sess"
	lines := []string{
		`{"type":"user","message":{"content":"hi"}}`,
		`{"type":"assistant","message":{"model":"claude-3-5","content":[]}}`,
		`{"type":"assistant","message":{"model":"claude-3-7","content":[]}}`,
		`not-json`,
	}
	writeClaudeProjectJSONL(t, home, project, sess, lines)
	got := resolveClaudeCodeModelFromJSONL(home, project, sess)
	if got != "claude-3-7" {
		t.Errorf("model = %q, want claude-3-7", got)
	}
}

func TestResolveClaudeCodeModelFromJSONL_MissingFile(t *testing.T) {
	if got := resolveClaudeCodeModelFromJSONL(t.TempDir(), "/none", "x"); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

// TestClaudeRemoveAndRecreateRules ensures the project-rules prune path covers
// the leftover-rule branch.
func TestClaudeRulePruneRemovesLeftover(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// Pre-seed a stale rules file.
	rulesDir := filepath.Join(repo, ".claude", "rules")
	stale := filepath.Join(rulesDir, "proj--ancient.md")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	// Seed one current rule for the project.
	live := filepath.Join(agentsHome, "rules", "proj", "current.md")
	if err := os.MkdirAll(filepath.Dir(live), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(live, []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewClaude().(*claude)
	if err := c.createRulesLinks("proj", repo, agentsHome); err != nil {
		t.Fatalf("createRulesLinks: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale rule should be pruned, stat err=%v", err)
	}
	want := filepath.Join(rulesDir, "proj--current.md")
	if _, err := os.Lstat(want); err != nil {
		t.Errorf("expected live rule at %s: %v", want, err)
	}
}

// TestClaudePruneRuleLinksWithoutSource: when project rules dir is missing,
// pruneProjectRuleLinks should still clean stray entries.
func TestClaudePruneRuleLinksWithoutSource(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	rulesDir := filepath.Join(repo, ".claude", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(rulesDir, "proj--ghost.md")
	if err := os.WriteFile(stale, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewClaude().(*claude)
	if err := c.createRulesLinks("proj", repo, agentsHome); err != nil {
		t.Fatalf("createRulesLinks: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale rule should be pruned even with missing source")
	}
}

// TestClaudeReadUsageStats_TooManyDailyEntries triggers the tail-trim branch.
func TestClaudeReadUsageStats_TooManyDailyEntries(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	sb.WriteString(`{"totalSessions":1,"totalMessages":2,"modelUsage":{},"dailyActivity":[`)
	for i := 0; i < 15; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"date":"d` + itoa(i) + `","messageCount":1,"sessionCount":1,"toolCallCount":1}`)
	}
	sb.WriteString(`]}`)
	if err := os.WriteFile(filepath.Join(dir, "stats-cache.json"), []byte(sb.String()), 0644); err != nil {
		t.Fatal(err)
	}
	stats := claudeReadUsageStats(tmp)
	if stats == nil {
		t.Fatal("nil stats")
	}
	if len(stats.DailyActivity) != 10 {
		t.Errorf("DailyActivity tail = %d, want 10", len(stats.DailyActivity))
	}
}

// TestClaudeRemoveLinksFullSweep drives the .claude / .agents remove paths.
func TestClaudeRemoveLinksFullSweep(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// Seed a `.mcp.json` symlink under repo pointing inside agentsHome.
	mcpSrc := filepath.Join(agentsHome, "mcp", "proj", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(mcpSrc), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcpSrc, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	mcpDst := filepath.Join(repo, ".mcp.json")
	linktest.Link(t, mcpSrc, mcpDst)
	// Seed a stale rule symlink.
	ruleSrc := filepath.Join(agentsHome, "rules", "proj", "x.md")
	if err := os.MkdirAll(filepath.Dir(ruleSrc), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ruleSrc, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	ruleDst := filepath.Join(repo, ".claude", "rules", "proj--x.md")
	if err := os.MkdirAll(filepath.Dir(ruleDst), 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, ruleSrc, ruleDst)

	if err := NewClaude().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
	if _, err := os.Lstat(mcpDst); !os.IsNotExist(err) {
		t.Error(".mcp.json symlink should be removed")
	}
	if _, err := os.Lstat(ruleDst); !os.IsNotExist(err) {
		t.Error("project rule symlink should be removed")
	}
}

// TestIsClaudeAgentDir covers the directory-with-AGENT.md check.
func TestIsClaudeAgentDir(t *testing.T) {
	tmp := t.TempDir()
	good := filepath.Join(tmp, "agent")
	if err := os.MkdirAll(good, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(good, "AGENT.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if !isClaudeAgentDir(good) {
		t.Error("expected true for dir with AGENT.md")
	}
	noMarker := filepath.Join(tmp, "no-marker")
	if err := os.MkdirAll(noMarker, 0755); err != nil {
		t.Fatal(err)
	}
	if isClaudeAgentDir(noMarker) {
		t.Error("expected false for dir without AGENT.md")
	}
	// Non-directory path.
	notDir := filepath.Join(tmp, "regular")
	if err := os.WriteFile(notDir, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if isClaudeAgentDir(notDir) {
		t.Error("expected false for non-directory")
	}
}

// ---------- P3: Badge + CountLinks (StatusBadger + LinkCounter) ----------

// TestClaudeBadge_EmptyProject pins the empty-project contract: no
// .claude tree means Present=false, Broken=false.
func TestClaudeBadge_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	got := NewClaude().(*claude).Badge("proj", tmp, filepath.Join(tmp, ".agents"))
	if got.Name != "Claude" {
		t.Errorf("Badge.Name = %q, want %q", got.Name, "Claude")
	}
	if got.Present || got.Broken {
		t.Errorf("empty project: Badge = %+v, want Present=false Broken=false", got)
	}
}

// TestClaudeCountLinks_BrokenRuleSymlink covers the broken-link branch:
// a dangling .claude/rules symlink surfaces as (ok=0, broken=1) and
// Badge.Broken=true.
func TestClaudeCountLinks_BrokenRuleSymlink(t *testing.T) {
	testutil.SymlinkOrSkip(t)

	tmp := t.TempDir()
	rulesDir := filepath.Join(tmp, ".claude", "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(tmp, "gone.md"), filepath.Join(rulesDir, "missing.md")); err != nil {
		t.Fatal(err)
	}

	c := NewClaude().(*claude)
	ok, broken := c.CountLinks("proj", tmp, filepath.Join(tmp, ".agents"))
	if ok != 0 || broken != 1 {
		t.Errorf("CountLinks = (%d,%d), want (0,1)", ok, broken)
	}
	b := c.Badge("proj", tmp, filepath.Join(tmp, ".agents"))
	if b.Present || !b.Broken {
		t.Errorf("Badge = %+v, want Present=false Broken=true", b)
	}
}

// TestClaudeCountLinks_HealthyManagedFile covers the positive single-file
// branch: a resolvable .mcp.json symlink to an existing target counts as
// (ok=1, broken=0) and Badge surfaces Present=true.
func TestClaudeCountLinks_HealthyManagedFile(t *testing.T) {
	testutil.SymlinkOrSkip(t)

	tmp := t.TempDir()
	target := filepath.Join(tmp, "real-mcp.json")
	if err := os.WriteFile(target, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(tmp, ".mcp.json")); err != nil {
		t.Fatal(err)
	}

	c := NewClaude().(*claude)
	ok, broken := c.CountLinks("proj", tmp, filepath.Join(tmp, ".agents"))
	if ok != 1 || broken != 0 {
		t.Errorf("CountLinks = (%d,%d), want (1,0)", ok, broken)
	}
	b := c.Badge("proj", tmp, filepath.Join(tmp, ".agents"))
	if !b.Present || b.Broken {
		t.Errorf("Badge = %+v, want Present=true Broken=false", b)
	}
}

// TestHasMultipleHardLinks_PlatformPkg exercises the canonical
// hasMultipleHardLinks helper (and its HasMultipleHardLinks public
// wrapper) directly from the platform package. The lifecycle-side
// coverage of the same helper still exists (status_exports_test.go), but
// the platform-package coverage profile only attributes hits to a file
// when the test lives in that file's package — hence this local copy.
// Build-tagged so the unix file is covered on POSIX runners and the
// windows file picks up coverage on the Windows runner (the merged
// multi-OS coverage profile fuses both).
func TestHasMultipleHardLinks_PlatformPkg(t *testing.T) {
	tmp := t.TempDir()

	// Absent path → false.
	if HasMultipleHardLinks(filepath.Join(tmp, "ghost")) {
		t.Error("HasMultipleHardLinks(absent) = true, want false")
	}

	// Regular file → false.
	regular := filepath.Join(tmp, "regular.txt")
	if err := os.WriteFile(regular, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if HasMultipleHardLinks(regular) {
		t.Error("HasMultipleHardLinks(single-link) = true, want false")
	}

	// Hard-link best-effort: skip if the runner's filesystem rejects.
	linked := filepath.Join(tmp, "linked.txt")
	if err := os.Link(regular, linked); err != nil {
		t.Logf("os.Link unsupported on this filesystem (%v); skipping multi-link assertion", err)
		return
	}
	if !HasMultipleHardLinks(regular) {
		t.Error("HasMultipleHardLinks(2-link) = false, want true")
	}
	if !HasMultipleHardLinks(linked) {
		t.Error("HasMultipleHardLinks(2-link via alias) = false, want true")
	}
}

// TestClaudeCountRules_ManagedFileBranch hits the hard-link (Windows
// reparse-point-free) branch of claudeCountRules: a regular file in
// .claude/rules whose link count > 1 is treated as a Windows managed
// rule and counted ok. Hard-link creation is best-effort: skip the
// assertion on filesystems that reject os.Link.
func TestClaudeCountRules_ManagedFileBranch(t *testing.T) {
	tmp := t.TempDir()
	rulesDir := filepath.Join(tmp, "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(tmp, "src.md")
	if err := os.WriteFile(source, []byte("rule"), 0644); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(rulesDir, "alias.md")
	if err := os.Link(source, managed); err != nil {
		t.Skipf("os.Link unsupported (%v); skipping", err)
	}
	ok, broken := claudeCountRules(rulesDir)
	if ok != 1 || broken != 0 {
		t.Errorf("managed hardlink: got (%d,%d), want (1,0)", ok, broken)
	}

	// Plain regular file (link count 1) → ignored entirely.
	plain := filepath.Join(rulesDir, "plain.md")
	if err := os.WriteFile(plain, []byte("plain"), 0644); err != nil {
		t.Fatal(err)
	}
	ok2, broken2 := claudeCountRules(rulesDir)
	if ok2 != 1 || broken2 != 0 {
		t.Errorf("with plain file present: got (%d,%d), want (1,0) (plain ignored)", ok2, broken2)
	}
}

// TestAddManagedFileCounts_BrokenSymlink covers the broken-link
// classification branch of addManagedFileCounts.
func TestAddManagedFileCounts_BrokenSymlink(t *testing.T) {
	testutil.SymlinkOrSkip(t)

	tmp := t.TempDir()
	broken := filepath.Join(tmp, "broken.json")
	if err := os.Symlink(filepath.Join(tmp, "missing.json"), broken); err != nil {
		t.Fatal(err)
	}
	regular := filepath.Join(tmp, "real.txt")
	if err := os.WriteFile(regular, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	absent := filepath.Join(tmp, "ghost")

	ok, b := 0, 0
	addManagedFileCounts(&ok, &b, []string{absent, regular, broken})
	if ok != 1 || b != 1 {
		t.Errorf("addManagedFileCounts: got ok=%d broken=%d, want ok=1 broken=1 (absent skipped)", ok, b)
	}
}

// TestAddManagedDirCounts_MixedEntries exercises the directory walk: one
// healthy symlink, one broken symlink, one plain file. Expect ok=2 (plain
// classified as not-a-link → ok), broken=1.
func TestAddManagedDirCounts_MixedEntries(t *testing.T) {
	testutil.SymlinkOrSkip(t)

	tmp := t.TempDir()
	dir := filepath.Join(tmp, "dir")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(tmp, "real.md")
	if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "healthy")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(tmp, "vanished"), filepath.Join(dir, "broken")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plain.md"), []byte("p"), 0644); err != nil {
		t.Fatal(err)
	}

	// Absent dir is silently skipped.
	ok, br := 0, 0
	addManagedDirCounts(&ok, &br, []string{filepath.Join(tmp, "missing"), dir})
	if ok != 2 || br != 1 {
		t.Errorf("addManagedDirCounts: got ok=%d broken=%d, want ok=2 broken=1 (absent dir skipped)", ok, br)
	}
}

// ---------- BrokenLinkReporter implementation (P1) ----------

// TestClaudeBrokenLinks_EmptyProject is the absent-surface sentinel:
// nothing managed yet, no diagnostics. Matches the lifecycle-side empty-
// project contract.
func TestClaudeBrokenLinks_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}

	c := &claude{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("expected no broken links in empty project, got %d: %+v", len(got), got)
	}
}

// TestClaudeBrokenLinks_HealthySymlinkSkipped guards the rules-dir healthy
// branch: a symlink whose target exists is NOT reported.
func TestClaudeBrokenLinks_HealthySymlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")

	target := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("# rules"), 0644); err != nil {
		t.Fatal(err)
	}
	claudeRulesDir := filepath.Join(projectPath, claudeDir, "rules")
	if err := os.MkdirAll(claudeRulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, target, filepath.Join(claudeRulesDir, "proj--agents.md"))

	c := &claude{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("expected no broken links for healthy symlink, got %+v", got)
	}
}

// TestClaudeBrokenLinks_BrokenRuleSymlink is the central broken-rule case:
// a dangling symlink under .claude/rules must surface with
// PlatformID="claude".
func TestClaudeBrokenLinks_BrokenRuleSymlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")

	claudeRulesDir := filepath.Join(projectPath, claudeDir, "rules")
	if err := os.MkdirAll(claudeRulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.DanglingLink(t, filepath.Join(claudeRulesDir, "proj--ghost.md"))

	c := &claude{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 {
		t.Fatalf("expected 1 broken link, got %d: %+v", len(got), got)
	}
	if got[0].PlatformID != "claude" {
		t.Errorf("PlatformID = %q, want claude", got[0].PlatformID)
	}
	if got[0].LinkPath == "" || got[0].DisplayDest == "" {
		t.Errorf("LinkPath/DisplayDest unset: %+v", got[0])
	}
}

// TestClaudeBrokenLinks_BrokenMCPJSON exercises the single-file .mcp.json
// branch: a dangling .mcp.json at the repo root must surface as a claude
// broken-link entry. This is the entry that previously lived in
// doctor.go's projectSingleFiles table.
func TestClaudeBrokenLinks_BrokenMCPJSON(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.DanglingLink(t, filepath.Join(projectPath, ".mcp.json"))

	c := &claude{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 || got[0].PlatformID != "claude" {
		t.Fatalf("expected 1 claude broken link for .mcp.json, got %+v", got)
	}
}

// TestClaudeBrokenLinks_PlainMCPJSONIgnored guards the contract carried over
// from lifecycle's managedLinkBroken: a plain regular file at .mcp.json
// (not a managed link) is unmanaged user content and must NOT be reported.
func TestClaudeBrokenLinks_PlainMCPJSONIgnored(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectPath, ".mcp.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	c := &claude{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("plain .mcp.json must be ignored by broken-link reporter, got %+v", got)
	}
}

// TestClaudeBrokenLinks_HealthyRuleNotLink confirms that a non-link entry
// (regular file dropped into .claude/rules) is silently skipped — only
// resolvable managed links whose target is missing should be reported.
// This is the explicit "not a link" branch of classifyManagedLink.
func TestClaudeBrokenLinks_HealthyRuleNotLink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	claudeRulesDir := filepath.Join(projectPath, claudeDir, "rules")
	if err := os.MkdirAll(claudeRulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeRulesDir, "plain.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	c := &claude{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("plain file in .claude/rules must be ignored, got %+v", got)
	}
}

// TestClaudeBrokenLinks_InterfaceConformance pins compile-time conformance
// with BrokenLinkReporter so doctor.collectBrokenLinks's type assertion
// will not silently regress.
func TestClaudeBrokenLinks_InterfaceConformance(t *testing.T) {
	var _ BrokenLinkReporter = (*claude)(nil)
}

// ---------- OrphanCanonicalReporter implementation (P4) ----------

// TestClaudeOrphanCanonicals is the table-driven cover for claude's
// OrphanCanonicalReporter: it owns only the "skills" bucket and reports plain
// orphans (no back-link), mis-pointed orphans (back-link resolves elsewhere),
// while skipping correctly-linked entries and any non-owned bucket.
func TestClaudeOrphanCanonicals(t *testing.T) {
	tests := []struct {
		name      string
		bucket    string
		setup     func(t *testing.T, agentsHome, projectPath string) (wantName string, wantNote bool)
		wantCount int
	}{
		{
			name:   "plain orphan in owned skills bucket",
			bucket: "skills",
			setup: func(t *testing.T, agentsHome, projectPath string) (string, bool) {
				mkdirAllT(t, filepath.Join(agentsHome, "skills", "proj", "alpha"))
				return "alpha", false
			},
			wantCount: 1,
		},
		{
			name:   "correctly-linked back-link not orphaned",
			bucket: "skills",
			setup: func(t *testing.T, agentsHome, projectPath string) (string, bool) {
				canonical := filepath.Join(agentsHome, "skills", "proj", "beta")
				mkdirAllT(t, canonical)
				repoLocal := filepath.Join(projectPath, ".agents", "skills")
				mkdirAllT(t, repoLocal)
				linktest.Link(t, canonical, filepath.Join(repoLocal, "beta"))
				return "", false
			},
			wantCount: 0,
		},
		{
			name:   "mis-pointed back-link is orphan with note",
			bucket: "skills",
			setup: func(t *testing.T, agentsHome, projectPath string) (string, bool) {
				mkdirAllT(t, filepath.Join(agentsHome, "skills", "proj", "gamma"))
				other := filepath.Join(agentsHome, "skills", "otherproj", "delta")
				mkdirAllT(t, other)
				repoLocal := filepath.Join(projectPath, ".agents", "skills")
				mkdirAllT(t, repoLocal)
				linktest.Link(t, other, filepath.Join(repoLocal, "gamma"))
				return "gamma", true
			},
			wantCount: 1,
		},
		{
			name:   "agents bucket not owned by claude",
			bucket: "agents",
			setup: func(t *testing.T, agentsHome, projectPath string) (string, bool) {
				mkdirAllT(t, filepath.Join(agentsHome, "agents", "proj", "orphan-agent"))
				return "", false
			},
			wantCount: 0,
		},
		{
			name:   "absent canonical bucket yields nothing",
			bucket: "skills",
			setup: func(t *testing.T, agentsHome, projectPath string) (string, bool) {
				return "", false
			},
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			agentsHome := filepath.Join(tmp, ".agents")
			projectPath := filepath.Join(tmp, "proj")
			mkdirAllT(t, projectPath)
			wantName, wantNote := tc.setup(t, agentsHome, projectPath)

			c := &claude{io: stdPlatformIO{}}
			got := c.OrphanCanonicals("proj", projectPath, agentsHome, tc.bucket)
			if len(got) != tc.wantCount {
				t.Fatalf("OrphanCanonicals(%q) = %d entries %+v, want %d", tc.bucket, len(got), got, tc.wantCount)
			}
			if tc.wantCount == 0 {
				return
			}
			if got[0].Name != wantName {
				t.Errorf("orphan Name = %q, want %q", got[0].Name, wantName)
			}
			if wantNote && !strings.Contains(got[0].DisplayNote, "mis-pointed") {
				t.Errorf("expected mis-pointed DisplayNote, got %q", got[0].DisplayNote)
			}
			if !wantNote && got[0].DisplayNote != "" {
				t.Errorf("expected empty DisplayNote for plain orphan, got %q", got[0].DisplayNote)
			}
		})
	}
}

// TestClaudeOrphanCanonicals_InterfaceConformance pins compile-time
// conformance with OrphanCanonicalReporter so doctor.collectOrphanCanonicals's
// type assertion cannot silently regress.
func TestClaudeOrphanCanonicals_InterfaceConformance(t *testing.T) {
	var _ OrphanCanonicalReporter = (*claude)(nil)
}

// ---------- UserConfigReporter implementation (P4) ----------

// TestClaudeUserBrokenLinks is the table-driven cover for claude's
// UserConfigReporter broken-link surface (CLAUDE.md, settings.json, agents/*,
// skills/*). Every reported link must carry PlatformID="claude".
func TestClaudeUserBrokenLinks(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T, home string)
		wantCount int
	}{
		{
			name:      "empty home reports nothing",
			setup:     func(t *testing.T, home string) {},
			wantCount: 0,
		},
		{
			name: "broken CLAUDE.md",
			setup: func(t *testing.T, home string) {
				linktest.DanglingLink(t, filepath.Join(home, ".claude", "CLAUDE.md"))
			},
			wantCount: 1,
		},
		{
			name: "broken settings.json",
			setup: func(t *testing.T, home string) {
				linktest.DanglingLink(t, filepath.Join(home, ".claude", "settings.json"))
			},
			wantCount: 1,
		},
		{
			name: "broken agents dir entry",
			setup: func(t *testing.T, home string) {
				linktest.DanglingLink(t, filepath.Join(home, ".claude", "agents", "ghost.md"))
			},
			wantCount: 1,
		},
		{
			name: "broken skills dir entry",
			setup: func(t *testing.T, home string) {
				linktest.DanglingLink(t, filepath.Join(home, ".claude", "skills", "ghost"))
			},
			wantCount: 1,
		},
		{
			name: "healthy CLAUDE.md symlink ignored",
			setup: func(t *testing.T, home string) {
				target := filepath.Join(home, ".agents", "rules", "global", "claude-code.md")
				mkdirAllT(t, filepath.Dir(target))
				if err := os.WriteFile(target, []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
				linktest.Link(t, target, filepath.Join(home, ".claude", "CLAUDE.md"))
			},
			wantCount: 0,
		},
		{
			name: "plain CLAUDE.md ignored",
			setup: func(t *testing.T, home string) {
				claudeHome := filepath.Join(home, ".claude")
				mkdirAllT(t, claudeHome)
				if err := os.WriteFile(filepath.Join(claudeHome, "CLAUDE.md"), []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			tc.setup(t, home)

			c := &claude{io: stdPlatformIO{}}
			got := c.UserBrokenLinks(home)
			if len(got) != tc.wantCount {
				t.Fatalf("UserBrokenLinks = %d %+v, want %d", len(got), got, tc.wantCount)
			}
			for _, bl := range got {
				if bl.PlatformID != "claude" {
					t.Errorf("PlatformID = %q, want claude", bl.PlatformID)
				}
				if bl.LinkPath == "" || bl.DisplayDest == "" {
					t.Errorf("LinkPath/DisplayDest unset: %+v", bl)
				}
			}
		})
	}
}

// TestClaudeUserBadge is the table-driven cover for claude's UserBadge:
// Present reflects any managed user-config presence and Broken reflects any
// dangling managed link.
func TestClaudeUserBadge(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, home string)
		wantPresent bool
		wantBroken  bool
	}{
		{
			name:        "empty home: absent badge",
			setup:       func(t *testing.T, home string) {},
			wantPresent: false,
			wantBroken:  false,
		},
		{
			name: "present healthy CLAUDE.md",
			setup: func(t *testing.T, home string) {
				claudeHome := filepath.Join(home, ".claude")
				mkdirAllT(t, claudeHome)
				if err := os.WriteFile(filepath.Join(claudeHome, "CLAUDE.md"), []byte("x"), 0644); err != nil {
					t.Fatal(err)
				}
			},
			wantPresent: true,
			wantBroken:  false,
		},
		{
			name: "broken settings surfaces broken badge",
			setup: func(t *testing.T, home string) {
				linktest.DanglingLink(t, filepath.Join(home, ".claude", "settings.json"))
			},
			wantPresent: false,
			wantBroken:  true,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			tc.setup(t, home)

			c := &claude{io: stdPlatformIO{}}
			got := c.UserBadge(home)
			if got.Name != "Claude" {
				t.Errorf("UserBadge.Name = %q, want Claude", got.Name)
			}
			if got.Present != tc.wantPresent || got.Broken != tc.wantBroken {
				t.Errorf("UserBadge = %+v, want Present=%v Broken=%v", got, tc.wantPresent, tc.wantBroken)
			}
		})
	}
}

// TestClaudeUserConfig_InterfaceConformance pins compile-time conformance with
// UserConfigReporter for the claude platform.
func TestClaudeUserConfig_InterfaceConformance(t *testing.T) {
	var _ UserConfigReporter = (*claude)(nil)
}

// mkdirAllT is a small test helper that os.MkdirAll's path and fails the test
// on error, keeping the table-driven setup closures terse.
func mkdirAllT(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
}
