package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCopilotSharedTargetIntentsPopulated drives the skills+agents combination.
func TestCopilotSharedTargetIntents_Populated(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	for _, p := range [][]string{
		{"skills", "proj", "alpha", "SKILL.md"},
		{"agents", "proj", "reviewer", "AGENT.md"},
	} {
		dir := filepath.Join(append([]string{agentsHome}, p[:3]...)...)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, p[3]), []byte("body"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	intents, err := NewCopilot().SharedTargetIntents("proj")
	if err != nil {
		t.Fatalf("SharedTargetIntents: %v", err)
	}
	if len(intents) == 0 {
		t.Error("expected non-zero intents")
	}
}

// TestCopilotScanSessionTokens_MtimeFilter exercises the time filter for
// session-state directories.
func TestCopilotScanSessionTokens_MtimeFilter(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".copilot", "session-state", "abc")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	events := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(events, []byte(`{"type":"session.shutdown","data":{"modelMetrics":{"x":{"usage":{"inputTokens":1}}}}}`+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Future cutoff → filtered out.
	got := copilotScanSessionTokens(home, "2099-01-01T00:00:00Z")
	if got.InputTokens != 0 {
		t.Errorf("expected filter to skip, got %+v", got)
	}
}

// TestRenderCopilotHookFile_TimeoutClampMinimum exercises the timeout clamp
// branch when TimeoutMS / 1000 == 0.
func TestRenderCopilotHookFile_TimeoutClampMinimum(t *testing.T) {
	_, _, _, err := renderCopilotHookFile(HookSpec{
		Name:      "tiny",
		When:      "user_prompt_submit",
		Command:   "/bin/true",
		TimeoutMS: 500,
	})
	if err != nil {
		t.Errorf("renderCopilotHookFile clamp: %v", err)
	}
}

// TestCopilotCreateMCPLinks_NoSource drives the no-source early-return branch
// (relocated from coverage_gap5_test.go).
func TestCopilotCreateMCPLinks_NoSource(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	c := NewCopilot().(*copilot)
	if err := c.createMCPLinks("proj", repo, filepath.Join(tmp, ".agents")); err != nil {
		t.Errorf("createMCPLinks no source: %v", err)
	}
	// .vscode/mcp.json should NOT exist.
	if _, err := os.Lstat(filepath.Join(repo, ".vscode", "mcp.json")); !os.IsNotExist(err) {
		t.Error("expected no mcp.json")
	}
}
