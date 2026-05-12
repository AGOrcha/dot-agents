package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

func TestHasPluginPlatform(t *testing.T) {
	cases := []struct {
		name      string
		platforms []string
		want      string
		expect    bool
	}{
		{"empty list", nil, "opencode", false},
		{"present", []string{"opencode", "claude"}, "opencode", true},
		{"absent", []string{"claude", "codex"}, "opencode", false},
		{"single match", []string{"opencode"}, "opencode", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := hasPluginPlatform(c.platforms, c.want); got != c.expect {
				t.Errorf("hasPluginPlatform(%v, %q) = %v, want %v", c.platforms, c.want, got, c.expect)
			}
		})
	}
}

// ---------- collectBrokenLinks ----------

func TestCollectBrokenLinks_EmptyProject(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)
	os.MkdirAll(agentsHome, 0755)

	got := collectBrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("expected no broken links in empty project, got %d: %+v", len(got), got)
	}
}

func TestCollectBrokenLinks_HealthyClaudeSymlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")

	target := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("# rules"), 0644)

	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	if err := os.Symlink(target, filepath.Join(claudeRules, "proj--agents.md")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("expected no broken links for healthy symlink, got %+v", got)
	}
}

func TestCollectBrokenLinks_BrokenClaudeSymlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")

	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	dangling := filepath.Join(agentsHome, "rules", "proj", "ghost.md")
	if err := os.Symlink(dangling, filepath.Join(claudeRules, "proj--ghost.md")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 {
		t.Fatalf("expected 1 broken link, got %d: %+v", len(got), got)
	}
	if got[0].platformID != "claude" {
		t.Errorf("expected platformID=claude, got %q", got[0].platformID)
	}
}

