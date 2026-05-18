package workflow

// Tests in this file cover iter_log.go branches that exercise Claude Code
// session-JSONL machinery. They run deterministically by seeding HOME with a
// synthetic .claude/projects/<slug>/<session>.jsonl and the matching session
// env vars. On bare CI runners none of these env vars are set and no real
// JSONL files exist, so the branches stay dark without this scaffolding.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// seedClaudeSessionJSONL drops a minimal Claude Code session JSONL under
// home/.claude/projects/<slug>/<sess>.jsonl. slug derivation mirrors
// claudeProjectDirSlug in internal/platform (project path with "/" → "-").
func seedClaudeSessionJSONL(t *testing.T, home, projectPath, sess string, lines []string) string {
	t.Helper()
	slug := strings.ReplaceAll(projectPath, "/", "-")
	dir := filepath.Join(home, ".claude", "projects", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, sess+".jsonl")
	content := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestResolveAgentBlock_WithClaudeSessionEnvCoversModelResolve covers the
// iter_log.go:428-430 branch (reader != nil && sessionID != "" && home != ""
// → block.Model = reader.ResolveModel(...)). It seeds HOME and the
// CLAUDE_CODE_SESSION_ID env var so probeSessionReader returns a non-nil
// claude reader, and writes a JSONL entry that resolveClaudeCodeModelFromJSONL
// will accept so block.Model is populated.
func TestResolveAgentBlock_WithClaudeSessionEnvCoversModelResolve(t *testing.T) {
	clearAgentSessionEnv(t)
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	project := "/repo/example"
	sess := "ci-drift-resolve-aaaa-bbbb-ccccdddd"

	// A single assistant turn whose model field is what ResolveModel returns.
	line := `{"type":"assistant","sessionId":"` + sess + `","timestamp":"2026-05-15T12:00:00Z","message":{"model":"claude-sonnet-4-6","content":[{"type":"text","text":"hi"}]}}`
	seedClaudeSessionJSONL(t, tmpHome, project, sess, []string{line})

	t.Setenv("CLAUDE_CODE_SESSION_ID", sess)

	block := resolveAgentBlock(project)
	if block == nil {
		t.Fatal("expected non-nil agent block with seeded session env + JSONL")
	}
	if block.SessionID != sess {
		t.Errorf("SessionID = %q, want %q", block.SessionID, sess)
	}
	if block.Harness != "claude-code" {
		t.Errorf("Harness = %q, want claude-code", block.Harness)
	}
	// Block.Model is set via reader.ResolveModel — we don't pin the value
	// (resolve heuristics vary), but covering the assignment branch is the
	// objective here.
}

// TestPopulateIterLogSessionTokens_PopulatesFromSeededJSONL covers
// iter_log.go:627-647 (the populateIterLogSessionTokens body). On CI nothing
// is set up to make this loop emit metrics. Here we seed HOME with a JSONL
// that has token usage so ScanSessionTokens returns MessageCount > 0, then
// assert the SessionTokens block is populated on the entry.
func TestPopulateIterLogSessionTokens_PopulatesFromSeededJSONL(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)

	project := "/repo/example-iterlog"
	sess := "ci-drift-tokens-1111-2222-3333"

	// One assistant entry inside the post-prevAt window with usage.
	line := `{"type":"assistant","timestamp":"2026-05-15T12:00:00Z","message":{"usage":{"input_tokens":10,"output_tokens":20,"cache_read_input_tokens":30,"cache_creation_input_tokens":40}}}`
	seedClaudeSessionJSONL(t, tmpHome, project, sess, []string{line})

	entry := &iterLogEntry{
		Agent: &iterLogAgentBlock{
			SessionID: sess,
			Harness:   "claude-code",
		},
	}

	// iterDir doesn't need to contain anything; loadPrevCheckpointAt returns
	// "" when iter-(n-1).yaml is missing, which is fine — every entry in our
	// seeded JSONL is "after" "".
	iterDir := t.TempDir()
	populateIterLogSessionTokens(entry, project, iterDir, 1)

	if entry.SessionTokens == nil {
		t.Fatal("expected SessionTokens to be populated after seeded JSONL scan")
	}
	if entry.SessionTokens.MessageCount == 0 {
		t.Error("expected MessageCount > 0")
	}
	if entry.SessionTokens.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", entry.SessionTokens.InputTokens)
	}
}

// TestPopulateIterLogSessionTokens_NoMatchingReaderLeavesNil covers the
// continue-when-prefix-mismatches branch (iter_log.go:632 → continue). When
// the Agent.Harness does not match any SessionReader's AIAgentPrefix, the
// SessionTokens block must remain nil.
func TestPopulateIterLogSessionTokens_NoMatchingReaderLeavesNil(t *testing.T) {
	entry := &iterLogEntry{
		Agent: &iterLogAgentBlock{
			SessionID: "anything",
			Harness:   "unknown-harness-prefix",
		},
	}
	populateIterLogSessionTokens(entry, "/repo/nowhere", t.TempDir(), 1)
	if entry.SessionTokens != nil {
		t.Errorf("expected SessionTokens to remain nil for unmatched harness, got %+v", entry.SessionTokens)
	}
}
