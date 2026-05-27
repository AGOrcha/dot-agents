package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/linktest"
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

// ---------------------------------------------------------------------------
// Copilot legacy + canonical hook fanout + instructions src + RemoveLinks
// (relocated from coverage_gap_test.go).
// ---------------------------------------------------------------------------

// TestCopilotLegacyHookFanoutBuilds wires up a legacy `.json` hooks directory
// and asserts copilot's createProjectHookFiles emits fanout files.
func TestCopilotLegacyHookFanoutBuilds(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(agentsHome, "hooks", "proj", "session-banner.json")
	if err := os.MkdirAll(filepath.Dir(legacy), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte(`{"version":1,"hooks":{"sessionStart":[{"type":"command","bash":"x"}]}}`), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCopilot().(*copilot)
	if err := c.createProjectHookFiles("proj", repo, agentsHome); err != nil {
		t.Fatalf("createProjectHookFiles: %v", err)
	}
	out := filepath.Join(repo, ".github", "hooks", "session-banner.json")
	if _, err := os.Stat(out); err != nil {
		t.Errorf("expected fanout file at %s: %v", out, err)
	}

	// Removing should clear the fanout via legacy entries.
	if err := NewCopilot().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
}

// TestCopilotCanonicalHookFanout drives the canonical-bundle code path with
// HOOK.yaml under hooks/proj/<name>/.
func TestCopilotCanonicalHookFanout(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(agentsHome, "hooks", "global", "prompt-log", "HOOK.yaml")
	if err := os.MkdirAll(filepath.Dir(manifest), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, []byte("name: prompt-log\nwhen: user_prompt_submit\nrun:\n  command: /bin/echo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCopilot().(*copilot)
	if err := c.createProjectHookFiles("proj", repo, agentsHome); err != nil {
		t.Fatalf("createProjectHookFiles canonical: %v", err)
	}
	out := filepath.Join(repo, ".github", "hooks", "prompt-log.json")
	if _, err := os.Stat(out); err != nil {
		t.Errorf("expected canonical fanout at %s: %v", out, err)
	}
}

// TestCopilotResolveInstructionsSrcFallbackPath drives the rules.md fallback
// branch.
func TestCopilotResolveInstructionsSrcFallback(t *testing.T) {
	tmp := t.TempDir()
	rules := filepath.Join(tmp, "rules", "global")
	if err := os.MkdirAll(rules, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rules, "rules.md"), []byte("# rules\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewCopilot().(*copilot)
	got := c.resolveInstructionsSrc("proj", tmp)
	if !strings.HasSuffix(got, "rules.md") {
		t.Errorf("expected rules.md fallback, got %q", got)
	}

	// Missing → empty.
	if got := c.resolveInstructionsSrc("proj", filepath.Join(tmp, "no-such")); got != "" {
		t.Errorf("expected empty for missing rules, got %q", got)
	}
}

// TestCopilotResolveInstructionsSrcDirectCopilotInstructions covers the
// preferred (copilot-instructions.md) branch.
func TestCopilotResolveInstructionsSrcDirectFile(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "rules", "proj")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "copilot-instructions.md")
	if err := os.WriteFile(src, []byte("# copilot\n"), 0644); err != nil {
		t.Fatal(err)
	}
	c := NewCopilot().(*copilot)
	if got := c.resolveInstructionsSrc("proj", tmp); got != src {
		t.Errorf("got %q, want %q", got, src)
	}
}

// TestCopilotRemoveLinksFullSweep wires Copilot remove against a seeded
// shared-target layout to drive removeAgentLinks and removeHookLinks.
func TestCopilotRemoveLinksFullSweep(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	// Agent symlink.
	src := filepath.Join(agentsHome, "agents", "proj", "reviewer.agent.md")
	if err := os.MkdirAll(filepath.Dir(src), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(repo, ".github", "agents", "reviewer.agent.md")
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, src, dst)
	// Hooks dir with a stale entry.
	hookSrc := filepath.Join(agentsHome, "hooks", "proj", "abc.json")
	if err := os.MkdirAll(filepath.Dir(hookSrc), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hookSrc, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	hookDst := filepath.Join(repo, ".github", "hooks", "abc.json")
	if err := os.MkdirAll(filepath.Dir(hookDst), 0755); err != nil {
		t.Fatal(err)
	}
	linktest.Link(t, hookSrc, hookDst)

	if err := NewCopilot().RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
	if _, err := os.Lstat(dst); !os.IsNotExist(err) {
		t.Error("agent symlink should be removed")
	}
	if _, err := os.Lstat(hookDst); !os.IsNotExist(err) {
		t.Error("hook symlink should be removed")
	}
}
