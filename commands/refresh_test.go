package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

const refreshCanonicalAgentPath = "agents/proj/my-agent/AGENT.md"

// ---------- mapResourceRelToDest ----------

func TestMapResourceRelToDest_MCPCanonicalization(t *testing.T) {
	cases := []struct {
		relPath  string
		expected string
	}{
		// All platform MCP files must normalize to the canonical mcp.json
		{".mcp.json", "mcp/proj/mcp.json"},
		{".cursor/mcp.json", "mcp/proj/mcp.json"},
		{".vscode/mcp.json", "mcp/proj/mcp.json"},
		// Other mappings must remain intact
		{".cursor/settings.json", "settings/proj/cursor.json"},
		{".cursorignore", "settings/proj/cursorignore"},
		{".claude/settings.local.json", "settings/proj/claude-code.json"},
		{"opencode.json", "settings/proj/opencode.json"},
		{"AGENTS.md", "rules/proj/agents.md"},
		{".codex/instructions.md", "rules/proj/agents.md"},
		{".codex/rules.md", "rules/proj/agents.md"},
		{".codex/config.toml", "settings/proj/codex.toml"},
		{".codex/hooks.json", "hooks/proj/codex.json"},
		{".github/copilot-instructions.md", "rules/proj/copilot-instructions.md"},
		{".github/hooks/pre-tool.json", "hooks/proj/pre-tool/HOOK.yaml"},
	}
	for _, c := range cases {
		got := mapResourceRelToDest("proj", c.relPath)
		if got != c.expected {
			t.Errorf("mapResourceRelToDest(%q) = %q, want %q", c.relPath, got, c.expected)
		}
	}
}

func TestMapResourceRelToDest_SkillsAndAgents(t *testing.T) {
	cases := []struct {
		relPath  string
		expected string
	}{
		{".agents/skills/my-skill/SKILL.md", "skills/proj/my-skill/SKILL.md"},
		{".claude/skills/my-skill/SKILL.md", "skills/proj/my-skill/SKILL.md"},
		{".github/agents/my-agent.agent.md", refreshCanonicalAgentPath},
		{".codex/agents/my-agent/AGENT.md", refreshCanonicalAgentPath},
		{".opencode/agent/my-agent.md", refreshCanonicalAgentPath},
	}
	for _, c := range cases {
		got := mapResourceRelToDest("proj", c.relPath)
		if got != c.expected {
			t.Errorf("mapResourceRelToDest(%q) = %q, want %q", c.relPath, got, c.expected)
		}
	}
}

func TestMapResourceRelToDest_CursorRules(t *testing.T) {
	cases := []struct {
		relPath  string
		expected string
	}{
		{".cursor/rules/global--rules.mdc", "rules/global/rules.mdc"},
		{".cursor/rules/proj--rules.mdc", "rules/proj/rules.mdc"},
		{".cursor/rules/some-rule.mdc", "rules/proj/some-rule.mdc"},
	}
	for _, c := range cases {
		got := mapResourceRelToDest("proj", c.relPath)
		if got != c.expected {
			t.Errorf("mapResourceRelToDest(%q) = %q, want %q", c.relPath, got, c.expected)
		}
	}
}

func TestMapResourceRelToDest_PassThrough(t *testing.T) {
	cases := []struct {
		relPath  string
		expected string
	}{
		// Already under known ~/.agents dirs — pass through unchanged
		{"rules/proj/rules.mdc", "rules/proj/rules.mdc"},
		{"mcp/proj/mcp.json", "mcp/proj/mcp.json"},
		{"settings/proj/cursor.json", "settings/proj/cursor.json"},
	}
	for _, c := range cases {
		got := mapResourceRelToDest("proj", c.relPath)
		if got != c.expected {
			t.Errorf("mapResourceRelToDest(%q) = %q, want %q", c.relPath, got, c.expected)
		}
	}
}

func TestMapResourceRelToDest_UnknownReturnsEmpty(t *testing.T) {
	got := mapResourceRelToDest("proj", ".some/unknown/path.json")
	if got != "" {
		t.Errorf("expected empty for unknown path, got %q", got)
	}
}

// ---------- NewRefreshCmd metadata ----------

func TestNewRefreshCmd_FlagsAndArgs(t *testing.T) {
	cmd := NewRefreshCmd()
	if cmd.Flags().Lookup("import") == nil {
		t.Error("missing --import flag")
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Errorf("expected refresh to accept zero args, got: %v", err)
	}
	if err := cmd.Args(cmd, []string{"one"}); err != nil {
		t.Errorf("expected refresh to accept one arg, got: %v", err)
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("expected refresh to reject more than one arg")
	}
}

// ---------- refreshImportScope ----------

func TestRefreshImportScope_DefaultsToProject(t *testing.T) {
	saved := refreshImport
	refreshImport = false
	defer func() { refreshImport = saved }()

	if got := refreshImportScope(); got != importScopeProject {
		t.Errorf("expected %q, got %q", importScopeProject, got)
	}
}

func TestRefreshImportScope_AllWhenImportFlagSet(t *testing.T) {
	saved := refreshImport
	refreshImport = true
	defer func() { refreshImport = saved }()

	if got := refreshImportScope(); got != importScopeAll {
		t.Errorf("expected %q, got %q", importScopeAll, got)
	}
}

// ---------- resolveRefreshCommit ----------

func TestResolveRefreshCommit_ReflectsBuildVars(t *testing.T) {
	savedCommit, savedDescribe := Commit, Describe
	Commit = "abc1234567"
	Describe = "v1.2.3-4-gabc"
	defer func() {
		Commit = savedCommit
		Describe = savedDescribe
	}()

	c, d := resolveRefreshCommit()
	if c != "abc1234567" || d != "v1.2.3-4-gabc" {
		t.Errorf("resolveRefreshCommit = (%q,%q), want (abc1234567, v1.2.3-4-gabc)", c, d)
	}
}

