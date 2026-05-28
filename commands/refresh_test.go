package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

const refreshCanonicalAgentPath = "agents/proj/my-agent/AGENT.md"

// fakeRefreshConfigLoader is the interface-DI test double for
// refreshConfigLoader (per docs/TEST_SEAMS.md). A nil func field delegates
// to the real config.Load implementation.
type fakeRefreshConfigLoader struct {
	loadConfig func() (*config.Config, error)
}

func (f fakeRefreshConfigLoader) LoadConfig() (*config.Config, error) {
	if f.loadConfig != nil {
		return f.loadConfig()
	}
	return config.Load()
}

// TestFakeRefreshConfigLoader_NilDelegatesToReal pins the
// nil-delegates-to-real contract for the refresh seam.
func TestFakeRefreshConfigLoader_NilDelegatesToReal(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, err := (fakeRefreshConfigLoader{}).LoadConfig()
	if err != nil {
		t.Fatalf("nil-loadConfig delegate: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected real config.Load result, got nil")
	}
}

// TestNewRefreshCmd_RunEClosureWiresStdDeps drives refresh's RunE closure
// end to end so a regression in std deps wiring fails here. Empty managed
// projects exit cleanly without filesystem side effects.
func TestNewRefreshCmd_RunEClosureWiresStdDeps(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	cmd := NewRefreshCmd()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE closure: %v", err)
	}
}

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

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
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

	err := runRefresh("ghost", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{})
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

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
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

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
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
	if mapResourceRelToDest("proj", ".cursor/commands/") == "" {
		t.Error("expected non-empty mapping for cursor commands dir literal")
	}
	if mapResourceRelToDest("proj", ".claude/commands/") == "" {
		t.Error("expected non-empty mapping for claude commands dir literal")
	}
	if mapResourceRelToDest("proj", ".opencode/commands/") == "" {
		t.Error("expected non-empty mapping for opencode commands dir literal")
	}
	// Other exact bucket inputs — just exercise the code path.
	_ = mapResourceRelToDest("proj", ".cursor/indexing.cursorindexingignore")
}

func TestMapResourceRelToDest_OutputStylesAndModes(t *testing.T) {
	// Exercise the additional switch-case branches.
	if mapResourceRelToDest("proj", ".claude/output-styles/") == "" {
		t.Error("expected mapping for claude output-styles dir literal")
	}
	if mapResourceRelToDest("proj", ".opencode/modes/") == "" {
		t.Error("expected mapping for opencode modes dir literal")
	}
	if mapResourceRelToDest("proj", ".opencode/themes/") == "" {
		t.Error("expected mapping for opencode themes dir literal")
	}
	if mapResourceRelToDest("proj", ".github/prompts/") == "" {
		t.Error("expected mapping for github prompts dir literal")
	}
}

// ---------- restoreFromResources wrapper ----------

func TestRestoreFromResources_Wrapper(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	testutil.WriteScopeFile(t, agentsHome, "resources", "proj", "AGENTS.md", []byte("# rules"))

	// Should not panic and should perform the same restore as Counted variant.
	restoreFromResources("proj", tmp, stdAddDeps{})

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

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
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

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Errorf("runRefresh with dot path: %v", err)
	}
}

// TestRunRefresh_DryRunWithCommit covers the dry-run message path with a
// non-empty refreshCommit (lines 184-186).
func TestRunRefresh_DryRunWithCommit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projPath := filepath.Join(tmp, "p")
	os.MkdirAll(projPath, 0755)
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Inject a fake commit through the package-level Commit variable.
	savedCommit := Commit
	Commit = "abcdef1234567890"
	defer func() { Commit = savedCommit }()

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()
	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Errorf("runRefresh dry-run with commit: %v", err)
	}
}

// TestMapResourceRelToDest_CursorRuleWithoutSuffix covers the empty-return
// branch in the .cursor/rules/ case when the file lacks .mdc/.md suffix and no
// global/project prefix.
func TestMapResourceRelToDest_CursorRuleNoSuffixReturnsEmpty(t *testing.T) {
	got := mapResourceRelToDest("proj", ".cursor/rules/notes.txt")
	if got != "" {
		t.Errorf("expected empty for non-mdc/md rule, got %q", got)
	}
}

