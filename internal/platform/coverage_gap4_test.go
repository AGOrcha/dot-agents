package platform

// Fourth-wave coverage tests. Focused on a single comprehensive
// "everything wired" fixture that drives each platform's CreateLinks +
// shared-target plan + RemoveLinks against every helper path: rules,
// settings, MCP, canonical hook bundles, agents, skills, plugins.
// This raises statement coverage on the deeply branched CreateLinks /
// shared-target functions.

import (
	"os"
	"path/filepath"
	"testing"
)

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
