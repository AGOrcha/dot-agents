package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/linktest"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/spf13/cobra"
)

// testDoctorDeps returns a lifecycle.Deps suitable for doctor_test exercises.
// Mirrors testStatusDeps in status_test.go: UsageError/ErrorWithHints forward
// to fmt.Errorf, the *WithHints validators always succeed, and ExampleBlock
// joins with newlines.
func testDoctorDeps() Deps {
	accept := func(*cobra.Command, []string) error { return nil }
	usage := func(msg string, hints ...string) error { return fmt.Errorf("%s", msg) }
	return Deps{
		Flags:                 GlobalFlags{},
		ErrorWithHints:        func(msg string, hints ...string) error { return fmt.Errorf("%s", msg) },
		UsageError:            usage,
		MaximumNArgsWithHints: func(int, ...string) cobra.PositionalArgs { return accept },
		RangeArgsWithHints:    func(int, int, ...string) cobra.PositionalArgs { return accept },
		ExactArgsWithHints:    func(int, ...string) cobra.PositionalArgs { return accept },
		// doctor's Args uses NoArgsWithHints — return a validator that
		// rejects any positional args so TestNewDoctorCmd_Metadata's
		// "doctor takes no args" assertion holds without depending on the
		// real commands.NoArgsWithHints implementation.
		NoArgsWithHints: func(hints ...string) cobra.PositionalArgs {
			return func(cmd *cobra.Command, args []string) error {
				if len(args) == 0 {
					return nil
				}
				return fmt.Errorf("doctor takes no positional arguments")
			}
		},
		ExampleBlock: func(lines ...string) string { return strings.Join(lines, "\n") },
	}
}

// fakeDoctorConfigLoader is the interface-DI test double for
// DoctorConfigLoader (per docs/TEST_SEAMS.md). A nil func field delegates
// to the real config.Load implementation.
type fakeDoctorConfigLoader struct {
	loadConfig func() (*config.Config, error)
}

func (f fakeDoctorConfigLoader) LoadConfig() (*config.Config, error) {
	if f.loadConfig != nil {
		return f.loadConfig()
	}
	return config.Load()
}

// TestFakeDoctorConfigLoader_NilDelegatesToReal pins the nil-delegates-to-real
// contract so tests that omit loadConfig hit the real config.Load.
func TestFakeDoctorConfigLoader_NilDelegatesToReal(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := (fakeDoctorConfigLoader{}).LoadConfig()
	if err != nil {
		t.Fatalf("nil-loadConfig delegate: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected real config.Load result, got nil")
	}
}

// TestNewDoctorCmd_RunEClosureWiresStdDeps drives doctor's RunE closure
// end to end so a regression in std deps wiring fails here.
func TestNewDoctorCmd_RunEClosureWiresStdDeps(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	cmd := NewDoctorCmd(testDoctorDeps())
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE closure: %v", err)
	}
}