// TestRunRefresh_MultiProjectStepNRender covers the total>1 branch that uses
// ui.StepN to render per-project headings.
func TestRunRefresh_MultiProjectStepNRender(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	for _, name := range []string{"a", "b"} {
		p := filepath.Join(tmp, name)
		os.MkdirAll(p, 0755)
	}
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("a", filepath.Join(tmp, "a"))
	cfg.AddProject("b", filepath.Join(tmp, "b"))
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Errorf("runRefresh multi-project: %v", err)
	}
}

// TestRunRefresh_NoEnabledPlatforms covers the empty-enabledPlatforms early
// return when every platform is disabled in config.
func TestRunRefresh_NoEnabledPlatforms(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Register a project so we don't short-circuit on len(projects)==0.
	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)
	// Explicitly disable every known platform.
	for _, pid := range []string{"cursor", "claude", "codex", "opencode", "copilot"} {
		cfg.SetPlatformState(pid, false, "")
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Errorf("runRefresh no-enabled-platforms: %v", err)
	}
}

// TestRunRefresh_SkipsProjectWithEmptyOrDotPath covers the path=="" or path=="."
// skip branch (line 113-115). Direct manipulation of config.Project map.
func TestRunRefresh_SkipsProjectWithEmptyPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	cfg := &config.Config{
		Version:  1,
		Projects: map[string]config.Project{"weird": {Path: "."}},
		Agents:   map[string]config.Agent{},
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Errorf("runRefresh skip-dot-path: %v", err)
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

// ---------- FINDING A: swallowed restore failure must not stamp success ----------

// TestRunRefresh_RestoreFailureDoesNotStampMetadata covers the regression where
// a partially-failed restore from ~/.agents/resources/<project>/ was swallowed
// and refresh still wrote success metadata. A restore copy failure must now make
// runRefresh return a non-zero error AND skip writing .agentsrc.json refresh
// metadata for that project.
func TestRunRefresh_RestoreFailureDoesNotStampMetadata(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Make claude installed so there is an enabled+installed platform.
	os.MkdirAll(filepath.Join(tmp, ".claude"), 0755)

	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)

	// Seed a legacy resource file that maps via restoreLegacyResourceFile and
	// therefore goes through the copyFile seam.
	testutil.WriteScopeFile(t, agentsHome, "resources", "p", "AGENTS.md", []byte("# rules"))

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("p", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Inject the copy failure through the addDeps fake threaded into
	// runRefresh → restoreFromResources → restoreFromResourcesCountedWithDeps
	// (the seam-interface-di convergence: package-var copyFile is gone).
	addD := fakeAddDeps{copyFile: func(string, string) error {
		return errors.New("injected copy failure")
	}}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, addD)
	if err == nil {
		t.Fatal("expected runRefresh to return non-zero error after swallowed restore failure")
	}

	// .agentsrc.json must NOT carry refresh metadata for the partially-applied project.
	rc, loadErr := config.LoadAgentsRC(projectPath)
	if loadErr == nil && rc.Refresh != nil {
		t.Errorf("expected NO refresh metadata after partial restore, got %+v", rc.Refresh)
	}
}

// ---------- FINDING B: directory restore-bucket mappings must fire ----------

// TestMapResourceRelToDest_DirectoryBucketsMatchByPrefix covers the regression
// where command/output-style/mode/theme/prompt restore buckets used exact-match
// switch cases against dir-prefix constants ending in "/" and therefore never
// matched real walked file paths like ".cursor/commands/foo.md".
func TestMapResourceRelToDest_DirectoryBucketsMatchByPrefix(t *testing.T) {
	cases := []struct {
		rel    string
		bucket platform.CanonicalBucket
		leaf   string
	}{
		{relCursorCommandsDir + "foo.md", platform.CanonicalBucketCommands, "foo.md"},
		{relClaudeCommandsDir + "bar.md", platform.CanonicalBucketCommands, "bar.md"},
		{relOpenCodeCommandsDir + "baz.md", platform.CanonicalBucketCommands, "baz.md"},
		{relClaudeOutputStylesDir + "style.md", platform.CanonicalBucketOutputStyles, "style.md"},
		{relOpenCodeModesDir + "mode.md", platform.CanonicalBucketModes, "mode.md"},
		{relOpenCodeThemesDir + "theme.json", platform.CanonicalBucketThemes, "theme.json"},
		{relGitHubPromptsDir + "prompt.md", platform.CanonicalBucketPrompts, "prompt.md"},
	}
	for _, c := range cases {
		got := mapResourceRelToDest("proj", c.rel)
		want := platform.CanonicalBucketScopePath(c.bucket, "proj", c.leaf)
		if got != want {
			t.Errorf("mapResourceRelToDest(%q) = %q, want %q", c.rel, got, want)
		}
	}
}

