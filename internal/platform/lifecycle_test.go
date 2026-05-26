package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// populatedAgentsHome builds a comprehensive ~/.agents/ fixture with
// rules, settings, mcp, skills, agents, and hooks for both "global" and the
// given project scope. Returned path is suitable for AGENTS_HOME.
func populatedAgentsHome(t *testing.T, project string) (agentsHome, home string) {
	t.Helper()
	tmp := t.TempDir()
	agentsHome = filepath.Join(tmp, ".agents")
	home = filepath.Join(tmp, "userhome")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}

	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{agentsHome}, parts...)...)
		if err := os.MkdirAll(p, 0755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	wf := func(path, content string) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Rules
	wf(filepath.Join(mk("rules", "global"), "rules.md"), "# global rules\n")
	wf(filepath.Join(mk("rules", project), "custom.md"), "# project rule\n")
	wf(filepath.Join(agentsHome, "rules", "global", "agents.md"), "# AGENTS\n")

	// Skills with marker
	for _, scope := range []string{"global", project} {
		d := mk("skills", scope, "my-skill")
		wf(filepath.Join(d, "SKILL.md"),
			"---\nname: my-skill\ndescription: a skill\n---\n# Body\n")
	}

	// Agents with marker
	for _, scope := range []string{"global", project} {
		d := mk("agents", scope, "reviewer")
		wf(filepath.Join(d, "AGENT.md"),
			"---\nname: reviewer\ndescription: a reviewer\n---\n# Body\n")
	}

	// Settings
	wf(filepath.Join(mk("settings", "global"), "claude-code.json"), `{"version":1}`)
	wf(filepath.Join(mk("settings", project), "claude-code.json"), `{"version":1}`)
	wf(filepath.Join(agentsHome, "settings", "global", "cursor.json"), `{}`)

	// MCP
	wf(filepath.Join(mk("mcp", project), "mcp.json"), `{"mcpServers":{"x":{}}}`)
	wf(filepath.Join(mk("mcp", "global"), "mcp.json"), `{"mcpServers":{"g":{}}}`)
	return agentsHome, home
}

func TestLifecycle_ClaudeCreateRemove(t *testing.T) {
	agentsHome, home := populatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	os.MkdirAll(repo, 0755)

	p := NewClaude()
	if err := p.CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}

	// Verify some artefacts
	if _, err := os.Stat(filepath.Join(repo, ".claude", "rules")); err != nil {
		t.Errorf("rules dir missing: %v", err)
	}
	if err := p.RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
}

func TestLifecycle_CursorCreateRemove(t *testing.T) {
	agentsHome, home := populatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	os.MkdirAll(repo, 0755)

	p := NewCursor()
	if err := p.CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	if err := p.RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
}

func TestLifecycle_CopilotCreateRemove(t *testing.T) {
	agentsHome, home := populatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	os.MkdirAll(repo, 0755)

	p := NewCopilot()
	if err := p.CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	if err := p.RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
}

func TestLifecycle_OpenCodeCreateRemove(t *testing.T) {
	agentsHome, home := populatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	os.MkdirAll(repo, 0755)

	p := NewOpenCode()
	if err := p.CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	if err := p.RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
}

func TestLifecycle_CodexCreateRemove(t *testing.T) {
	agentsHome, home := populatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	os.MkdirAll(repo, 0755)

	p := NewCodex()
	if err := p.CreateLinks("proj", repo); err != nil {
		t.Fatalf("CreateLinks: %v", err)
	}
	if err := p.RemoveLinks("proj", repo); err != nil {
		t.Fatalf("RemoveLinks: %v", err)
	}
}