// TestNewDoctorCmd_RunEAppliesDepsToGlobals pins the t13a-introduced
// RunE wrapper contract: applyDepsToGlobals must fire before runDoctor
// so deps.FlagsFn / .Version (etc.) reach the lifecycle package vars
// without the parent shim's syncLifecycleGlobals. Mirrors
// TestNewInstallCmd_RunEAppliesDepsToGlobals at the doctor seat.
//
// Doctor's reportLinkHealth path reads Flags.Verbose / Flags.DryRun
// directly through the moved body; we exercise the wrapper's sync by
// passing FlagsFn that returns Verbose=true, then run RunE and assert
// the package var was populated. The doctor body itself is hermetic
// against an empty config so the test does not need to seed projects.
func TestNewDoctorCmd_RunEAppliesDepsToGlobals(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	savedV := Version
	Flags = GlobalFlags{}
	Version = "sentinel-stale"
	defer func() {
		Flags = saved
		Version = savedV
	}()

	deps := testDoctorDeps()
	deps.FlagsFn = func() GlobalFlags { return GlobalFlags{Verbose: true} }
	deps.Version = "1.0.0-doctor"

	cmd := NewDoctorCmd(deps)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !Flags.Verbose {
		t.Errorf("RunE wrapper did not propagate FlagsFn().Verbose; got %+v", Flags)
	}
	if Version != "1.0.0-doctor" {
		t.Errorf("RunE wrapper did not propagate Version; got %q", Version)
	}
}

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
	linktest.Link(t, target, filepath.Join(claudeRules, "proj--agents.md"))

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
	linktest.DanglingLink(t, filepath.Join(claudeRules, "proj--ghost.md"))

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

	linktest.DanglingLink(t, filepath.Join(projectPath, "AGENTS.md"))

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

	linktest.DanglingLink(t, filepath.Join(ghDir, "copilot-instructions.md"))

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

	linktest.DanglingLink(t, filepath.Join(vsDir, "mcp.json"))

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

	linktest.DanglingLink(t, filepath.Join(projectPath, ".mcp.json"))

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

	linktest.DanglingLink(t, filepath.Join(projectPath, "opencode.json"))

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
	linktest.Link(t, target, filepath.Join(claudeRules, "proj--agents.md"))

	// Broken AGENTS.md
	linktest.DanglingLink(t, filepath.Join(projectPath, "AGENTS.md"))

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
	linktest.DanglingLink(t, filepath.Join(claudeHome, "CLAUDE.md"))

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
	linktest.DanglingLink(t, filepath.Join(agentsSubDir, "ghost.md"))

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
	linktest.DanglingLink(t, filepath.Join(codexAgentsDir, "missing"))

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
	linktest.DanglingLink(t, filepath.Join(ocDir, "missing.md"))

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

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
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
	linktest.DanglingLink(t, filepath.Join(claudeHome, "settings.json"))

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
	linktest.DanglingLink(t, filepath.Join(skillsDir, "ghost"))

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
	linktest.Link(t, target, filepath.Join(projectPath, "AGENTS.md"))

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

	target := filepath.Join(agentsHome, "rules", "global", "CLAUDE.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("# claude"), 0644)
	claudeHome := filepath.Join(tmp, ".claude")
	os.MkdirAll(claudeHome, 0755)
	linktest.Link(t, target, filepath.Join(claudeHome, "CLAUDE.md"))

	os.WriteFile(filepath.Join(claudeHome, "settings.json"), []byte("{}"), 0644)

	agentsDir := filepath.Join(claudeHome, "agents")
	os.MkdirAll(agentsDir, 0755)
	linktest.DanglingLink(t, filepath.Join(agentsDir, "ghost.md"))

	skillTarget := filepath.Join(agentsHome, "skills", "global", "demo", "SKILL.md")
	os.MkdirAll(filepath.Dir(skillTarget), 0755)
	os.WriteFile(skillTarget, []byte("ok"), 0644)
	skillsDir := filepath.Join(claudeHome, "skills")
	os.MkdirAll(skillsDir, 0755)
	linktest.Link(t, skillTarget, filepath.Join(skillsDir, "demo"))

	codexDir := filepath.Join(tmp, ".codex", "agents")
	os.MkdirAll(codexDir, 0755)
	linktest.DanglingLink(t, filepath.Join(codexDir, "ghost"))

	opencodeDir := filepath.Join(tmp, ".opencode", "agent")
	os.MkdirAll(opencodeDir, 0755)
	ocTarget := filepath.Join(agentsHome, "agents", "global", "demo", "AGENT.md")
	os.MkdirAll(filepath.Dir(ocTarget), 0755)
	os.WriteFile(ocTarget, []byte("ok"), 0644)
	linktest.Link(t, ocTarget, filepath.Join(opencodeDir, "demo.md"))

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

	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
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
	os.WriteFile(filepath.Join(projectPath, ".agentsrc.json"), []byte("not json"), 0644)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
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

	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
		t.Errorf("runDoctor with unfetched git source: %v", err)
	}
}

// TestRunDoctor_GitSourceCachePresent covers the presentGit append + ok
// manifest branch.
func TestRunDoctor_GitSourceCachePresent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)
	cacheRoot := filepath.Join(tmp, ".cache")
	t.Setenv("XDG_CACHE_HOME", cacheRoot)

	projPath := filepath.Join(tmp, "p")
	os.MkdirAll(projPath, 0755)
	url := "https://example.invalid/cached.git"
	rc := &config.AgentsRC{Version: 1, Project: "p", Sources: []config.Source{{Type: "git", URL: url}}}
	if err := rc.Save(projPath); err != nil {
		t.Fatal(err)
	}

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
	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
		t.Errorf("runDoctor with cached git source: %v", err)
	}
}

// TestRunDoctor_NoAgentsHome covers the early "~/.agents/ not found" branch
// and the absent-config warning branch.
func TestRunDoctor_NoAgentsHomeAndNoConfig(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, "absent-agents-home"))

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
		t.Errorf("runDoctor with absent home: %v", err)
	}
}

// TestRunDoctor_BrokenUserLinksReportedNonVerbose covers the non-verbose
// broken-user-link rendering loop.
func TestRunDoctor_BrokenUserLinksReportedNonVerbose(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	claudeHome := filepath.Join(tmp, ".claude")
	os.MkdirAll(claudeHome, 0755)
	linktest.DanglingLink(t, filepath.Join(claudeHome, "CLAUDE.md"))

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()
	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
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

	pluginDir := filepath.Join(agentsHome, "plugins", "global", "demo")
	os.MkdirAll(pluginDir, 0755)
	os.WriteFile(filepath.Join(pluginDir, "PLUGIN.yaml"),
		[]byte("schema_version: 1\nkind: native\nname: demo\nplatforms: [claude, opencode]\n"), 0644)

	projPath := filepath.Join(tmp, "p")
	os.MkdirAll(projPath, 0755)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	pluginLink := filepath.Join(projPath, ".opencode", "plugins", "demo")
	os.MkdirAll(filepath.Dir(pluginLink), 0755)
	linktest.DanglingLink(t, pluginLink)

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
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

	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
		t.Errorf("runDoctor orphan: %v", err)
	}
}