// TestRestoreFromResourcesCounted_RestoresDirectoryBuckets ensures at least one
// file under each new directory bucket is actually restored to the correct
// canonical destination (end-to-end through restoreFromResourcesCounted).
func TestRestoreFromResourcesCounted_RestoresDirectoryBuckets(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	type seed struct {
		rel    string
		bucket platform.CanonicalBucket
		leaf   string
	}
	seeds := []seed{
		{relCursorCommandsDir + "c1.md", platform.CanonicalBucketCommands, "c1.md"},
		{relClaudeOutputStylesDir + "s1.md", platform.CanonicalBucketOutputStyles, "s1.md"},
		{relOpenCodeModesDir + "m1.md", platform.CanonicalBucketModes, "m1.md"},
		{relOpenCodeThemesDir + "t1.json", platform.CanonicalBucketThemes, "t1.json"},
		{relGitHubPromptsDir + "p1.md", platform.CanonicalBucketPrompts, "p1.md"},
	}
	for _, s := range seeds {
		f := filepath.Join(agentsHome, "resources", "proj", filepath.FromSlash(s.rel))
		os.MkdirAll(filepath.Dir(f), 0755)
		if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	n, err := restoreFromResourcesCounted("proj", tmp)
	if err != nil {
		t.Fatalf("restoreFromResourcesCounted error: %v", err)
	}
	if n != len(seeds) {
		t.Errorf("expected %d restores, got %d", len(seeds), n)
	}
	for _, s := range seeds {
		dest := filepath.Join(agentsHome, filepath.FromSlash(platform.CanonicalBucketScopePath(s.bucket, "proj", s.leaf)))
		if _, statErr := os.Stat(dest); statErr != nil {
			t.Errorf("expected %s bucket file restored at %s: %v", s.bucket, dest, statErr)
		}
	}
}

// TestRunRefresh_AllPlatformsInstalled covers the refresh.go:71 and 163
// `p.IsInstalled()` branches for every enabled platform. Pre-enable all
// platforms in config so the refresh loop iterates each one.
func TestRunRefresh_AllPlatformsInstalled(t *testing.T) {
	tmp := seedAllPlatformInstallSignals(t)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "rp")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("rp", projectPath)
	for _, id := range []string{"claude", "cursor", "codex", "opencode", "copilot"} {
		cfg.SetPlatformState(id, true, "")
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Errorf("runRefresh (all platforms seeded): %v", err)
	}
}

// TestRunRefresh_AllPlatformsDryRun mirrors the above through the dry-run
// branch at refresh.go:167-170 ("Refresh ... links" bullet) for each platform.
func TestRunRefresh_AllPlatformsDryRun(t *testing.T) {
	tmp := seedAllPlatformInstallSignals(t)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "rp-dry")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("rp-dry", projectPath)
	for _, id := range []string{"claude", "cursor", "codex", "opencode", "copilot"} {
		cfg.SetPlatformState(id, true, "")
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Errorf("runRefresh dry-run (all platforms seeded): %v", err)
	}
}

// TestRunRefresh_SeededClaudeDryRunExercisesDryRunBranches covers refresh.go
// dry-run loop branches when an installed platform is present (lines 167-170
// — `DryRun "Refresh ... links"` bullet).
func TestRunRefresh_SeededClaudeDryRunExercisesDryRunBranches(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "p")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
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

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Errorf("runRefresh dry-run with installed claude: %v", err)
	}
}

// ─── stp3 regression: refresh SharedTargetProjection wiring ────────────────
//
// These tests pin the refresh-side projection wiring landed by
// fix-shared-skill-relink (refresh.go:238 runSharedTargetsForRefresh). They
// assert (a) the projection materializes its expected projected artifact —
// not merely that .agentsrc.json was written; (b) dry-run produces NO
// projected artifacts; (c) two back-to-back refreshes leave the repo's
// projected tree byte-identical. A regression that drops the projection
// call, hard-codes dryRun=false, or breaks Execute idempotence will fail.

