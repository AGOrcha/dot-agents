package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/linktest"
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

// ---------- P3: Badge + CountLinks (StatusBadger + LinkCounter) ----------

// TestCopilotBadge_EmptyProject pins the empty-project contract.
func TestCopilotBadge_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	got := NewCopilot().(*copilot).Badge("proj", tmp, filepath.Join(tmp, ".agents"))
	if got.Name != "Copilot" {
		t.Errorf("Badge.Name = %q, want %q", got.Name, "Copilot")
	}
	if got.Present || got.Broken {
		t.Errorf("empty project: Badge = %+v, want Present=false Broken=false", got)
	}
}

// TestCopilotCountLinks_HealthyInstructionsFile covers the positive
// single-file branch: a present .github/copilot-instructions.md counts as
// healthy and Badge surfaces Present=true.
func TestCopilotCountLinks_HealthyInstructionsFile(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, ".github")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "copilot-instructions.md"), []byte("# x"), 0644); err != nil {
		t.Fatal(err)
	}

	c := NewCopilot().(*copilot)
	ok, broken := c.CountLinks("proj", tmp, filepath.Join(tmp, ".agents"))
	if ok < 1 || broken != 0 {
		t.Errorf("CountLinks = (%d,%d), want (>=1,0)", ok, broken)
	}
	b := c.Badge("proj", tmp, filepath.Join(tmp, ".agents"))
	if !b.Present || b.Broken {
		t.Errorf("Badge = %+v, want Present=true Broken=false", b)
	}
}

// ---------- BrokenLinkReporter implementation (P2) ----------

// TestCopilotBrokenLinks_EmptyProject is the absent-surface sentinel:
// neither .github/copilot-instructions.md nor .vscode/mcp.json present
// means no diagnostics.
func TestCopilotBrokenLinks_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}

	c := &copilot{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("expected no broken links in empty project, got %d: %+v", len(got), got)
	}
}

// TestCopilotBrokenLinks_BrokenInstructions migrates doctor's
// TestCollectBrokenLinks_BrokenCopilotInstructions: a dangling
// .github/copilot-instructions.md must surface as copilot broken-link.
func TestCopilotBrokenLinks_BrokenInstructions(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	ghDir := filepath.Join(projectPath, copilotGitHubDir)
	if err := os.MkdirAll(ghDir, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.DanglingLink(t, filepath.Join(ghDir, copilotInstructionsMD))

	c := &copilot{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 || got[0].PlatformID != "copilot" {
		t.Fatalf("expected 1 copilot broken link, got %+v", got)
	}
	if got[0].LinkPath == "" || got[0].DisplayDest == "" {
		t.Errorf("LinkPath/DisplayDest unset: %+v", got[0])
	}
}

// TestCopilotBrokenLinks_BrokenVSCodeMCP migrates doctor's
// TestCollectBrokenLinks_BrokenVSCodeMCP: a dangling .vscode/mcp.json
// must surface as a copilot broken-link.
func TestCopilotBrokenLinks_BrokenVSCodeMCP(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	vsDir := filepath.Join(projectPath, copilotVSCodeDir)
	if err := os.MkdirAll(vsDir, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.DanglingLink(t, filepath.Join(vsDir, copilotMCPJSON))

	c := &copilot{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 || got[0].PlatformID != "copilot" {
		t.Fatalf("expected 1 copilot mcp broken link, got %+v", got)
	}
}

// TestCopilotBrokenLinks_BothBroken exercises the multi-entry path: both
// owned single-file links broken simultaneously must produce two records,
// each carrying PlatformID="copilot". This is the case the previous
// projectSingleFiles table covered with two consecutive copilot entries.
func TestCopilotBrokenLinks_BothBroken(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	ghDir := filepath.Join(projectPath, copilotGitHubDir)
	vsDir := filepath.Join(projectPath, copilotVSCodeDir)
	if err := os.MkdirAll(ghDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(vsDir, 0755); err != nil {
		t.Fatal(err)
	}
	linktest.DanglingLink(t, filepath.Join(ghDir, copilotInstructionsMD))
	linktest.DanglingLink(t, filepath.Join(vsDir, copilotMCPJSON))

	c := &copilot{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 2 {
		t.Fatalf("expected 2 broken links, got %d: %+v", len(got), got)
	}
	for _, bl := range got {
		if bl.PlatformID != "copilot" {
			t.Errorf("PlatformID = %q, want copilot", bl.PlatformID)
		}
	}
}

// TestCopilotBrokenLinks_PlainFilesIgnored guards the contract carried over
// from lifecycle's managedLinkBroken: plain regular files at the owned paths
// (not managed links) are unmanaged user content and must NOT be reported.
func TestCopilotBrokenLinks_PlainFilesIgnored(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	ghDir := filepath.Join(projectPath, copilotGitHubDir)
	vsDir := filepath.Join(projectPath, copilotVSCodeDir)
	if err := os.MkdirAll(ghDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(vsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ghDir, copilotInstructionsMD), []byte("plain"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vsDir, copilotMCPJSON), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	c := &copilot{io: stdPlatformIO{}}
	got := c.BrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("plain files must be ignored, got %+v", got)
	}
}

// TestCopilotBrokenLinks_InterfaceConformance pins compile-time conformance.
func TestCopilotBrokenLinks_InterfaceConformance(t *testing.T) {
	var _ BrokenLinkReporter = (*copilot)(nil)
}

// TestCopilotUserConfig_EmptyUntilWired pins copilot's UserConfigReporter
// behavior: copilot's documented user-config layer (~/.copilot/*) is not yet
// wired by dot-agents, so it reports no managed user-home links and an
// absent/clean badge — which appendPlatformIfPresent filters out of the status
// list. This is intentional, not "no user-config layer": the interface is
// implemented so copilot opts into the diagnostics path the moment user-scope
// wiring lands.
func TestCopilotUserConfig_EmptyUntilWired(t *testing.T) {
	var _ UserConfigReporter = (*copilot)(nil)

	home := t.TempDir()
	c := &copilot{io: stdPlatformIO{}}

	if got := c.UserBrokenLinks(home); got != nil {
		t.Errorf("UserBrokenLinks = %+v, want nil (no copilot user-scope wiring yet)", got)
	}
	badge := c.UserBadge(home)
	if badge.Name != "Copilot" {
		t.Errorf("UserBadge.Name = %q, want Copilot", badge.Name)
	}
	if badge.Present || badge.Broken {
		t.Errorf("UserBadge = %+v, want absent/clean (Present=false Broken=false)", badge)
	}
}