// TestRunDoctor_ReportsBrokenClaudeRuleLink asserts doctor surfaces a broken
// .claude/rules managed link and completes without error. Doctor is read-only
// (§7A.6) so it reports the breakage but does not repair it.
func TestRunDoctor_ReportsBrokenClaudeRuleLink(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignalLifecycle(t, tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	projPath := filepath.Join(tmp, "p")
	claudeRules := filepath.Join(projPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	linktest.DanglingLink(t, filepath.Join(claudeRules, "p--ghost.md"))

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	out := captureDoctorOutput(t, func() {
		if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
			t.Errorf("runDoctor: %v", err)
		}
	})
	if !strings.Contains(out, "broken") {
		t.Fatalf("doctor should report the broken claude rule link; output:\n%s", out)
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

	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
		t.Errorf("runDoctor with missing project dir: %v", err)
	}
}

func TestNewDoctorCmd_Metadata(t *testing.T) {
	cmd := NewDoctorCmd(testDoctorDeps())
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

	repoLocal := filepath.Join(projectPath, ".agents", "skills")
	os.MkdirAll(repoLocal, 0755)
	linktest.Link(t, filepath.Join(canonicalBase, "beta"), filepath.Join(repoLocal, "beta"))

	got := collectOrphanCanonicals("proj", projectPath, agentsHome, "skills")
	if len(got) != 1 || got[0] != "alpha" {
		t.Errorf("expected ['alpha'], got %v", got)
	}

	if got := collectOrphanCanonicals("proj", projectPath, agentsHome, "missing"); got != nil {
		t.Errorf("expected nil for missing canonical bucket, got %v", got)
	}
}

// TestCollectOrphanCanonicals_DetectsMispointedSymlink verifies that a
// back-link symlink whose target is NOT the matching canonical entry is
// still reported as an orphan.
func TestCollectOrphanCanonicals_DetectsMispointedSymlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	canonicalBase := filepath.Join(agentsHome, "skills", "proj")
	if err := os.MkdirAll(filepath.Join(canonicalBase, "gamma"), 0755); err != nil {
		t.Fatal(err)
	}
	otherCanonical := filepath.Join(agentsHome, "skills", "otherproj", "delta")
	if err := os.MkdirAll(otherCanonical, 0755); err != nil {
		t.Fatal(err)
	}

	repoLocal := filepath.Join(projectPath, ".agents", "skills")
	os.MkdirAll(repoLocal, 0755)
	linktest.Link(t, otherCanonical, filepath.Join(repoLocal, "gamma"))

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
// reported.
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
	linktest.Link(t, canonical, filepath.Join(repoLocal, "epsilon"))

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

	if err := os.MkdirAll(filepath.Join(agentsHome, "skills", "myproj", "ghostskill"), 0755); err != nil {
		t.Fatal(err)
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = w

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	runErr := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{})
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
	if !strings.Contains(out, "ln -s") {
		t.Errorf("expected 'ln -s' restore hint, got:\n%s", out)
	}
	if !strings.Contains(out, "rm -rf") {
		t.Errorf("expected 'rm -rf' purge hint, got:\n%s", out)
	}
	if strings.Contains(out, "promote --force") {
		t.Errorf("stale promote --force hint should not appear, got:\n%s", out)
	}
}

// TestRunDoctor_ReportsBrokenAgentsMD asserts doctor reports a broken AGENTS.md
// managed link and leaves the project tree unchanged (read-only, §7A.6).
func TestRunDoctor_ReportsBrokenAgentsMD(t *testing.T) {
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

	linktest.DanglingLink(t, filepath.Join(projectPath, "AGENTS.md"))

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	before := doctorE2ESnapshotTree(t, projectPath)
	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
		t.Errorf("runDoctor with broken link: %v", err)
	}
	after := doctorE2ESnapshotTree(t, projectPath)
	if msg, ok := doctorE2ESnapshotsEqual(before, after); !ok {
		t.Fatalf("doctor must be read-only but mutated the project repo: %s", msg)
	}
}

// TestCountProjectLinks_AllHealthyVariants exercises the cursor global and
// project hardlink "healthy" branches plus the multi single-file symlink
// branches.
func TestCountProjectLinks_AllHealthyVariants(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	globalSrc := filepath.Join(agentsHome, "rules", "global", "g.mdc")
	os.MkdirAll(filepath.Dir(globalSrc), 0755)
	os.WriteFile(globalSrc, []byte("g"), 0644)
	cursorRules := filepath.Join(projectPath, ".cursor", "rules")
	os.MkdirAll(cursorRules, 0755)
	if err := os.Link(globalSrc, filepath.Join(cursorRules, "global--g.mdc")); err != nil {
		t.Fatal(err)
	}

	globalSrcMD := filepath.Join(agentsHome, "rules", "global", "h.md")
	os.WriteFile(globalSrcMD, []byte("h"), 0644)
	if err := os.Link(globalSrcMD, filepath.Join(cursorRules, "global--h.mdc")); err != nil {
		t.Fatal(err)
	}

	os.WriteFile(filepath.Join(cursorRules, "global--g.mdc.dot-agents-backup"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(cursorRules, "loose.txt"), []byte("x"), 0644)

	claudeTarget := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	os.MkdirAll(filepath.Dir(claudeTarget), 0755)
	os.WriteFile(claudeTarget, []byte("ok"), 0644)
	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	linktest.Link(t, claudeTarget, filepath.Join(claudeRules, "proj--agents.md"))

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
		linktest.Link(t, lp.src, lp.dst)
	}

	ok, broken := countProjectLinks("proj", projectPath, agentsHome)
	if broken != 0 {
		t.Errorf("expected 0 broken, got %d", broken)
	}
	if ok < 8 {
		t.Errorf("expected ok>=8, got %d", ok)
	}
}