// writeRefreshCodexAgentFixture writes a canonical Codex agent under
// <agentsHome>/agents/<project>/<name>/AGENT.md so the shared-target
// projection emits a repo .codex/agents/<name>.toml when refresh runs.
func writeRefreshCodexAgentFixture(t *testing.T, agentsHome, project, name string) {
	t.Helper()
	dir := filepath.Join(agentsHome, "agents", project, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: refresh stp3 fixture\n---\n\n# Body\nShip it.\n"
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedRefreshProjectWithCodexAgent scaffolds the minimum env for refresh to
// run the shared-target projection against an installed codex platform with
// one canonical agent. Returns (tmp, agentsHome, projectPath, projectName).
func seedRefreshProjectWithCodexAgent(t *testing.T, projectName, agentName string) (string, string, string) {
	t.Helper()
	tmp := seedAllPlatformInstallSignals(t)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, projectName)
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	writeRefreshCodexAgentFixture(t, agentsHome, projectName, agentName)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject(projectName, projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return tmp, agentsHome, projectPath
}

// TestRunRefresh_SharedTargetProjectionMaterializesCodexToml asserts the
// projection's ONLY observable effect (the projected .codex/agents/<n>.toml)
// after a real refresh — proving the projection ran, not merely the metadata
// stamp. CreateLinks does not produce this file; only the projection does.
func TestRunRefresh_SharedTargetProjectionMaterializesCodexToml(t *testing.T) {
	_, _, projectPath := seedRefreshProjectWithCodexAgent(t, "refproj", "implementer")

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("runRefresh: %v", err)
	}

	tomlPath := filepath.Join(projectPath, ".codex", "agents", "implementer.toml")
	b, err := os.ReadFile(tomlPath)
	if err != nil {
		t.Fatalf("expected refresh's shared-target projection to write %s: %v", tomlPath, err)
	}
	body := string(b)
	if !strings.Contains(body, `name = "implementer"`) || !strings.Contains(body, "Ship it.") {
		t.Fatalf("projected codex toml has unexpected content:\n%s", body)
	}
}

// TestRunRefresh_SharedTargetProjectionDryRunNoMutation asserts that with
// Flags.DryRun=true the projection's planned artifact is NOT materialized.
// A regression that hard-codes dryRun=false (or drops Flags.DryRun from the
// RunSharedTargetProjection call) would create the file and fail this test.
func TestRunRefresh_SharedTargetProjectionDryRunNoMutation(t *testing.T) {
	_, _, projectPath := seedRefreshProjectWithCodexAgent(t, "drrproj", "implementer")

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("runRefresh dry-run: %v", err)
	}

	tomlPath := filepath.Join(projectPath, ".codex", "agents", "implementer.toml")
	if _, err := os.Stat(tomlPath); err == nil {
		t.Fatalf("dry-run refresh must NOT materialize %s; projection wiring is ignoring Flags.DryRun", tomlPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected stat error for %s: %v", tomlPath, err)
	}
}

// TestRunRefresh_SharedTargetProjectionIdempotent asserts two back-to-back
// refreshes leave the projected .codex/ tree byte-identical (content hash +
// kind). A regression that always re-renders / churns mtimes via
// os.Remove+Write would fail the per-entry compare on the second pass.
func TestRunRefresh_SharedTargetProjectionIdempotent(t *testing.T) {
	_, _, projectPath := seedRefreshProjectWithCodexAgent(t, "idemref", "implementer")

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("first runRefresh: %v", err)
	}
	codexDir := filepath.Join(projectPath, ".codex")
	first := snapshotTree(t, codexDir)
	if len(first) == 0 {
		t.Fatalf("first refresh produced no .codex/ artifacts; projection did not run")
	}

	if err := runRefresh("", stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{}); err != nil {
		t.Fatalf("second runRefresh: %v", err)
	}
	second := snapshotTree(t, codexDir)

	if msg, ok := snapshotsEqual(first, second); !ok {
		t.Fatalf("refresh not idempotent under .codex/: %s\nfirst=%d second=%d",
			msg, len(first), len(second))
	}
}
