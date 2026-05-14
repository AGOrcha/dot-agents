package commands

import (
	"os"
	"path/filepath"
	"strings"
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

// ---------- additional coverage ----------

func TestCollectBrokenUserLinks_BrokenClaudeSettings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	claudeHome := filepath.Join(tmp, ".claude")
	os.MkdirAll(claudeHome, 0755)
	dangling := filepath.Join(agentsHome, "missing-settings.json")
	if err := os.Symlink(dangling, filepath.Join(claudeHome, "settings.json")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenUserLinks(agentsHome)
	if len(got) != 1 || got[0].platformID != "claude" {
		t.Fatalf("expected 1 claude settings broken link, got %+v", got)
	}
}

func TestCollectBrokenUserLinks_BrokenClaudeSkill(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	skillsDir := filepath.Join(tmp, ".claude", "skills")
	os.MkdirAll(skillsDir, 0755)
	dangling := filepath.Join(agentsHome, "skills", "ghost", "SKILL.md")
	if err := os.Symlink(dangling, filepath.Join(skillsDir, "ghost")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenUserLinks(agentsHome)
	if len(got) != 1 || got[0].platformID != "claude" {
		t.Fatalf("expected 1 claude skill broken link, got %+v", got)
	}
}

func TestCollectBrokenLinks_HealthyAGENTSMD(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	target := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("ok"), 0644)
	if err := os.Symlink(target, filepath.Join(projectPath, "AGENTS.md")); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 0 {
		t.Errorf("healthy AGENTS.md should not be broken, got %+v", got)
	}
}

func TestCollectBrokenLinks_BrokenCursorGlobalHardlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectPath := filepath.Join(tmp, "proj")
	rulesDir := filepath.Join(projectPath, ".cursor", "rules")
	os.MkdirAll(rulesDir, 0755)

	// A loose .mdc file that does not hardlink to ~/.agents/rules/global/...
	if err := os.WriteFile(filepath.Join(rulesDir, "global--rule.mdc"), []byte("rule"), 0644); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 || got[0].platformID != "cursor" {
		t.Fatalf("expected 1 broken cursor hardlink, got %+v", got)
	}
}

func TestCollectBrokenLinks_BrokenCursorProjectHardlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectPath := filepath.Join(tmp, "proj")
	rulesDir := filepath.Join(projectPath, ".cursor", "rules")
	os.MkdirAll(rulesDir, 0755)

	if err := os.WriteFile(filepath.Join(rulesDir, "proj--rule.mdc"), []byte("rule"), 0644); err != nil {
		t.Fatal(err)
	}

	got := collectBrokenLinks("proj", projectPath, agentsHome)
	if len(got) != 1 || got[0].platformID != "cursor" {
		t.Fatalf("expected 1 broken cursor project hardlink, got %+v", got)
	}
}

// printUserConfigStatus smoke runs (verbose user-config output).
func TestPrintUserConfigStatus_EmptyHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	printUserConfigStatus(agentsHome)
}