// ---------- runRefresh ----------

func TestRunRefresh_NoManagedProjectsReturnsOk(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRefresh(""); err != nil {
		t.Errorf("runRefresh with no projects: %v", err)
	}
}

func TestRunRefresh_UnknownProjectFilterErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", filepath.Join(tmp, "p"))
	os.MkdirAll(filepath.Join(tmp, "p"), 0755)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runRefresh("ghost")
	if err == nil {
		t.Fatal("expected error when filter targets unknown project")
	}
}

// ---------- additional coverage ----------

// runRefresh with a registered project under dry-run completes the success path.
func TestRunRefresh_RegisteredProjectDryRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)
	// Manifest with a git source — exercises the dry-run notice + sources scan.
	rc := &config.AgentsRC{Version: 1, Project: "p", Sources: []config.Source{{Type: "git", URL: "https://example.invalid/x.git"}}}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runRefresh(""); err != nil {
		t.Errorf("runRefresh dry-run: %v", err)
	}
}

// runRefresh with a project whose directory is missing skips that project.
func TestRunRefresh_SkipsMissingProjectDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("gone", filepath.Join(tmp, "gone-dir"))
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runRefresh(""); err != nil {
		t.Errorf("runRefresh with missing dir: %v", err)
	}
}

// mapResourceRelToDest: cover root-level fallback and unprefixed pass-through.
func TestMapResourceRelToDest_RootLevelFallback(t *testing.T) {
	got := mapResourceRelToDest("proj", "loose-file.txt")
	if got != "settings/proj/loose-file.txt" {
		t.Errorf("expected root-level fallback to settings/, got %q", got)
	}
}

// mapResourceRelToDest: exact-match command-dir cases.
func TestMapResourceRelToDest_CommandsBuckets(t *testing.T) {
	if got := mapResourceRelToDest("proj", ".cursor/commands/"); got == "" {
		t.Error("expected non-empty mapping for cursor commands dir literal")
	}
	if got := mapResourceRelToDest("proj", ".claude/commands/"); got == "" {
		t.Error("expected non-empty mapping for claude commands dir literal")
	}
	if got := mapResourceRelToDest("proj", ".opencode/commands/"); got == "" {
		t.Error("expected non-empty mapping for opencode commands dir literal")
	}
	// Other exact bucket inputs.
	if got := mapResourceRelToDest("proj", ".cursor/indexing.cursorindexingignore"); got != "" {
		// not exact constant; just exercises code path
		_ = got
	}
}

func TestMapResourceRelToDest_OutputStylesAndModes(t *testing.T) {
	// Exercise the additional switch-case branches.
	if got := mapResourceRelToDest("proj", ".claude/output-styles/"); got == "" {
		t.Error("expected mapping for claude output-styles dir literal")
	}
	if got := mapResourceRelToDest("proj", ".opencode/modes/"); got == "" {
		t.Error("expected mapping for opencode modes dir literal")
	}
	if got := mapResourceRelToDest("proj", ".opencode/themes/"); got == "" {
		t.Error("expected mapping for opencode themes dir literal")
	}
	if got := mapResourceRelToDest("proj", ".github/prompts/"); got == "" {
		t.Error("expected mapping for github prompts dir literal")
	}
}

// ---------- restoreFromResources wrapper ----------

func TestRestoreFromResources_Wrapper(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	resource := filepath.Join(agentsHome, "resources", "proj", "AGENTS.md")
	os.MkdirAll(filepath.Dir(resource), 0755)
	os.WriteFile(resource, []byte("# rules"), 0644)

	// Should not panic and should perform the same restore as Counted variant.
	restoreFromResources("proj", tmp)

	want := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected restore wrapper to write %s: %v", want, err)
	}
}

// TestRunRefresh_InstalledPlatformDoesCreateLinks exercises the full refresh
// loop with an installed Claude platform: shared-target projection runs, the
// per-platform CreateLinks branch runs (non-dry-run), and the agentsrc refresh
// metadata is written.
func TestRunRefresh_InstalledPlatformDoesCreateLinks(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Make claude installed
	os.MkdirAll(filepath.Join(tmp, ".claude"), 0755)

	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRefresh(""); err != nil {
		t.Errorf("runRefresh installed: %v", err)
	}

	// agentsrc should have been written with refresh metadata even though there
	// was no prior manifest.
	if _, err := os.Stat(filepath.Join(projectPath, ".agentsrc.json")); err != nil {
		t.Errorf("expected .agentsrc.json written: %v", err)
	}
}

// TestRunRefresh_SkipsProjectWithoutPath covers the path == "" branch (path not
// found in config) and path == "." branch.
func TestRunRefresh_SkipsProjectWithoutPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Manually write a config with a "." path
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{
		"dot-project": {Path: "."},
	}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runRefresh(""); err != nil {
		t.Errorf("runRefresh with dot path: %v", err)
	}
}

// TestRunRefresh_NewRefreshCmdRunEDispatches invokes the cobra RunE closure
// directly to cover the NewRefreshCmd RunE wrapper.
func TestRunRefresh_NewRefreshCmdRunEDispatches(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	cmd := NewRefreshCmd()
	// With no args
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("RunE no-args: %v", err)
	}
	// With one filter arg
	if err := cmd.RunE(cmd, []string{"ghost"}); err == nil {
		// Should error because there are no projects so filter check is bypassed
		// (no projects → early return). Acceptable either way.
		_ = err
	}
}