// TestLifecycle_SharedTargetIntentsForAllPlatforms drives the shared-target
// projection path across every platform with a populated AgentsHome.
func TestLifecycle_SharedTargetIntentsForAllPlatforms(t *testing.T) {
	agentsHome, home := populatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	for _, p := range All() {
		intents, err := p.SharedTargetIntents("proj")
		if err != nil {
			t.Errorf("%s: SharedTargetIntents: %v", p.ID(), err)
		}
		// Just sanity-check intents have non-empty targets and the right project.
		for i, intent := range intents {
			if intent.TargetPath == "" {
				t.Errorf("%s intent[%d] empty TargetPath", p.ID(), i)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Comprehensive fully-populated fixture tests (relocated from coverage_gap4).
// These drive each platform's CreateLinks + shared-target plan + RemoveLinks
// against every helper path: rules, settings, MCP, canonical hook bundles,
// agents, skills, plugins.
// ---------------------------------------------------------------------------

// fullyPopulatedAgentsHome builds an exhaustive fixture covering every
// resource bucket used by the platform package.
func fullyPopulatedAgentsHome(t *testing.T, project string) (agentsHome, home string) {
	t.Helper()
	tmp := t.TempDir()
	agentsHome = filepath.Join(tmp, ".agents")
	home = filepath.Join(tmp, "home")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}

	mkfile := func(parts ...string) string {
		path := filepath.Join(append([]string{agentsHome}, parts...)...)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	wf := func(path, content string) {
		mkfile(path) // ensure parent
		if err := os.MkdirAll(filepath.Dir(filepath.Join(agentsHome, path)), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(agentsHome, path), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Rules — multiple flavours used by different platforms.
	wf("rules/global/rules.md", "# global rules\n")
	wf("rules/global/claude-code.md", "# global claude rules\n")
	wf("rules/global/agents.md", "# global codex agents.md\n")
	wf("rules/global/copilot-instructions.md", "# global copilot\n")
	wf("rules/"+project+"/custom.md", "# project rule\n")
	wf("rules/"+project+"/copilot-instructions.md", "# project copilot\n")
	wf("rules/"+project+"/agents.md", "# project codex\n")

	// Settings — JSON for each platform.
	wf("settings/global/claude-code.json", `{"version":1}`)
	wf("settings/"+project+"/claude-code.json", `{"version":1}`)
	wf("settings/global/cursor.json", "{}")
	wf("settings/"+project+"/cursor.json", "{}")
	wf("settings/"+project+"/cursorignore", "node_modules\n")
	wf("settings/"+project+"/codex.toml", `model = "x"`)
	wf("settings/"+project+"/opencode.json", "{}")

	// MCP.
	wf("mcp/"+project+"/claude.json", `{"mcpServers":{}}`)
	wf("mcp/"+project+"/cursor.json", `{"mcpServers":{}}`)
	wf("mcp/"+project+"/copilot.json", `{"mcpServers":{}}`)
	wf("mcp/"+project+"/mcp.json", `{"mcpServers":{}}`)

	// Canonical hook bundles (HOOK.yaml) — different `when` so different platforms keep them.
	wf("hooks/global/prompt-log/HOOK.yaml", `name: prompt-log
when: user_prompt_submit
run:
  command: /bin/echo prompt-log
`)
	wf("hooks/"+project+"/pre-tool/HOOK.yaml", `name: pre-tool
when: pre_tool_use
match:
  expression: "Bash"
run:
  command: /bin/echo pre-tool
  timeout_ms: 7000
`)
	// Legacy hook files (single-file JSON).
	wf("hooks/"+project+"/claude-code.json", `{"hooks":{}}`)
	wf("hooks/"+project+"/cursor.json", `{"hooks":{}}`)
	wf("hooks/"+project+"/codex.json", `{"hooks":{}}`)

	// Skills.
	for _, scope := range []string{"global", project} {
		wf("skills/"+scope+"/my-skill/SKILL.md",
			"---\nname: my-skill\ndescription: x\n---\n# Body\n")
	}

	// Agents.
	for _, scope := range []string{"global", project} {
		wf("agents/"+scope+"/reviewer/AGENT.md",
			"---\nname: reviewer\ndescription: reviewer\n---\n# Body\n")
	}

	// Plugin bundles (for opencode SharedTargetIntents plugin branch).
	wf("plugins/"+project+"/rt/PLUGIN.yaml",
		"schema_version: 1\nkind: native\nname: rt\nplatforms: [opencode]\n")

	return agentsHome, home
}

func TestLifecycle_AllPlatformsFullyPopulated(t *testing.T) {
	agentsHome, home := fullyPopulatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)

	for _, p := range All() {
		p := p
		t.Run(p.ID(), func(t *testing.T) {
			repo := filepath.Join(t.TempDir(), "repo-"+p.ID())
			if err := os.MkdirAll(repo, 0755); err != nil {
				t.Fatal(err)
			}
			// Run the shared-target projection first (mirrors the command flow).
			if err := CollectAndExecuteSharedTargetPlan("proj", repo, []Platform{p}); err != nil {
				t.Errorf("shared-target plan: %v", err)
			}
			if err := p.CreateLinks("proj", repo); err != nil {
				t.Errorf("CreateLinks: %v", err)
			}
			if err := p.RemoveLinks("proj", repo); err != nil {
				t.Errorf("RemoveLinks: %v", err)
			}
		})
	}
}

// TestRemoveSharedTargetPlan_Populated drives RemoveSharedTargetPlan with a
// realistic fixture so the remove path is exercised end-to-end.
func TestRemoveSharedTargetPlan_Populated(t *testing.T) {
	agentsHome, home := fullyPopulatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	platforms := All()
	if err := CollectAndExecuteSharedTargetPlan("proj", repo, platforms); err != nil {
		t.Fatalf("CollectAndExecuteSharedTargetPlan: %v", err)
	}
	if err := RemoveSharedTargetPlan("proj", repo, platforms); err != nil {
		t.Errorf("RemoveSharedTargetPlan: %v", err)
	}
}

// TestRunSharedTargetProjection_DryAndExecute drives both paths.
func TestRunSharedTargetProjection_DryAndExecute(t *testing.T) {
	agentsHome, home := fullyPopulatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	platforms := All()
	lines, err := RunSharedTargetProjection("proj", repo, platforms, true)
	if err != nil {
		t.Fatalf("dry-run projection: %v", err)
	}
	if len(lines) == 0 {
		t.Error("expected dry-run lines")
	}
	if _, err := RunSharedTargetProjection("proj", repo, platforms, false); err != nil {
		t.Errorf("execute projection: %v", err)
	}
}

// TestDryRunSharedTargetPlanLines_NoIntents covers the no-resources branch.
func TestDryRunSharedTargetPlanLines_NoIntents(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	t.Setenv("HOME", filepath.Join(tmp, "home"))
	if err := os.MkdirAll(filepath.Join(tmp, "home"), 0755); err != nil {
		t.Fatal(err)
	}
	lines, err := DryRunSharedTargetPlanLines("proj", tmp, All())
	if err != nil {
		t.Fatalf("DryRunSharedTargetPlanLines: %v", err)
	}
	if len(lines) != 1 {
		t.Errorf("expected one (none) line, got %v", lines)
	}
}

// TestExecuteSharedSkillMirrorPlan_MultipleRoots drives multi-root iteration.
func TestExecuteSharedSkillMirrorPlan_MultipleRoots(t *testing.T) {
	agentsHome, home := fullyPopulatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(repo, 0755); err != nil {
		t.Fatal(err)
	}
	if err := ExecuteSharedSkillMirrorPlan("proj", repo, ".agents/skills", ".claude/skills"); err != nil {
		t.Fatalf("ExecuteSharedSkillMirrorPlan multi-root: %v", err)
	}
	for _, p := range []string{".agents/skills/my-skill", ".claude/skills/my-skill"} {
		if _, err := os.Lstat(filepath.Join(repo, p)); err != nil {
			t.Errorf("expected mirror at %s: %v", p, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Shared-target intents + dry-run formatter coverage (relocated from
// coverage_gap5_test.go).
// ---------------------------------------------------------------------------

// TestSharedTargetIntents_AllPlatformsPopulated provides coverage of the
// concatenation paths for each platform's SharedTargetIntents.
func TestSharedTargetIntents_AllPlatformsCoverConcat(t *testing.T) {
	agentsHome, home := fullyPopulatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	for _, p := range All() {
		intents, err := p.SharedTargetIntents("proj")
		if err != nil {
			t.Errorf("%s SharedTargetIntents: %v", p.ID(), err)
		}
		if len(intents) == 0 {
			t.Errorf("%s expected intents", p.ID())
		}
	}
}

// TestFormatSharedTargetPlanForDryRun_AllVariants exercises each formatter
// branch (DirectDir, DirectFile, RenderSingle, default).
func TestFormatSharedTargetPlanForDryRun_RenderVariant(t *testing.T) {
	agentsHome, home := fullyPopulatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	lines, err := DryRunSharedTargetPlanLines("proj", repo, []Platform{NewCodex()})
	if err != nil {
		t.Fatalf("DryRunSharedTargetPlanLines: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("expected dry-run lines")
	}
	// At least one line should mention "write" for codex agent toml.
	gotWrite := false
	for _, l := range lines {
		if strings.Contains(l, "write") {
			gotWrite = true
			break
		}
	}
	if !gotWrite {
		t.Errorf("expected write line in %+v", lines)
	}
}

// TestFormatSharedTargetPlanForDryRun_FileVariant drives the DirectFile branch
// (BuildSharedAgentFileSymlinkIntents → copilot/opencode).
func TestFormatSharedTargetPlanForDryRun_FileVariant(t *testing.T) {
	agentsHome, home := fullyPopulatedAgentsHome(t, "proj")
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", home)
	repo := filepath.Join(t.TempDir(), "repo")
	lines, err := DryRunSharedTargetPlanLines("proj", repo, []Platform{NewCopilot()})
	if err != nil {
		t.Fatalf("DryRunSharedTargetPlanLines: %v", err)
	}
	if len(lines) == 0 {
		t.Fatal("expected dry-run lines")
	}
	gotFile := false
	for _, l := range lines {
		if strings.Contains(l, "symlink file") {
			gotFile = true
			break
		}
	}
	if !gotFile {
		t.Errorf("expected symlink-file line in %+v", lines)
	}
}