func TestPrintUserConfigStatus_PopulatedHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Healthy CLAUDE.md symlink
	target := filepath.Join(agentsHome, "rules", "global", "CLAUDE.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("# claude"), 0644)
	claudeHome := filepath.Join(tmp, ".claude")
	os.MkdirAll(claudeHome, 0755)
	os.Symlink(target, filepath.Join(claudeHome, "CLAUDE.md"))

	// Settings as local file (not symlink)
	os.WriteFile(filepath.Join(claudeHome, "settings.json"), []byte("{}"), 0644)

	// Broken agent symlink under ~/.claude/agents/
	agentsDir := filepath.Join(claudeHome, "agents")
	os.MkdirAll(agentsDir, 0755)
	os.Symlink(filepath.Join(agentsHome, "ghost.md"), filepath.Join(agentsDir, "ghost.md"))

	// Healthy skill symlink
	skillTarget := filepath.Join(agentsHome, "skills", "global", "demo", "SKILL.md")
	os.MkdirAll(filepath.Dir(skillTarget), 0755)
	os.WriteFile(skillTarget, []byte("ok"), 0644)
	skillsDir := filepath.Join(claudeHome, "skills")
	os.MkdirAll(skillsDir, 0755)
	os.Symlink(skillTarget, filepath.Join(skillsDir, "demo"))

	// Codex agents broken link
	codexDir := filepath.Join(tmp, ".codex", "agents")
	os.MkdirAll(codexDir, 0755)
	os.Symlink(filepath.Join(agentsHome, "ghost-codex"), filepath.Join(codexDir, "ghost"))

	// OpenCode agent healthy
	opencodeDir := filepath.Join(tmp, ".opencode", "agent")
	os.MkdirAll(opencodeDir, 0755)
	ocTarget := filepath.Join(agentsHome, "agents", "global", "demo", "AGENT.md")
	os.MkdirAll(filepath.Dir(ocTarget), 0755)
	os.WriteFile(ocTarget, []byte("ok"), 0644)
	os.Symlink(ocTarget, filepath.Join(opencodeDir, "demo.md"))

	printUserConfigStatus(agentsHome)
}

// runDoctor verbose mode exercises full audit output.
func TestRunDoctor_VerboseMode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	os.MkdirAll(projectPath, 0755)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Verbose: true}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor verbose: %v", err)
	}
}

// runDoctor surfaces manifest issues (corrupt manifest path).
func TestRunDoctor_CorruptManifestReported(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)
	// Write a corrupted manifest
	os.WriteFile(filepath.Join(projectPath, ".agentsrc.json"), []byte("not json"), 0644)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor with corrupt manifest: %v", err)
	}
}

// runDoctor with a manifest that has a git source not yet fetched.
func TestRunDoctor_GitSourceNotFetched(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)
	rc := &config.AgentsRC{Version: 1, Project: "p", Sources: []config.Source{{Type: "git", URL: "https://example.invalid/repo.git"}}}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor with unfetched git source: %v", err)
	}
}

// TestRunDoctor_GitSourceCachePresent covers the presentGit append + ok
// manifest branch (lines 218-220 + 227-229).
func TestRunDoctor_GitSourceCachePresent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	// Force XDG_CACHE_HOME so GitSourceCacheDir resolves there.
	cacheRoot := filepath.Join(tmp, ".cache")
	t.Setenv("XDG_CACHE_HOME", cacheRoot)

	projPath := filepath.Join(tmp, "p")
	os.MkdirAll(projPath, 0755)
	url := "https://example.invalid/cached.git"
	rc := &config.AgentsRC{Version: 1, Project: "p", Sources: []config.Source{{Type: "git", URL: url}}}
	if err := rc.Save(projPath); err != nil {
		t.Fatal(err)
	}

	// Pre-create the cache dir so doctor reports it as present.
	cacheDir := config.GitSourceCacheDir(url)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor with cached git source: %v", err)
	}
}

// TestRunDoctor_NoAgentsHome covers the early "~/.agents/ not found" branch
// and the absent-config warning branch.
func TestRunDoctor_NoAgentsHomeAndNoConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Point AGENTS_HOME at a non-existent dir.
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, "absent-agents-home"))

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor with absent home: %v", err)
	}
}

// TestRunDoctor_BrokenUserLinksReportedNonVerbose covers the non-verbose
// broken-user-link rendering loop (lines 83-86).
func TestRunDoctor_BrokenUserLinksReportedNonVerbose(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Broken user-level Claude rule symlink so collectBrokenUserLinks returns > 0.
	claudeHome := filepath.Join(tmp, ".claude")
	os.MkdirAll(claudeHome, 0755)
	os.Symlink(filepath.Join(agentsHome, "ghost.md"), filepath.Join(claudeHome, "CLAUDE.md"))

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor with broken user links: %v", err)
	}
}

