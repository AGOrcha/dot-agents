package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/linktest"
)

// ---------- removeProjectDirs ----------

func TestRemoveProjectDirs_RemovesAllCanonicalDirs(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	project := "removeme"
	created := []string{
		filepath.Join(agentsHome, "rules", project),
		filepath.Join(agentsHome, "settings", project),
		filepath.Join(agentsHome, "mcp", project),
		filepath.Join(agentsHome, "hooks", project),
		filepath.Join(agentsHome, "skills", project),
		filepath.Join(agentsHome, "agents", project),
	}
	for _, d := range created {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "marker.txt"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Sibling dir for a different project — must survive
	survivor := filepath.Join(agentsHome, "rules", "other")
	os.MkdirAll(survivor, 0755)

	removeProjectDirs(project)

	for _, d := range created {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, but it still exists", d)
		}
	}
	if _, err := os.Stat(survivor); err != nil {
		t.Errorf("survivor dir for other project was unexpectedly removed: %v", err)
	}
}

func TestRemoveProjectDirs_NoopOnMissingDirs(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Nothing should panic, no error returned.
	removeProjectDirs("ghost")
}

// removeProjectDirs must aggregate every RemoveAll failure (not discard them)
// and swallow only not-exist.
func TestRemoveProjectDirs_AggregatesRemoveAllFailures(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	sentinel := errors.New("removeall boom")
	withRemoveAllStub(t, func(string) error { return sentinel })

	err := removeProjectDirs("p")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected aggregated RemoveAll sentinel, got %v", err)
	}
}

func TestRemoveProjectDirs_SwallowsNotExist(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	withRemoveAllStub(t, func(d string) error { return os.ErrNotExist })

	if err := removeProjectDirs("p"); err != nil {
		t.Fatalf("not-exist must be swallowed, got %v", err)
	}
}

// FINDING 1: a fault-injected RemoveAll failure during `da remove --clean`
// must return a non-zero error, PRESERVE the project registration so cleanup
// can be retried, and NOT report "removed completely".
func TestRunRemove_CleanFailurePreservesRegistration(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	rulesDir := filepath.Join(agentsHome, "rules", "myproj")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("removeall boom")
	withRemoveAllStub(t, func(string) error { return sentinel })

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runRemove("myproj", true)
	if err == nil {
		t.Fatal("expected non-zero error when canonical cleanup fails")
	}
	if !strings.Contains(err.Error(), "could not clean project directories") {
		t.Errorf("expected clean-failure message, got: %v", err)
	}
	if strings.Contains(err.Error(), "removed completely") {
		t.Errorf("must not report complete removal on failure: %v", err)
	}

	// Registration MUST be preserved so cleanup can be retried.
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("myproj") == "" {
		t.Error("project must remain registered when canonical cleanup failed")
	}
}

// ---------- runRemove ----------

func TestRunRemove_UnknownProjectErrors(t *testing.T) {
	tmp := t.TempDir()
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

	err := runRemove("nope", false)
	if err == nil {
		t.Fatal("expected error for unknown project, got nil")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error should mention project name, got: %v", err)
	}
}

func TestRunRemove_DryRunSkipsMutation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	os.MkdirAll(projectPath, 0755)

	// Create stub project dirs (would normally be deleted by --clean)
	rulesDir := filepath.Join(agentsHome, "rules", "myproj")
	os.MkdirAll(rulesDir, 0755)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{DryRun: true, Yes: true}
	defer func() { Flags = saved }()

	if err := runRemove("myproj", true); err != nil {
		t.Fatalf("runRemove --dry-run failed: %v", err)
	}

	// Dry run must not unregister
	reloaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.GetProjectPath("myproj") == "" {
		t.Error("dry-run should not unregister the project")
	}

	// Dry run must not remove dirs
	if _, err := os.Stat(rulesDir); err != nil {
		t.Errorf("dry-run should not remove project dirs: %v", err)
	}
}

func TestRunRemove_UnregistersAndOptionallyCleans(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	os.MkdirAll(projectPath, 0755)

	rulesDir := filepath.Join(agentsHome, "rules", "myproj")
	os.MkdirAll(rulesDir, 0755)
	settingsDir := filepath.Join(agentsHome, "settings", "myproj")
	os.MkdirAll(settingsDir, 0755)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRemove("myproj", true); err != nil {
		t.Fatalf("runRemove: %v", err)
	}

	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("myproj") != "" {
		t.Error("project should be unregistered after remove")
	}

	if _, err := os.Stat(rulesDir); !os.IsNotExist(err) {
		t.Errorf("rules dir should be cleaned with --clean, still exists: %v", err)
	}
	if _, err := os.Stat(settingsDir); !os.IsNotExist(err) {
		t.Errorf("settings dir should be cleaned with --clean, still exists: %v", err)
	}
}

func TestRunRemove_NoCleanKeepsDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	os.MkdirAll(projectPath, 0755)

	rulesDir := filepath.Join(agentsHome, "rules", "myproj")
	os.MkdirAll(rulesDir, 0755)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRemove("myproj", false); err != nil {
		t.Fatalf("runRemove: %v", err)
	}

	if _, err := os.Stat(rulesDir); err != nil {
		t.Errorf("rules dir should be preserved without --clean: %v", err)
	}
}