// TestCountProjectLinks_CursorProjectHardlinkHealthy covers the project--<name>
// cursor hardlink healthy branches (.mdc and .md fallback).
func TestCountProjectLinks_CursorProjectHardlinkHealthy(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	cursorRules := filepath.Join(projectPath, ".cursor", "rules")
	os.MkdirAll(cursorRules, 0755)

	src := filepath.Join(agentsHome, "rules", "proj", "p.mdc")
	os.MkdirAll(filepath.Dir(src), 0755)
	os.WriteFile(src, []byte("p"), 0644)
	if err := os.Link(src, filepath.Join(cursorRules, "proj--p.mdc")); err != nil {
		t.Fatal(err)
	}

	srcMD := filepath.Join(agentsHome, "rules", "proj", "q.md")
	os.WriteFile(srcMD, []byte("q"), 0644)
	if err := os.Link(srcMD, filepath.Join(cursorRules, "proj--q.mdc")); err != nil {
		t.Fatal(err)
	}

	_, broken := countProjectLinks("proj", projectPath, agentsHome)
	if broken != 0 {
		t.Errorf("expected 0 broken, got %d", broken)
	}
}

// TestRunDoctor_WithInstalledClaudePlatformAndPlugins exercises the full doctor
// loop with installed claude + plugin specs + symlink.
func TestRunDoctor_WithInstalledClaudePlatformAndPlugins(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignalLifecycle(t, tmp)

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

	opencodePluginDir := filepath.Join(agentsHome, "plugins", "global", "demo")
	os.MkdirAll(opencodePluginDir, 0755)
	os.WriteFile(filepath.Join(opencodePluginDir, "PLUGIN.yaml"),
		[]byte("schema_version: 1\nkind: native\nname: demo\nplatforms: [opencode]\n"), 0644)
	pluginLink := filepath.Join(projectPath, ".opencode", "plugins", "demo")
	os.MkdirAll(filepath.Dir(pluginLink), 0755)
	linktest.Link(t, opencodePluginDir, pluginLink)

	unsupportedDir := filepath.Join(agentsHome, "plugins", "global", "alien")
	os.MkdirAll(unsupportedDir, 0755)
	os.WriteFile(filepath.Join(unsupportedDir, "PLUGIN.yaml"),
		[]byte("schema_version: 1\nkind: native\nname: alien\nplatforms: [cursor]\n"), 0644)

	brokenPluginDir := filepath.Join(agentsHome, "plugins", "global", "ghost")
	os.MkdirAll(brokenPluginDir, 0755)
	os.WriteFile(filepath.Join(brokenPluginDir, "PLUGIN.yaml"),
		[]byte("schema_version: 1\nkind: native\nname: ghost\nplatforms: [opencode]\n"), 0644)
	brokenPluginLink := filepath.Join(projectPath, ".opencode", "plugins", "ghost")
	linktest.DanglingLink(t, brokenPluginLink)

	target := filepath.Join(agentsHome, "rules", "myproj", "agents.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("# rules"), 0644)
	linktest.Link(t, target, filepath.Join(projectPath, "AGENTS.md"))

	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	linktest.DanglingLink(t, filepath.Join(claudeRules, "myproj--missing.md"))

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	r, w, _ := os.Pipe()
	oldStdout := os.Stdout
	os.Stdout = w
	runErr := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{})
	w.Close()
	os.Stdout = oldStdout

	buf := make([]byte, 1<<16)
	n, _ := r.Read(buf)
	out := string(buf[:n])

	if runErr != nil {
		t.Errorf("runDoctor: %v", runErr)
	}
	if !strings.Contains(out, "alien") {
		t.Errorf("expected alien plugin to be mentioned, got:\n%s", out)
	}
}

// TestRunDoctor_VerboseWithHealthyAndManifest covers verbose-mode rendering
// for projects whose manifest references no git source.
func TestRunDoctor_VerboseWithHealthyAndManifest(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)
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

	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
		t.Errorf("runDoctor verbose: %v", err)
	}
}

// TestPrintUserConfigStatus_BrokenSymlinks covers all broken-symlink
// branches in the verbose user-config detail printer.
func TestPrintUserConfigStatus_BrokenSymlinks(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	claudeHome := filepath.Join(tmp, ".claude")
	os.MkdirAll(filepath.Join(claudeHome, "agents"), 0755)
	os.MkdirAll(filepath.Join(claudeHome, "skills"), 0755)
	linktest.DanglingLink(t, filepath.Join(claudeHome, "CLAUDE.md"))
	linktest.DanglingLink(t, filepath.Join(claudeHome, "settings.json"))
	linktest.DanglingLink(t, filepath.Join(claudeHome, "agents", "a1"))
	linktest.DanglingLink(t, filepath.Join(claudeHome, "skills", "s1"))

	codexAgents := filepath.Join(tmp, ".codex", "agents")
	os.MkdirAll(codexAgents, 0755)
	linktest.DanglingLink(t, filepath.Join(codexAgents, "c1"))

	opencodeAgents := filepath.Join(tmp, ".opencode", "agent")
	os.MkdirAll(opencodeAgents, 0755)
	linktest.DanglingLink(t, filepath.Join(opencodeAgents, "o1"))

	printUserConfigStatus(agentsHome)
}