// TestRunDoctor_PluginUnsupportedPlatform covers the warn branch when a
// plugin spec lists a non-opencode platform.
func TestRunDoctor_PluginUnsupportedPlatform(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Seed a plugin spec listing claude (unsupported emitter).
	pluginDir := filepath.Join(agentsHome, "plugins", "global", "demo")
	os.MkdirAll(pluginDir, 0755)
	os.WriteFile(filepath.Join(pluginDir, "PLUGIN.yaml"),
		[]byte("schema_version: 1\nkind: native\nname: demo\nplatforms: [claude, opencode]\n"), 0644)

	// Register a project so the for-each-project loop runs for the opencode plugin.
	projPath := filepath.Join(tmp, "p")
	os.MkdirAll(projPath, 0755)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Add a broken opencode plugin symlink inside the project.
	pluginLink := filepath.Join(projPath, ".opencode", "plugins", "demo")
	os.MkdirAll(filepath.Dir(pluginLink), 0755)
	if err := os.Symlink(filepath.Join(agentsHome, "missing-target"), pluginLink); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor with plugins: %v", err)
	}
}

// TestRunDoctor_OrphanCanonicalReported covers the orphan canonical resource
// warn branch.
func TestRunDoctor_OrphanCanonicalReported(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	projPath := filepath.Join(tmp, "p")
	os.MkdirAll(projPath, 0755)

	// Canonical skill exists but no back-link in project → orphan.
	skillCanonical := filepath.Join(agentsHome, "skills", "p", "abandoned")
	os.MkdirAll(skillCanonical, 0755)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor orphan: %v", err)
	}
}

// TestRunDoctor_RepairBrokenLinksFlow exercises the broken-link repair branch:
// project has a broken Claude rule symlink and an installed Claude binary.
func TestRunDoctor_RepairBrokenLinksDryRun(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Pretend Claude is installed.
	os.MkdirAll(filepath.Join(tmp, ".claude"), 0755)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	projPath := filepath.Join(tmp, "p")
	claudeRules := filepath.Join(projPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	// Broken symlink → collectBrokenLinks returns it.
	os.Symlink(filepath.Join(agentsHome, "rules", "p", "missing.md"), filepath.Join(claudeRules, "p--ghost.md"))

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{DryRun: true}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor repair dry-run: %v", err)
	}
}

// runDoctor when project directory is missing should still complete.
func TestRunDoctor_MissingProjectDirectory(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("gone", filepath.Join(tmp, "gone-path"))
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor with missing project dir: %v", err)
	}
}

func TestNewDoctorCmd_Metadata(t *testing.T) {
	cmd := NewDoctorCmd()
	if cmd.Use != "doctor" {
		t.Errorf("unexpected Use=%q", cmd.Use)
	}
	if err := cmd.Args(cmd, []string{"x"}); err == nil {
		t.Error("doctor takes no args")
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Errorf("doctor should accept zero args, got %v", err)
	}
}

// TestCollectOrphanCanonicals verifies the unit helper detects only
// canonical resource dirs missing a repo-local back-link.
func TestCollectOrphanCanonicals(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	canonicalBase := filepath.Join(agentsHome, "skills", "proj")
	os.MkdirAll(filepath.Join(canonicalBase, "alpha"), 0755) // orphan
	os.MkdirAll(filepath.Join(canonicalBase, "beta"), 0755)  // linked

	// Back-link only for beta.
	repoLocal := filepath.Join(projectPath, ".agents", "skills")
	os.MkdirAll(repoLocal, 0755)
	if err := os.Symlink(filepath.Join(canonicalBase, "beta"), filepath.Join(repoLocal, "beta")); err != nil {
		t.Fatal(err)
	}

	got := collectOrphanCanonicals("proj", projectPath, agentsHome, "skills")
	if len(got) != 1 || got[0] != "alpha" {
		t.Errorf("expected ['alpha'], got %v", got)
	}

	// No canonical dir at all -> nil result.
	if got := collectOrphanCanonicals("proj", projectPath, agentsHome, "missing"); got != nil {
		t.Errorf("expected nil for missing canonical bucket, got %v", got)
	}
}