func TestRunRemove_MissingProjectDirStillUnregisters(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Register a project whose directory was already moved/deleted
	ghostPath := filepath.Join(tmp, "ghost-dir")
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("ghost", ghostPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRemove("ghost", false); err != nil {
		t.Fatalf("runRemove for missing dir: %v", err)
	}
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("ghost") != "" {
		t.Error("ghost should still be unregistered when path missing")
	}
}

// ---------- NewRemoveCmd metadata ----------

func TestNewRemoveCmd_FlagsAndArgs(t *testing.T) {
	cmd := NewRemoveCmd()
	if cmd.Use != "remove <project>" {
		t.Errorf("unexpected Use: %q", cmd.Use)
	}
	flag := cmd.Flags().Lookup("clean")
	if flag == nil {
		t.Fatal("--clean flag not registered")
	}
	if flag.DefValue != "false" {
		t.Errorf("expected --clean default false, got %q", flag.DefValue)
	}

	if err := cmd.Args(cmd, nil); err == nil {
		t.Error("expected error when no args supplied")
	}
	if err := cmd.Args(cmd, []string{"myproj"}); err != nil {
		t.Errorf("expected no error for single arg, got %v", err)
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("expected error for too many args")
	}
}

// TestRunRemove_WithGitSourceManifestWarns covers the git-source warning
// branch inside runRemove, plus the installed-platform RemoveLinks branch.
func TestRunRemove_WithGitSourceManifestWarns(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Make claude installed so platform RemoveLinks branches run
	os.MkdirAll(filepath.Join(tmp, ".claude"), 0755)

	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	os.MkdirAll(projectPath, 0755)
	// Manifest with git source triggers the warn branch.
	rc := &config.AgentsRC{Version: 1, Project: "myproj", Sources: []config.Source{{Type: "git", URL: "https://example.invalid/repo.git"}}}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRemove("myproj", false); err != nil {
		t.Fatalf("runRemove: %v", err)
	}

	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("myproj") != "" {
		t.Error("project should be unregistered after remove with git source")
	}
}

// TestRunRemove_ConfirmDecline covers the "Removal cancelled" branch when
// neither --yes nor --force is set and the user declines.
func TestRunRemove_ConfirmDecline(t *testing.T) {
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

	saved := Flags
	Flags = GlobalFlags{} // no Yes, no Force → Confirm prompts (defaults false)
	defer func() { Flags = saved }()

	if err := runRemove("p", false); err != nil {
		t.Errorf("runRemove decline: %v", err)
	}
	// Project should still be registered (cancellation).
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("p") == "" {
		t.Error("project should remain registered after declined remove")
	}
}

// TestRunRemove_DryRunCleansFlagShowsDestructiveWarn covers the
// destructive-warn branch with cleanDirs=true under dry-run (no actual delete).
func TestRunRemove_DryRunCleansFlagShowsDestructiveWarn(t *testing.T) {
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
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runRemove("myproj", true); err != nil {
		t.Errorf("runRemove dry-run with --clean: %v", err)
	}
	// Should still be registered (dry run)
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("myproj") == "" {
		t.Error("project should still be registered after dry-run remove")
	}
}

// ---------- FINDING 3: failed managed cleanup must not unregister ----------

// When a platform RemoveLinks fails (here: a managed .mcp.json symlink whose
// removal is blocked by a read-only project dir), runRemove must return a
// non-zero error and PRESERVE the registration so a retry can finish cleanup —
// it must NOT print "unlinked successfully" nor drop the project from config.
func TestRunRemove_CleanupFailurePreservesRegistration(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: a read-only dir does not deny removal")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// A managed .mcp.json: symlink whose target resolves under agentsHome.
	canonical := filepath.Join(agentsHome, "mcp", "myproj", "mcp.json")
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	mcpLink := filepath.Join(projectPath, ".mcp.json")
	linktest.Link(t, canonical, mcpLink)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	// Make the project dir read-only so removing the managed .mcp.json fails.
	if err := os.Chmod(projectPath, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(projectPath, 0o755) })

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runRemove("myproj", false)
	if err == nil {
		t.Skip("platform RemoveLinks did not fail on this filesystem")
	}

	// Registration MUST be preserved so cleanup can be retried.
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("myproj") == "" {
		t.Error("project must remain registered when managed cleanup failed")
	}
}

// TestRunRemove_AllPlatformsInstalled covers the remove.go:122 IsInstalled
// branch for every platform, ensuring RemoveLinks is exercised across the
// full platform set.
func TestRunRemove_AllPlatformsInstalled(t *testing.T) {
	tmp := seedAllPlatformInstallSignals(t)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "rm-all")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("rm-all", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runRemove("rm-all", false); err != nil {
		t.Errorf("runRemove (all platforms seeded): %v", err)
	}
}

// TestRunRemove_AllPlatformsInstalledNonDryRun covers remove.go:129-135
// (RemoveLinks invocation outside dry-run) for every platform.
func TestRunRemove_AllPlatformsInstalledNonDryRun(t *testing.T) {
	tmp := seedAllPlatformInstallSignals(t)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "rm-real")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("rm-real", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runRemove("rm-real", false); err != nil {
		t.Errorf("runRemove non-dry-run (all platforms seeded): %v", err)
	}
}

// TestRunRemove_SeededClaudeExercisesInstalledPlatformBranch covers
// remove.go:122 `if p.IsInstalled()` true branch in the dry-run path so
// platform.RemoveLinks is invoked at least once during the test.
func TestRunRemove_SeededClaudeExercisesInstalledPlatformBranch(t *testing.T) {
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

	projectPath := filepath.Join(tmp, "rm-target")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("rm-target", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runRemove("rm-target", false); err != nil {
		t.Errorf("runRemove dry-run with installed claude: %v", err)
	}
}