func TestCollectBrokenLinks_BrokenAgentsMD(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	dangling := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	if err := os.Symlink(dangling, filepath.Join(projectPath, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 || got[0].platformID != "codex" {
		t.Fatalf("expected 1 codex broken link, got %+v", got)
	}
}

func TestCollectBrokenLinks_BrokenCopilotInstructions(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	ghDir := filepath.Join(projectPath, ".github")
	os.MkdirAll(ghDir, 0755)

	dangling := filepath.Join(agentsHome, "missing.md")
	if err := os.Symlink(dangling, filepath.Join(ghDir, "copilot-instructions.md")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 || got[0].platformID != "copilot" {
		t.Fatalf("expected 1 copilot broken link, got %+v", got)
	}
}

func TestCollectBrokenLinks_BrokenVSCodeMCP(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	vsDir := filepath.Join(projectPath, ".vscode")
	os.MkdirAll(vsDir, 0755)

	dangling := filepath.Join(agentsHome, "missing.json")
	if err := os.Symlink(dangling, filepath.Join(vsDir, "mcp.json")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 || got[0].platformID != "copilot" {
		t.Fatalf("expected 1 copilot mcp broken link, got %+v", got)
	}
}

func TestCollectBrokenLinks_BrokenClaudeMCP(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	dangling := filepath.Join(agentsHome, "missing.json")
	if err := os.Symlink(dangling, filepath.Join(projectPath, ".mcp.json")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 || got[0].platformID != "claude" {
		t.Fatalf("expected 1 claude mcp broken link, got %+v", got)
	}
}

func TestCollectBrokenLinks_BrokenOpenCodeJSON(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	dangling := filepath.Join(agentsHome, "missing.json")
	if err := os.Symlink(dangling, filepath.Join(projectPath, "opencode.json")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 || got[0].platformID != "opencode" {
		t.Fatalf("expected 1 opencode broken link, got %+v", got)
	}
}

// ---------- countProjectLinks ----------

func TestCountProjectLinks_Empty(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)
	os.MkdirAll(agentsHome, 0755)

	ok, broken := countProjectLinks("proj", projectPath, agentsHome)
	if ok != 0 || broken != 0 {
		t.Errorf("expected (0,0), got (%d,%d)", ok, broken)
	}
}

func TestCountProjectLinks_HealthyAndBroken(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	// Healthy claude symlink
	target := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("ok"), 0644)
	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	if err := os.Symlink(target, filepath.Join(claudeRules, "proj--agents.md")); err != nil {
		t.Fatal(err)
	}

	// Broken AGENTS.md
	dangling := filepath.Join(agentsHome, "missing.md")
	if err := os.Symlink(dangling, filepath.Join(projectPath, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}

	ok, broken := countProjectLinks("proj", projectPath, agentsHome)
	if ok != 1 {
		t.Errorf("expected ok=1, got %d", ok)
	}
	if broken != 1 {
		t.Errorf("expected broken=1, got %d", broken)
	}
}

// ---------- collectBrokenUserLinks ----------

func TestCollectBrokenUserLinks_EmptyHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	got := collectBrokenUserLinks(agentsHome)
	if len(got) != 0 {
		t.Errorf("expected no broken user links on fresh home, got %+v", got)
	}
}

func TestCollectBrokenUserLinks_BrokenClaudeMD(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	claudeHome := filepath.Join(tmp, ".claude")
	os.MkdirAll(claudeHome, 0755)
	dangling := filepath.Join(agentsHome, "missing-claude.md")
	if err := os.Symlink(dangling, filepath.Join(claudeHome, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenUserLinks(agentsHome)
	if len(got) != 1 || got[0].platformID != "claude" {
		t.Fatalf("expected 1 claude broken link, got %+v", got)
	}
}

func TestCollectBrokenUserLinks_BrokenClaudeAgentsDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	agentsSubDir := filepath.Join(tmp, ".claude", "agents")
	os.MkdirAll(agentsSubDir, 0755)
	dangling := filepath.Join(agentsHome, "agents", "global", "ghost.md")
	if err := os.Symlink(dangling, filepath.Join(agentsSubDir, "ghost.md")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenUserLinks(agentsHome)
	if len(got) != 1 || got[0].platformID != "claude" {
		t.Fatalf("expected 1 claude broken agent, got %+v", got)
	}
}

func TestCollectBrokenUserLinks_BrokenCodexAgent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	codexAgentsDir := filepath.Join(tmp, ".codex", "agents")
	os.MkdirAll(codexAgentsDir, 0755)
	dangling := filepath.Join(agentsHome, "agents", "global", "missing")
	if err := os.Symlink(dangling, filepath.Join(codexAgentsDir, "missing")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenUserLinks(agentsHome)
	if len(got) != 1 || got[0].platformID != "codex" {
		t.Fatalf("expected 1 codex broken agent, got %+v", got)
	}
}

func TestCollectBrokenUserLinks_BrokenOpenCodeAgent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	ocDir := filepath.Join(tmp, ".opencode", "agent")
	os.MkdirAll(ocDir, 0755)
	dangling := filepath.Join(agentsHome, "agents", "global", "missing.md")
	if err := os.Symlink(dangling, filepath.Join(ocDir, "missing.md")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenUserLinks(agentsHome)
	if len(got) != 1 || got[0].platformID != "opencode" {
		t.Fatalf("expected 1 opencode broken agent, got %+v", got)
	}
}

// ---------- runDoctor end-to-end (no projects) ----------

func TestRunDoctor_EmptyConfigSucceeds(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Write an empty config.json so Load returns a parsed-but-empty config
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Reset relevant Flags between tests
	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor on empty home: %v", err)
	}
}

func TestRunDoctor_DryRunWithBrokenLinks(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	os.MkdirAll(projectPath, 0755)

	// Register project
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Introduce a broken AGENTS.md symlink
	dangling := filepath.Join(agentsHome, "rules", "myproj", "agents.md")
	if err := os.Symlink(dangling, filepath.Join(projectPath, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()

	// Dry run should not error even with broken links
	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor --dry-run with broken link: %v", err)
	}
}