// TestCollectOrphanCanonicals_DetectsMispointedSymlink verifies that a
// back-link symlink whose target is NOT the matching canonical entry is
// still reported as an orphan. Without this, a swapped/renamed canonical
// would silently appear healthy to the doctor.
func TestCollectOrphanCanonicals_DetectsMispointedSymlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	canonicalBase := filepath.Join(agentsHome, "skills", "proj")
	// Two canonical entries: gamma is the one whose link is mis-pointed.
	if err := os.MkdirAll(filepath.Join(canonicalBase, "gamma"), 0755); err != nil {
		t.Fatal(err)
	}
	// A different canonical that gamma's back-link will incorrectly target.
	otherCanonical := filepath.Join(agentsHome, "skills", "otherproj", "delta")
	if err := os.MkdirAll(otherCanonical, 0755); err != nil {
		t.Fatal(err)
	}

	repoLocal := filepath.Join(projectPath, ".agents", "skills")
	os.MkdirAll(repoLocal, 0755)
	// gamma's back-link points at the WRONG canonical.
	if err := os.Symlink(otherCanonical, filepath.Join(repoLocal, "gamma")); err != nil {
		t.Fatal(err)
	}

	got := collectOrphanCanonicals("proj", projectPath, agentsHome, "skills")
	if len(got) != 1 {
		t.Fatalf("expected 1 orphan, got %v", got)
	}
	if !strings.HasPrefix(got[0], "gamma") {
		t.Errorf("expected orphan entry for gamma, got %q", got[0])
	}
	if !strings.Contains(got[0], "mis-pointed") {
		t.Errorf("expected 'mis-pointed' annotation, got %q", got[0])
	}
	if !strings.Contains(got[0], otherCanonical) {
		t.Errorf("expected actual target in annotation, got %q", got[0])
	}
}

// TestCollectOrphanCanonicals_CorrectlyLinkedSymlinkNotOrphan ensures the
// happy path: a back-link that points at the matching canonical is NOT
// reported. Guards against the mis-pointed check over-reporting.
func TestCollectOrphanCanonicals_CorrectlyLinkedSymlinkNotOrphan(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	canonicalBase := filepath.Join(agentsHome, "skills", "proj")
	canonical := filepath.Join(canonicalBase, "epsilon")
	if err := os.MkdirAll(canonical, 0755); err != nil {
		t.Fatal(err)
	}

	repoLocal := filepath.Join(projectPath, ".agents", "skills")
	os.MkdirAll(repoLocal, 0755)
	if err := os.Symlink(canonical, filepath.Join(repoLocal, "epsilon")); err != nil {
		t.Fatal(err)
	}

	got := collectOrphanCanonicals("proj", projectPath, agentsHome, "skills")
	if len(got) != 0 {
		t.Errorf("expected no orphans for correctly-linked back-link, got %v", got)
	}
}

