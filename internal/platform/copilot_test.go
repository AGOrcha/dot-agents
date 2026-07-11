package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/linktest"
	"github.com/AGOrcha/dot-agents/internal/testutil"
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
	got, err := c.resolveInstructionsSrc("proj", tmp)
	if err != nil {
		t.Fatalf("resolveInstructionsSrc: %v", err)
	}
	if !strings.HasSuffix(got, "rules.md") {
		t.Errorf("expected rules.md fallback, got %q", got)
	}

	// Missing → empty, no error.
	if got, err := c.resolveInstructionsSrc("proj", filepath.Join(tmp, "no-such")); got != "" || err != nil {
		t.Errorf("expected (\"\", nil) for missing rules, got (%q, %v)", got, err)
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
	if got, err := c.resolveInstructionsSrc("proj", tmp); got != src || err != nil {
		t.Errorf("got (%q, %v), want (%q, nil)", got, err, src)
	}
}

// TestCopilotResolveInstructionsSrc_RealStatErrorPropagates covers the
// swallow fixed in se9-platform-shared: a permission-denied Stat on a
// candidate must abort the search with a wrapped error, never be silently
// read as "this candidate doesn't exist" and skipped to the next one.
func TestCopilotResolveInstructionsSrc_RealStatErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	projectRules := filepath.Join(tmp, "rules", "proj")
	mustMkdirAllT(t, projectRules)
	testutil.MakeDirUnreadable(t, projectRules)

	c := NewCopilot().(*copilot)
	got, err := c.resolveInstructionsSrc("proj", tmp)
	if err == nil {
		t.Fatalf("expected a real Stat error, got (%q, nil)", got)
	}
	if got != "" {
		t.Errorf("expected empty src alongside the error, got %q", got)
	}
}