// TestPrintUserConfigStatus_LocalFiles covers the "local file" branches.
func TestPrintUserConfigStatus_LocalFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	claudeHome := filepath.Join(tmp, ".claude")
	os.MkdirAll(claudeHome, 0755)
	os.WriteFile(filepath.Join(claudeHome, "CLAUDE.md"), []byte("# local"), 0644)
	os.WriteFile(filepath.Join(claudeHome, "settings.json"), []byte("{}"), 0644)

	printUserConfigStatus(agentsHome)
}

// TestRunDoctor_RepairBrokenLinksWithInstalledClaude covers the broken-link
// repair branch in non-dry-run mode.
// TestRunDoctor_BrokenLinksWithInstalledClaudeIsReadOnly asserts that, even
// with a platform installed (so the old doctor would have re-run CreateLinks),
// doctor only detects the broken managed link and leaves the project tree
// byte-identical — the §7A.6 read-only contract.
func TestRunDoctor_BrokenLinksWithInstalledClaudeIsReadOnly(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignalLifecycle(t, tmp)

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

	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	linktest.DanglingLink(t, filepath.Join(claudeRules, "myproj--missing.md"))

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	before := doctorE2ESnapshotTree(t, projectPath)
	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
		t.Errorf("runDoctor with broken links + installed claude: %v", err)
	}
	after := doctorE2ESnapshotTree(t, projectPath)
	if msg, ok := doctorE2ESnapshotsEqual(before, after); !ok {
		t.Fatalf("doctor must be read-only but mutated the project repo: %s", msg)
	}
}

// TestRunDoctor_WithClaudeVersionShimCoversInstalledWithVersionBranch covers
// the claude `--version` "if-branch" in reportPlatformInventory.
func TestRunDoctor_WithClaudeVersionShimCoversInstalledWithVersionBranch(t *testing.T) {
	tmp := seedAllPlatformInstallSignalsLifecycle(t)

	binDir := filepath.Join(tmp, "fakebin")
	claudeShim := filepath.Join(binDir, "claude")
	if err := os.WriteFile(claudeShim, []byte("#!/bin/sh\necho 'claude 1.2.3 (ci-drift)'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runDoctor(nil, nil, StdDoctorConfigLoader{}); err != nil {
		t.Errorf("runDoctor with claude version shim: %v", err)
	}
}

// TestPrintUserConfigStatus_BrokenSymlinksCIDrift covers the broken-symlink
// branches in printUserConfigStatus (broken claude settings, claude agents,
// codex agents, claude skills).
func TestPrintUserConfigStatus_BrokenSymlinksCIDrift(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	claudeDir := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linktest.DanglingLink(t, filepath.Join(claudeDir, "settings.json"))

	claudeAgentsDir := filepath.Join(claudeDir, "agents")
	if err := os.MkdirAll(claudeAgentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linktest.DanglingLink(t, filepath.Join(claudeAgentsDir, "demo"))

	claudeSkillsDir := filepath.Join(claudeDir, "skills")
	if err := os.MkdirAll(claudeSkillsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linktest.DanglingLink(t, filepath.Join(claudeSkillsDir, "demo"))

	codexAgentsDir := filepath.Join(tmp, ".codex", "agents")
	if err := os.MkdirAll(codexAgentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linktest.DanglingLink(t, filepath.Join(codexAgentsDir, "demo"))

	printUserConfigStatus(agentsHome)
}

// TestCollectBrokenUserLinks_BrokenClaudeMDCIDrift covers the broken-symlink
// branch inside collectBrokenUserLinks for the CLAUDE.md path.
func TestCollectBrokenUserLinks_BrokenClaudeMDCIDrift(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	claudeDir := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	linktest.DanglingLink(t, filepath.Join(claudeDir, "CLAUDE.md"))

	got := collectBrokenUserLinks(agentsHome)

	_ = got
}

// TestRepairManagedProject_DryRunLines covers the repair-dry-run branch.
// TestRunDoctor_VerboseWithHealthyLinks covers the verbose-mode printAudit
// branch when a project has healthy managed links (len(brokenLinks)==0,
// total>0). Without this the line-229 verbose+healthy branch is uncovered.
func TestRunDoctor_VerboseWithHealthyLinks(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0o755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	os.MkdirAll(projectPath, 0o755)
	// One healthy claude rule symlink so total>0 and len(broken)==0.
	target := filepath.Join(agentsHome, "rules", "myproj", "agents.md")
	os.MkdirAll(filepath.Dir(target), 0o755)
	os.WriteFile(target, []byte("ok"), 0o644)
	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0o755)
	linktest.Link(t, target, filepath.Join(claudeRules, "myproj--agents.md"))

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Verbose: true}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
		t.Errorf("runDoctor verbose-healthy: %v", err)
	}
}

// TestRunDoctor_VerboseWithBrokenLinks covers the verbose-mode printAudit
// branch when a project has broken links (len(brokenLinks)>0). Without
// this the line-237 verbose+broken branch is uncovered.
func TestRunDoctor_VerboseWithBrokenLinks(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0o755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	os.MkdirAll(projectPath, 0o755)
	// Broken AGENTS.md → reportOneProjectLinkHealth verbose-broken path.
	linktest.DanglingLink(t, filepath.Join(projectPath, "AGENTS.md"))

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Verbose: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runDoctor(NewDoctorCmd(testDoctorDeps()), nil, StdDoctorConfigLoader{}); err != nil {
		t.Errorf("runDoctor verbose-broken: %v", err)
	}
}

// TestRunDoctor_ConfigLoadError covers the deps.LoadConfig() != nil branch
// in runDoctor (the moved counterpart of the pre-t09 seams_test.go
// TestRunDoctor_ConfigLoadError).
func TestRunDoctor_ConfigLoadError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	deps := fakeDoctorConfigLoader{loadConfig: func() (*config.Config, error) {
		return nil, fmt.Errorf("load boom")
	}}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := runDoctor(nil, nil, deps); err != nil {
		t.Fatalf("runDoctor expected nil on configLoad err, got %v", err)
	}
}