// TestRunDoctor_DetectsOrphanCanonicalResource ensures doctor surfaces the
// orphan canonical case (canonical exists, no repo-local back-link).
func TestRunDoctor_DetectsOrphanCanonicalResource(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	os.MkdirAll(projectPath, 0755)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Canonical skill exists, no repo-local back-link.
	if err := os.MkdirAll(filepath.Join(agentsHome, "skills", "myproj", "ghostskill"), 0755); err != nil {
		t.Fatal(err)
	}

	// Capture stdout so we can assert the warning message.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = w

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	runErr := runDoctor(NewDoctorCmd(), nil)
	w.Close()
	os.Stdout = oldStdout

	if runErr != nil {
		t.Errorf("runDoctor: %v", runErr)
	}

	buf := make([]byte, 1<<14)
	n, _ := r.Read(buf)
	out := string(buf[:n])
	if !strings.Contains(out, "orphan") {
		t.Errorf("expected 'orphan' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "ghostskill") {
		t.Errorf("expected resource name in output, got:\n%s", out)
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

// TestCountProjectLinks_AllHealthyVariants exercises the cursor global and
// project hardlink "healthy" branches plus the multi single-file symlink
// branches that the prior TestCountProjectLinks_HealthyAndBroken did not cover.
func TestCountProjectLinks_AllHealthyVariants(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	// Cursor global hardlink (.mdc)
	globalSrc := filepath.Join(agentsHome, "rules", "global", "g.mdc")
	os.MkdirAll(filepath.Dir(globalSrc), 0755)
	os.WriteFile(globalSrc, []byte("g"), 0644)
	cursorRules := filepath.Join(projectPath, ".cursor", "rules")
	os.MkdirAll(cursorRules, 0755)
	if err := os.Link(globalSrc, filepath.Join(cursorRules, "global--g.mdc")); err != nil {
		t.Fatal(err)
	}

	// Cursor global hardlink falling back to .md source (file named .mdc on disk but src is .md)
	globalSrcMD := filepath.Join(agentsHome, "rules", "global", "h.md")
	os.WriteFile(globalSrcMD, []byte("h"), 0644)
	if err := os.Link(globalSrcMD, filepath.Join(cursorRules, "global--h.mdc")); err != nil {
		t.Fatal(err)
	}

	// Backup artifact + non-.mdc entries (skipped)
	os.WriteFile(filepath.Join(cursorRules, "global--g.mdc.dot-agents-backup"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(cursorRules, "loose.txt"), []byte("x"), 0644)

	// Claude symlink (healthy)
	claudeTarget := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	os.MkdirAll(filepath.Dir(claudeTarget), 0755)
	os.WriteFile(claudeTarget, []byte("ok"), 0644)
	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	if err := os.Symlink(claudeTarget, filepath.Join(claudeRules, "proj--agents.md")); err != nil {
		t.Fatal(err)
	}

	// Single-file healthy symlinks for all five paths
	type linkPair struct {
		src, dst string
	}
	files := []linkPair{
		{filepath.Join(agentsHome, "rules", "proj", "AGENTS.md"), filepath.Join(projectPath, "AGENTS.md")},
		{filepath.Join(agentsHome, "rules", "proj", "copilot-instructions.md"), filepath.Join(projectPath, ".github", "copilot-instructions.md")},
		{filepath.Join(agentsHome, "settings", "proj", "opencode.json"), filepath.Join(projectPath, "opencode.json")},
		{filepath.Join(agentsHome, "mcp", "proj", "mcp.json"), filepath.Join(projectPath, ".mcp.json")},
		{filepath.Join(agentsHome, "mcp", "proj", "mcp.json.vscode"), filepath.Join(projectPath, ".vscode", "mcp.json")},
	}
	for _, lp := range files {
		os.MkdirAll(filepath.Dir(lp.src), 0755)
		os.WriteFile(lp.src, []byte("ok"), 0644)
		os.MkdirAll(filepath.Dir(lp.dst), 0755)
		if err := os.Symlink(lp.src, lp.dst); err != nil {
			t.Fatal(err)
		}
	}

	ok, broken := countProjectLinks("proj", projectPath, agentsHome)
	if broken != 0 {
		t.Errorf("expected 0 broken, got %d", broken)
	}
	// Expect: 2 cursor + 1 claude + 5 single-file = 8
	if ok < 8 {
		t.Errorf("expected ok>=8, got %d", ok)
	}
}

// TestCountProjectLinks_CursorProjectHardlinkHealthy covers the project--<name>
// cursor hardlink healthy branches (.mdc and .md fallback) — these two
// branches are not hit by the global-prefix tests above.
func TestCountProjectLinks_CursorProjectHardlinkHealthy(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	cursorRules := filepath.Join(projectPath, ".cursor", "rules")
	os.MkdirAll(cursorRules, 0755)

	// Project hardlink (.mdc match)
	src := filepath.Join(agentsHome, "rules", "proj", "p.mdc")
	os.MkdirAll(filepath.Dir(src), 0755)
	os.WriteFile(src, []byte("p"), 0644)
	if err := os.Link(src, filepath.Join(cursorRules, "proj--p.mdc")); err != nil {
		t.Fatal(err)
	}

	// Project hardlink (.md fallback)
	srcMD := filepath.Join(agentsHome, "rules", "proj", "q.md")
	os.WriteFile(srcMD, []byte("q"), 0644)
	if err := os.Link(srcMD, filepath.Join(cursorRules, "proj--q.mdc")); err != nil {
		t.Fatal(err)
	}

	// countProjectLinks counts only global-- cursor in current implementation,
	// but exercises the project-- branch in collectBrokenLinks. Confirm we get
	// no broken entries reported for either pair.
	_, broken := countProjectLinks("proj", projectPath, agentsHome)
	if broken != 0 {
		t.Errorf("expected 0 broken, got %d", broken)
	}
}

// TestRunDoctor_WithInstalledClaudePlatformAndPlugins exercises the full doctor
// loop: an installed claude platform, a registered project, a plugin spec with
// an unsupported platform (triggers warning), and a plugin with opencode that
// links into the project (triggers symlink validation).
func TestRunDoctor_WithInstalledClaudePlatformAndPlugins(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Make claude.IsInstalled() return true via ~/.claude
	os.MkdirAll(filepath.Join(tmp, ".claude"), 0755)

	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	os.MkdirAll(projectPath, 0755)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Plugin: opencode (supported) — register a symlink under the project that
	// points to the canonical plugin dir so the dest-stat branch is hit.
	opencodePluginDir := filepath.Join(agentsHome, "plugins", "global", "demo")
	os.MkdirAll(opencodePluginDir, 0755)
	os.WriteFile(filepath.Join(opencodePluginDir, "PLUGIN.yaml"),
		[]byte("schema_version: 1\nkind: native\nname: demo\nplatforms: [opencode]\n"), 0644)
	pluginLink := filepath.Join(projectPath, ".opencode", "plugins", "demo")
	os.MkdirAll(filepath.Dir(pluginLink), 0755)
	if err := os.Symlink(opencodePluginDir, pluginLink); err != nil {
		t.Fatal(err)
	}

	// Plugin: unsupported platform (triggers the "no emitter implemented yet" warn)
	unsupportedDir := filepath.Join(agentsHome, "plugins", "global", "alien")
	os.MkdirAll(unsupportedDir, 0755)
	os.WriteFile(filepath.Join(unsupportedDir, "PLUGIN.yaml"),
		[]byte("schema_version: 1\nkind: native\nname: alien\nplatforms: [cursor]\n"), 0644)

	// Plugin: opencode with broken symlink (triggers "broken symlink" error path)
	brokenPluginDir := filepath.Join(agentsHome, "plugins", "global", "ghost")
	os.MkdirAll(brokenPluginDir, 0755)
	os.WriteFile(filepath.Join(brokenPluginDir, "PLUGIN.yaml"),
		[]byte("schema_version: 1\nkind: native\nname: ghost\nplatforms: [opencode]\n"), 0644)
	brokenLink := filepath.Join(projectPath, ".opencode", "plugins", "ghost")
	if err := os.Symlink(filepath.Join(agentsHome, "missing"), brokenLink); err != nil {
		t.Fatal(err)
	}

	// Add a healthy AGENTS.md symlink + a broken claude symlink so we exercise
	// the link-health repair path with installed platform.
	target := filepath.Join(agentsHome, "rules", "myproj", "agents.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("# rules"), 0644)
	os.Symlink(target, filepath.Join(projectPath, "AGENTS.md"))

	dangling := filepath.Join(agentsHome, "rules", "myproj", "missing.md")
	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	os.Symlink(dangling, filepath.Join(claudeRules, "myproj--missing.md"))

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	runErr := runDoctor(NewDoctorCmd(), nil)
	w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 1<<16)
	n, _ := r.Read(buf)
	out := string(buf[:n])

	if runErr != nil {
		t.Errorf("runDoctor: %v", runErr)
	}
	// Plugin warnings should mention the alien spec
	if !strings.Contains(out, "alien") {
		t.Errorf("expected alien plugin to be mentioned, got:\n%s", out)
	}
}

// TestRunDoctor_VerboseWithHealthyAndManifest covers the verbose-mode rendering
// for projects whose manifest references no git source (local manifest "ok"
// branch) and projects with no links yet.
func TestRunDoctor_VerboseWithHealthyAndManifest(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)
	// Local manifest with no sources
	rc := &config.AgentsRC{Version: 1, Project: "p"}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Verbose: true}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor verbose: %v", err)
	}
}

// TestPrintUserConfigStatus_BrokenSymlinks covers all the broken-symlink
// branches in printUserConfigStatus (the verbose user-config detail printer).
func TestPrintUserConfigStatus_BrokenSymlinks(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Build broken symlinks across all five tracked user-config locations.
	dangling := filepath.Join(agentsHome, "no-such-target")

	claudeHome := filepath.Join(tmp, ".claude")
	os.MkdirAll(filepath.Join(claudeHome, "agents"), 0755)
	os.MkdirAll(filepath.Join(claudeHome, "skills"), 0755)
	os.Symlink(dangling, filepath.Join(claudeHome, "CLAUDE.md"))
	os.Symlink(dangling, filepath.Join(claudeHome, "settings.json"))
	os.Symlink(dangling, filepath.Join(claudeHome, "agents", "a1"))
	os.Symlink(dangling, filepath.Join(claudeHome, "skills", "s1"))

	codexAgents := filepath.Join(tmp, ".codex", "agents")
	os.MkdirAll(codexAgents, 0755)
	os.Symlink(dangling, filepath.Join(codexAgents, "c1"))

	opencodeAgents := filepath.Join(tmp, ".opencode", "agent")
	os.MkdirAll(opencodeAgents, 0755)
	os.Symlink(dangling, filepath.Join(opencodeAgents, "o1"))

	// Should print without panicking even with all broken
	printUserConfigStatus(agentsHome)
}

// TestPrintUserConfigStatus_LocalFiles covers the "local file" (not a symlink)
// branches in printUserConfigStatus.
func TestPrintUserConfigStatus_LocalFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	claudeHome := filepath.Join(tmp, ".claude")
	os.MkdirAll(claudeHome, 0755)
	// Regular files (not symlinks)
	os.WriteFile(filepath.Join(claudeHome, "CLAUDE.md"), []byte("# local"), 0644)
	os.WriteFile(filepath.Join(claudeHome, "settings.json"), []byte("{}"), 0644)

	printUserConfigStatus(agentsHome)
}

// TestRunDoctor_RepairBrokenLinksWithInstalledClaude covers the broken-link
// repair branch in non-dry-run mode: doctor reruns CreateLinks for the affected
// platform and reports "repaired".
func TestRunDoctor_RepairBrokenLinksWithInstalledClaude(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Make claude installed
	os.MkdirAll(filepath.Join(tmp, ".claude"), 0755)

	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	os.MkdirAll(projectPath, 0755)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Introduce a broken claude rule symlink — repair should re-run CreateLinks.
	dangling := filepath.Join(agentsHome, "rules", "myproj", "missing.md")
	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	os.Symlink(dangling, filepath.Join(claudeRules, "myproj--missing.md"))

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(), nil); err != nil {
		t.Errorf("runDoctor with broken links + installed claude: %v", err)
	}
}