// TestCopilotCreateLinks_UnreadableInstructionsSourceLeavesExistingLink is
// the se2-contract survival check: CreateLinks succeeds once with a real
// global instructions source, creating .github/copilot-instructions.md.
// Once that source directory becomes unreadable, a second CreateLinks call
// must abort with an error and leave the pre-existing managed link alone —
// never delete it because the resolver misread the permission error as
// "nothing to link".
func TestCopilotCreateLinks_UnreadableInstructionsSourceLeavesExistingLink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	repo := filepath.Join(tmp, "repo")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	mustMkdirAllT(t, home)
	mustMkdirAllT(t, repo)

	globalRules := filepath.Join(agentsHome, "rules", "global")
	mustMkdirAllT(t, globalRules)
	src := filepath.Join(globalRules, copilotInstructionsMD)
	if err := os.WriteFile(src, []byte("# instructions\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := NewCopilot().CreateLinks("proj", repo); err != nil {
		t.Fatalf("initial CreateLinks: %v", err)
	}
	dst := filepath.Join(repo, copilotGitHubDir, copilotInstructionsMD)
	if !links.IsManagedLink(dst, src) {
		t.Fatalf("expected %s to be a managed link to %s", dst, src)
	}

	testutil.MakeDirUnreadable(t, globalRules)
	if err := NewCopilot().CreateLinks("proj", repo); err == nil {
		t.Fatal("expected CreateLinks to abort once the instructions source is unreadable")
	}
	if !links.IsManagedLink(dst, src) {
		t.Errorf("existing managed link %s must survive the aborted sync", dst)
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

// ---------- UserConfigReporter implementation (P4b) ----------

// writeCopilotUserHook writes a plausible rendered copilot hook file under
// ~/.copilot/hooks/, mirroring the bytes createUserHomeHookFiles emits. Keeps
// the table-driven setup closures terse (low cognitive complexity).
func writeCopilotUserHook(t *testing.T, home, name string) {
	t.Helper()
	dir := copilotUserHooksDir(home)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"version":1,"hooks":{"sessionStart":[{"type":"command","bash":"x"}]}}`)
	if err := os.WriteFile(filepath.Join(dir, name), body, 0644); err != nil {
		t.Fatal(err)
	}
}

// TestCopilotUserBrokenLinks is the table-driven cover for copilot's
// UserConfigReporter broken-link surface (~/.copilot/hooks/*). Every reported
// link carries PlatformID="copilot"; a rendered file is silently skipped.
func TestCopilotUserBrokenLinks(t *testing.T) {
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
			name: "broken hook dir entry",
			setup: func(t *testing.T, home string) {
				linktest.DanglingLink(t, filepath.Join(copilotUserHooksDir(home), "ghost.json"))
			},
			wantCount: 1,
		},
		{
			name: "rendered hook file ignored",
			setup: func(t *testing.T, home string) {
				writeCopilotUserHook(t, home, "prompt-log.json")
			},
			wantCount: 0,
		},
		{
			name: "healthy hook symlink ignored",
			setup: func(t *testing.T, home string) {
				target := filepath.Join(home, ".agents", "hooks", "global", "prompt-log.json")
				mkdirAllT(t, filepath.Dir(target))
				if err := os.WriteFile(target, []byte("{}"), 0644); err != nil {
					t.Fatal(err)
				}
				linktest.Link(t, target, filepath.Join(copilotUserHooksDir(home), "prompt-log.json"))
			},
			wantCount: 0,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			tc.setup(t, home)

			c := &copilot{io: stdPlatformIO{}}
			got := c.UserBrokenLinks(home)
			assertUserBrokenLinks(t, "copilot", got, tc.wantCount)
		})
	}
}

// TestCopilotUserBadge covers the copilot user-config badge over
// ~/.copilot/hooks/: Present reflects any managed rendered hook and Broken
// reflects any dangling managed link.
func TestCopilotUserBadge(t *testing.T) {
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
			name: "present rendered hook",
			setup: func(t *testing.T, home string) {
				writeCopilotUserHook(t, home, "prompt-log.json")
			},
			wantPresent: true,
			wantBroken:  false,
		},
		{
			name: "broken hook surfaces broken badge",
			setup: func(t *testing.T, home string) {
				linktest.DanglingLink(t, filepath.Join(copilotUserHooksDir(home), "ghost.json"))
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

			c := &copilot{io: stdPlatformIO{}}
			got := c.UserBadge(home)
			if got.Name != "Copilot" {
				t.Errorf("UserBadge.Name = %q, want Copilot", got.Name)
			}
			if got.Present != tc.wantPresent || got.Broken != tc.wantBroken {
				t.Errorf("UserBadge = %+v, want Present=%v Broken=%v", got, tc.wantPresent, tc.wantBroken)
			}
		})
	}
}

// TestCopilotUserConfig_InterfaceConformance pins compile-time conformance with
// UserConfigReporter for the copilot platform.
func TestCopilotUserConfig_InterfaceConformance(t *testing.T) {
	var _ UserConfigReporter = (*copilot)(nil)
}

// TestCopilotCreateUserHomeHookFiles proves CreateLinks wires a global-scope
// canonical hook into ~/.copilot/hooks/ (the user-home fanout), and that the
// freshly rendered file is reported present/clean by the badge.
func TestCopilotCreateUserHomeHookFiles(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	if err := os.MkdirAll(home, 0755); err != nil {
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
	if err := c.createUserHomeHookFiles("proj", agentsHome); err != nil {
		t.Fatalf("createUserHomeHookFiles: %v", err)
	}
	out := filepath.Join(copilotUserHooksDir(home), "prompt-log.json")
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected user-home hook at %s: %v", out, err)
	}

	badge := c.UserBadge(home)
	if !badge.Present || badge.Broken {
		t.Errorf("UserBadge = %+v, want Present=true Broken=false", badge)
	}
}

// seedCopilotGlobalHook writes a global-scope canonical HOOK.yaml that renders
// to a single ~/.copilot/hooks/<name>.json file, mirroring the fixture used by
// TestCopilotCreateUserHomeHookFiles. Keeps the user-home wiring tests terse.
func seedCopilotGlobalHook(t *testing.T, agentsHome string) {
	t.Helper()
	manifest := filepath.Join(agentsHome, "hooks", "global", "prompt-log", "HOOK.yaml")
	mkdirAllT(t, filepath.Dir(manifest))
	if err := os.WriteFile(manifest, []byte("name: prompt-log\nwhen: user_prompt_submit\nrun:\n  command: /bin/echo\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

// TestCopilotCreateUserHomeHookFiles_Errors drives the two error returns of
// createUserHomeHookFiles: a malformed global HOOK.yaml fails the canonical
// collect, and a MkdirAll fault on the ~/.copilot/hooks/ target fails the
// per-home-root emit (a seeded valid hook makes that fanout non-empty).
func TestCopilotCreateUserHomeHookFiles_Errors(t *testing.T) {
	tests := []struct {
		name  string
		seed  func(t *testing.T, agentsHome string)
		ioErr string
	}{
		{
			name: "collect error: malformed manifest",
			seed: func(t *testing.T, agentsHome string) {
				manifest := filepath.Join(agentsHome, "hooks", "global", "bad", "HOOK.yaml")
				mkdirAllT(t, filepath.Dir(manifest))
				if err := os.WriteFile(manifest, []byte("name: [unterminated\n"), 0644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:  "emit error: mkdir fault on user hooks dir",
			seed:  seedCopilotGlobalHook,
			ioErr: ".copilot",
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			agentsHome := filepath.Join(tmp, ".agents")
			t.Setenv("AGENTS_HOME", agentsHome)
			t.Setenv("HOME", filepath.Join(tmp, "home"))
			mkdirAllT(t, filepath.Join(tmp, "home"))
			tc.seed(t, agentsHome)

			c := &copilot{io: stdPlatformIO{}}
			if tc.ioErr != "" {
				c.io = withMkdirAllError(t, tc.ioErr)
			}
			if err := c.createUserHomeHookFiles("proj", agentsHome); err == nil {
				t.Fatal("expected createUserHomeHookFiles to return an error")
			}
		})
	}
}

// TestCopilotCreateLinks_WiresUserHomeHooks drives CreateLinks end to end with a
// seeded global hook and a real HOME, proving the user-home fanout call wires
// ~/.copilot/hooks/<name>.json alongside the repo-scope links.
func TestCopilotCreateLinks_WiresUserHomeHooks(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	mkdirAllT(t, home)
	seedCopilotGlobalHook(t, agentsHome)

	repo := filepath.Join(tmp, "repo")
	mkdirAllT(t, repo)
	if err := NewCopilot().CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	for _, expect := range []string{
		filepath.Join(repo, ".github", "hooks", "prompt-log.json"),
		filepath.Join(copilotUserHooksDir(home), "prompt-log.json"),
	} {
		if _, err := os.Stat(expect); err != nil {
			t.Errorf("expected %s: %v", expect, err)
		}
	}
}

// TestCopilotCreateLinks_ProjectHookFilesMkdirError covers CreateLinks' repo-
// scope hooks error return: a MkdirAll fault scoped to .github/hooks lets
// every earlier step (instructions, skills, agents, mcp, claude-compat)
// succeed, then fails createProjectHookFiles so CreateLinks propagates the
// error instead of continuing to the user-home hooks step.
func TestCopilotCreateLinks_ProjectHookFilesMkdirError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	mkdirAllT(t, home)
	seedCopilotGlobalHook(t, agentsHome)

	repo := filepath.Join(tmp, "repo")
	mkdirAllT(t, repo)
	c := &copilot{io: withMkdirAllError(t, filepath.Join(".github", "hooks"))}
	if err := c.CreateLinks("proj", repo); err == nil {
		t.Fatal("expected CreateLinks to surface the .github/hooks MkdirAll fault")
	}
}

// TestCopilotCreateLinks_UserHomeHookError covers CreateLinks' user-home error
// return: a MkdirAll fault scoped to the ~/.copilot/hooks/ path lets every
// earlier repo-scope link succeed, then fails the trailing createUserHomeHookFiles
// call so CreateLinks propagates the error.
func TestCopilotCreateLinks_UserHomeHookError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	home := filepath.Join(tmp, "home")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	mkdirAllT(t, home)
	seedCopilotGlobalHook(t, agentsHome)

	repo := filepath.Join(tmp, "repo")
	mkdirAllT(t, repo)
	c := &copilot{io: withMkdirAllError(t, filepath.Join(".copilot", "hooks"))}
	if err := c.CreateLinks("proj", repo); err == nil {
		t.Fatal("expected CreateLinks to surface the user-home MkdirAll fault")
	}
}

// TestCopilotManagedOutputs_CoversSharedTargets pins the BLOCKER-1 root cause
// (drift): every repo-local target copilot PROJECTS via SharedTargetIntents
// (e.g. the .agents/skills/ mirror, .github/agents/*.agent.md) must be covered
// by some ManagedOutputs() pattern, or `da refresh` leaves it un-ignored. This
// catches a FUTURE output added without updating ManagedOutputs, not just the
// one omission the cross-harness reviewer found. Authoritative output set:
// docs/PLATFORM_DIRS_DOCS.md ("GitHub Copilot" impl-audit row).
func TestCopilotManagedOutputs_CoversSharedTargets(t *testing.T) {
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
	c := NewCopilot()
	intents, err := c.SharedTargetIntents("proj")
	if err != nil {
		t.Fatalf("SharedTargetIntents: %v", err)
	}
	if len(intents) == 0 {
		t.Fatal("expected non-zero shared-target intents")
	}
	r, ok := c.(ManagedOutputReporter)
	if !ok {
		t.Fatal("copilot must implement ManagedOutputReporter")
	}
	outs := r.ManagedOutputs()
	for _, in := range intents {
		target := strings.ReplaceAll(in.TargetPath, `\`, "/")
		covered := false
		for _, o := range outs {
			dir := strings.TrimSuffix(strings.ReplaceAll(o, `\`, "/"), "/")
			if target == dir || strings.HasPrefix(target, dir+"/") {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("copilot shared-target %q not covered by any ManagedOutputs entry %v; managed .gitignore would leak it", target, outs)
		}
	}
}

// TestCollectManagedOutputs_ReporterAndStaticBranches covers both arms of
// CollectManagedOutputs: a ManagedOutputReporter platform (copilot, dynamic) and
// a static-table platform (cursor). Pins the D14 collector's coverage.
func TestCollectManagedOutputs_ReporterAndStaticBranches(t *testing.T) {
	got := CollectManagedOutputs([]Platform{NewCopilot(), NewCursor()})
	has := func(want string) bool {
		for _, g := range got {
			if g == want {
				return true
			}
		}
		return false
	}
	// copilot (ManagedOutputReporter) dynamic outputs:
	for _, w := range []string{".agents/skills/", ".github/hooks/*.json"} {
		if !has(w) {
			t.Errorf("CollectManagedOutputs missing copilot reporter output %q; got %v", w, got)
		}
	}
	// cursor (static table) output:
	if !has(cursorDir + "/") {
		t.Errorf("CollectManagedOutputs missing cursor static output %q; got %v", cursorDir+"/", got)
	}
}