// TestRunDoctor_TrampolineWired pins that the exported RunDoctor entry
// point (used by commands/doctor.go's root shim before t11 splits
// seams_test.go) forwards through to the package-private runDoctor.
// Mirrors status_exports_test.go's TestRunStatus_ExportTrampoline.
func TestRunDoctor_TrampolineWired(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatalf("mkdir agentsHome: %v", err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save: %v", err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	if err := RunDoctor(nil, nil, StdDoctorConfigLoader{}); err != nil {
		t.Errorf("RunDoctor trampoline: %v", err)
	}
}

// TestClaudeRuleHardlinked_AllBranches covers the global / project-- /
// default switch arms plus the .mdc and .md fallback paths inside
// claudeRuleHardlinked.
func TestClaudeRuleHardlinked_AllBranches(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")

	// default branch: neither prefix matches → false.
	if claudeRuleHardlinked(filepath.Join(tmp, "x"), "weird-name.md", "proj", agentsHome) {
		t.Error("expected false for unmatched prefix")
	}

	// global-- with no source file → false (both AreHardlinked calls fail).
	globalEntry := filepath.Join(tmp, "g.mdc")
	os.WriteFile(globalEntry, []byte("x"), 0o644)
	if claudeRuleHardlinked(globalEntry, "global--g.mdc", "proj", agentsHome) {
		t.Error("expected false for global-- with no canonical source")
	}

	// global-- with a .mdc source hardlinked → true.
	globalSrcMDC := filepath.Join(agentsHome, "rules", "global", "ok.mdc")
	os.MkdirAll(filepath.Dir(globalSrcMDC), 0o755)
	os.WriteFile(globalSrcMDC, []byte("ok"), 0o644)
	globalLinkMDC := filepath.Join(tmp, "global--ok.mdc")
	if err := os.Link(globalSrcMDC, globalLinkMDC); err != nil {
		t.Fatal(err)
	}
	if !claudeRuleHardlinked(globalLinkMDC, "global--ok.mdc", "proj", agentsHome) {
		t.Error("expected true for global-- .mdc hardlink")
	}

	// global-- with .md fallback → true.
	globalSrcMD := filepath.Join(agentsHome, "rules", "global", "fb.md")
	os.WriteFile(globalSrcMD, []byte("ok"), 0o644)
	globalLinkFB := filepath.Join(tmp, "global--fb.mdc")
	if err := os.Link(globalSrcMD, globalLinkFB); err != nil {
		t.Fatal(err)
	}
	if !claudeRuleHardlinked(globalLinkFB, "global--fb.mdc", "proj", agentsHome) {
		t.Error("expected true for global-- .md fallback")
	}

	// project-- with .mdc hardlink → true.
	projSrcMDC := filepath.Join(agentsHome, "rules", "proj", "p.mdc")
	os.MkdirAll(filepath.Dir(projSrcMDC), 0o755)
	os.WriteFile(projSrcMDC, []byte("p"), 0o644)
	projLinkMDC := filepath.Join(tmp, "proj--p.mdc")
	if err := os.Link(projSrcMDC, projLinkMDC); err != nil {
		t.Fatal(err)
	}
	if !claudeRuleHardlinked(projLinkMDC, "proj--p.mdc", "proj", agentsHome) {
		t.Error("expected true for project-- .mdc hardlink")
	}
}

// TestReportOnePluginSpec_ContinueBranches drives reportOnePluginSpec
// branches that the broader runDoctor tests do not reach: a spec with no
// opencode platform (early return), and a project with empty path
// (continue).
func TestReportOnePluginSpec_ContinueBranches(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0o755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("ghost", "") // empty path so projectPath == "" continues
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Spec with no opencode platform → hasPluginPlatform returns false →
	// function returns early after the warn loop.
	specNoOC := platform.PluginSpec{Scope: "global", Name: "demo", Platforms: []string{"cursor"}}
	reportOnePluginSpec(specNoOC, cfg, []string{"ghost"})

	// Spec with opencode + a project whose path is empty → the for-range
	// projectPath == "" continue branch.
	specOC := platform.PluginSpec{Scope: "global", Name: "demo2", Platforms: []string{"opencode"}}
	reportOnePluginSpec(specOC, cfg, []string{"ghost"})
}

