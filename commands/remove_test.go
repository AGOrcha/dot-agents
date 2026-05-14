package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
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