// ---------- .agentsrc.lock health (config-v2 p2) ----------

// seedDoctorLockProject writes a manifest and (optionally) a committed
// .agentsrc.lock into a fresh project dir, returning the path.
func seedDoctorLockProject(t *testing.T, manifest string, layers map[string]config.LockedLayer) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.AgentsRCFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if layers != nil {
		if err := config.WriteConfigLock(dir, layers); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestReportOneProjectLockHealth_NoExtendsNotApplicable(t *testing.T) {
	dir := seedDoctorLockProject(t, `{"version":2}`, nil)
	applicable, issue := false, false
	out := captureDoctorOutput(t, func() {
		applicable, issue = reportOneProjectLockHealth("p", dir)
	})
	if applicable || issue {
		t.Errorf("expected not applicable/no issue, got applicable=%v issue=%v", applicable, issue)
	}
	if out != "" {
		t.Errorf("expected no bullet for no-extends project, got %q", out)
	}
}

func TestReportOneProjectLockHealth_MissingManifestSilent(t *testing.T) {
	dir := t.TempDir() // no manifest
	applicable, issue := false, false
	captureDoctorOutput(t, func() {
		applicable, issue = reportOneProjectLockHealth("p", dir)
	})
	if applicable || issue {
		t.Errorf("missing manifest must be silent here, got applicable=%v issue=%v", applicable, issue)
	}
}

func TestReportOneProjectLockHealth_NoLockWarns(t *testing.T) {
	dir := seedDoctorLockProject(t, `{"extends":["acme:org/base.json"]}`, nil)
	var applicable, issue bool
	out := captureDoctorOutput(t, func() {
		applicable, issue = reportOneProjectLockHealth("p", dir)
	})
	if !applicable || !issue {
		t.Errorf("expected applicable && issue, got applicable=%v issue=%v", applicable, issue)
	}
	if !strings.Contains(out, "no .agentsrc.lock") {
		t.Errorf("expected missing-lock warning, got %q", out)
	}
}

func TestReportOneProjectLockHealth_HealthyLock(t *testing.T) {
	dir := seedDoctorLockProject(t, `{"extends":["acme:org/base.json"]}`, map[string]config.LockedLayer{
		"acme:org/base.json": {ResolvedSHA: "a1", FetchedAt: "t"},
	})
	var applicable, issue bool
	out := captureDoctorOutput(t, func() {
		applicable, issue = reportOneProjectLockHealth("p", dir)
	})
	if !applicable || issue {
		t.Errorf("expected applicable && no issue, got applicable=%v issue=%v", applicable, issue)
	}
	if !strings.Contains(out, "unit(s) locked") {
		t.Errorf("expected healthy lock bullet, got %q", out)
	}
}

func TestReportOneProjectLockHealth_DriftWarns(t *testing.T) {
	dir := seedDoctorLockProject(t, `{"extends":["acme:org/base.json","acme:org/missing.json"]}`, map[string]config.LockedLayer{
		"acme:org/base.json": {ResolvedSHA: "a1", FetchedAt: "t"},
	})
	var issue bool
	out := captureDoctorOutput(t, func() {
		_, issue = reportOneProjectLockHealth("p", dir)
	})
	if !issue {
		t.Error("expected issue=true for drifted lock")
	}
	if !strings.Contains(out, "acme:org/missing.json") || !strings.Contains(out, "da config sync") {
		t.Errorf("expected per-layer drift warning with hint, got %q", out)
	}
}

func TestReportLockHealth_AllProjectsLocalNoApplicable(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	dir := seedDoctorLockProject(t, `{"version":2}`, nil)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", dir)

	out := captureDoctorOutput(t, func() { reportLockHealth(cfg, cfg.ListProjects()) })
	if !strings.Contains(out, "not applicable") {
		t.Errorf("expected not-applicable summary, got %q", out)
	}
}

func TestReportLockHealth_AllFreshSummary(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	dir := seedDoctorLockProject(t, `{"extends":["acme:org/base.json"]}`, map[string]config.LockedLayer{
		"acme:org/base.json": {ResolvedSHA: "a1", FetchedAt: "t"},
	})
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", dir)

	out := captureDoctorOutput(t, func() { reportLockHealth(cfg, cfg.ListProjects()) })
	if !strings.Contains(out, "unit(s) locked") {
		t.Errorf("expected per-project fresh bullet, got %q", out)
	}
}

func TestReportLockHealth_SkipsMissingDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("ghost", filepath.Join(tmp, "does-not-exist"))

	out := captureDoctorOutput(t, func() { reportLockHealth(cfg, cfg.ListProjects()) })
	if !strings.Contains(out, "not applicable") {
		t.Errorf("expected not-applicable summary when dir missing, got %q", out)
	}
}

func TestLockDriftMessageAndHint(t *testing.T) {
	cases := []struct {
		status   config.LockDriftStatus
		inMsg    string
		wantHint string
	}{
		{config.LockStatusMissingFromLock, "absent from lock", "  hint: da config sync"},
		{config.LockStatusExtraInLock, "no longer declared", "  hint: da config sync to prune"},
		{config.LockDriftStatus("weird"), "weird", ""},
	}
	for _, tc := range cases {
		if msg := lockDriftMessage(tc.status); !strings.Contains(msg, tc.inMsg) {
			t.Errorf("lockDriftMessage(%q) = %q, want substring %q", tc.status, msg, tc.inMsg)
		}
		if hint := lockDriftHint(tc.status); hint != tc.wantHint {
			t.Errorf("lockDriftHint(%q) = %q, want %q", tc.status, hint, tc.wantHint)
		}
	}
}

// ---------- D4: headline link count == real managed-link count ----------

// countPlatformLinksOkSum is the independent oracle for the expected healthy
// count: it sums every platform's LinkCounter.CountLinks ok return — the same
// source of truth the badge row and per-platform verbose audit consume. The
// headline "N links healthy" must equal this, not the partial cursor+claude-
// rules+single-file subset doctor used to recount on its own (D4 bug #1).
func countPlatformLinksOkSum(t *testing.T, name, path, agentsHome string) int {
	t.Helper()
	sum := 0
	for _, p := range platform.All() {
		c, ok := p.(platform.LinkCounter)
		if !ok {
			continue
		}
		platformOK, _ := c.CountLinks(name, path, agentsHome)
		sum += platformOK
	}
	return sum
}

// TestCountProjectLinks_HeadlineMatchesPerPlatformAudit seeds managed links
// spanning several platforms (claude rules + agents dir, codex AGENTS.md +
// hooks.json, opencode.json, copilot instructions) and asserts the headline
// ok tally returned by countProjectLinks equals the per-platform CountLinks
// sum — proving the count can no longer undercount (the "1 links healthy"
// while ~28 exist regression).
func TestCountProjectLinks_HeadlineMatchesPerPlatformAudit(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	mkLink := func(src, dst string) {
		t.Helper()
		os.MkdirAll(filepath.Dir(src), 0755)
		os.WriteFile(src, []byte("ok"), 0644)
		os.MkdirAll(filepath.Dir(dst), 0755)
		linktest.Link(t, src, dst)
	}

	// Claude rules symlink + a managed agent under .claude/agents/.
	mkLink(filepath.Join(agentsHome, "rules", "proj", "agents.md"),
		filepath.Join(projectPath, ".claude", "rules", "proj--agents.md"))
	mkLink(filepath.Join(agentsHome, "agents", "proj", "helper.md"),
		filepath.Join(projectPath, ".claude", "agents", "helper.md"))

	// Codex AGENTS.md symlink + present-but-rendered .codex/hooks.json.
	mkLink(filepath.Join(agentsHome, "rules", "proj", "AGENTS.md"),
		filepath.Join(projectPath, "AGENTS.md"))
	os.MkdirAll(filepath.Join(projectPath, ".codex"), 0755)
	os.WriteFile(filepath.Join(projectPath, ".codex", "hooks.json"), []byte("{}"), 0644)

	// OpenCode opencode.json symlink.
	mkLink(filepath.Join(agentsHome, "settings", "proj", "opencode.json"),
		filepath.Join(projectPath, "opencode.json"))

	// Copilot instructions symlink.
	mkLink(filepath.Join(agentsHome, "rules", "proj", "copilot-instructions.md"),
		filepath.Join(projectPath, ".github", "copilot-instructions.md"))

	wantOK := countPlatformLinksOkSum(t, "proj", projectPath, agentsHome)
	gotOK, broken := countProjectLinks("proj", projectPath, agentsHome)
	if broken != 0 {
		t.Errorf("expected 0 broken, got %d", broken)
	}
	if gotOK != wantOK {
		t.Errorf("headline ok=%d does not match per-platform CountLinks sum=%d", gotOK, wantOK)
	}
	// Guard against the silent-undercount regression: the seeded tree spans
	// >=5 managed entries across platforms, so a partial recount would land
	// well below this floor.
	if gotOK < 5 {
		t.Errorf("expected headline ok>=5 for multi-platform tree, got %d", gotOK)
	}
}

// TestCountPlatformLinksOk_DelegatesToEveryCounter pins that the helper sums
// across all platforms (not just one) by comparing against the oracle.
func TestCountPlatformLinksOk_DelegatesToEveryCounter(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	projectPath := filepath.Join(tmp, "proj")
	os.MkdirAll(projectPath, 0755)

	// One healthy link each for claude (rules) and codex (AGENTS.md).
	target := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	os.MkdirAll(filepath.Dir(target), 0755)
	os.WriteFile(target, []byte("ok"), 0644)
	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	os.MkdirAll(claudeRules, 0755)
	linktest.Link(t, target, filepath.Join(claudeRules, "proj--agents.md"))
	linktest.Link(t, target, filepath.Join(projectPath, "AGENTS.md"))

	got := countPlatformLinksOk("proj", projectPath, agentsHome)
	want := countPlatformLinksOkSum(t, "proj", projectPath, agentsHome)
	if got != want {
		t.Errorf("countPlatformLinksOk=%d, oracle=%d", got, want)
	}
	if got < 2 {
		t.Errorf("expected at least 2 (claude rule + codex AGENTS.md), got %d", got)
	}
}
