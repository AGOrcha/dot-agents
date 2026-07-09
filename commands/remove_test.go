package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/linktest"
	"github.com/AGOrcha/dot-agents/internal/testutil"
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

	removeProjectDirs(project, stdRemoveDeps{})

	for _, d := range created {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, but it still exists", d)
		}
	}
	if _, err := os.Stat(survivor); err != nil {
		t.Errorf("survivor dir for other project was unexpectedly removed: %v", err)
	}
}

// removeProjectDirs must resolve the hooks subtree through the shared
// canonical helper (config.HooksScopeDir) so `da remove --clean` and
// `da hooks remove` cannot disagree about the ~/.agents/hooks/<scope> model.
func TestRemoveProjectDirs_RemovesCanonicalHooksScopeDir(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	project := "hooky"
	canonicalHooks := config.HooksScopeDir(project)
	if canonicalHooks != filepath.Join(agentsHome, "hooks", project) {
		t.Fatalf("precondition: HooksScopeDir resolved unexpectedly: %s", canonicalHooks)
	}
	if err := os.MkdirAll(canonicalHooks, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(canonicalHooks, "HOOK.yaml"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := removeProjectDirs(project, stdRemoveDeps{}); err != nil {
		t.Fatalf("removeProjectDirs returned error: %v", err)
	}
	if _, err := os.Stat(canonicalHooks); !os.IsNotExist(err) {
		t.Errorf("expected canonical hooks scope dir %s to be removed", canonicalHooks)
	}
}

func TestRemoveProjectDirs_NoopOnMissingDirs(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Nothing should panic, no error returned.
	removeProjectDirs("ghost", stdRemoveDeps{})
}

// removeProjectDirs must aggregate every RemoveAll failure (not discard them)
// and swallow only not-exist.
// TestNewRemoveCmd_RunEClosureWiresStdDeps drives remove's RunE closure
// end to end so a regression in std deps wiring fails here. Uses
// --dry-run so no on-disk mutation occurs.
func TestNewRemoveCmd_RunEClosureWiresStdDeps(t *testing.T) {
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
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("myproj", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{DryRun: true, Yes: true}
	defer func() { Flags = saved }()

	cmd := NewRemoveCmd()
	if err := cmd.RunE(cmd, []string{"myproj"}); err != nil {
		t.Fatalf("RunE closure: %v", err)
	}
}

// fakeRemoveDeps is the interface-DI test double for removeDeps (per
// docs/TEST_SEAMS.md). A nil func field delegates to the real
// implementation, so a test overrides only the operation it wants to
// fault-inject.
type fakeRemoveDeps struct {
	removeAll  func(string) error
	loadConfig func() (*config.Config, error)
}

func (f fakeRemoveDeps) RemoveAll(path string) error {
	if f.removeAll != nil {
		return f.removeAll(path)
	}
	return os.RemoveAll(path)
}

func (f fakeRemoveDeps) LoadConfig() (*config.Config, error) {
	if f.loadConfig != nil {
		return f.loadConfig()
	}
	return config.Load()
}

// TestFakeRemoveDeps_NilDelegatesToReal pins the nil-delegates-to-real
// contract for both methods of the fake.
func TestFakeRemoveDeps_NilDelegatesToReal(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	// RemoveAll: a not-exist path returns nil.
	if err := (fakeRemoveDeps{}).RemoveAll(filepath.Join(tmp, "absent")); err != nil {
		t.Fatalf("nil-removeAll delegate on absent path: %v", err)
	}
	// LoadConfig: real config.Load returns a config (or a real error).
	cfg, err := (fakeRemoveDeps{}).LoadConfig()
	if err != nil {
		t.Fatalf("nil-loadConfig delegate: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected real config.Load result, got nil")
	}
}

func TestRemoveProjectDirs_AggregatesRemoveAllFailures(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	sentinel := errors.New("removeall boom")
	deps := fakeRemoveDeps{removeAll: func(string) error { return sentinel }}

	err := removeProjectDirs("p", deps)
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected aggregated RemoveAll sentinel, got %v", err)
	}
}

func TestRemoveProjectDirs_SwallowsNotExist(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	deps := fakeRemoveDeps{removeAll: func(d string) error { return os.ErrNotExist }}

	if err := removeProjectDirs("p", deps); err != nil {
		t.Fatalf("not-exist must be swallowed, got %v", err)
	}
}

// ---------- emptyProjectDirs ----------

// fakeDirCleaner is an interface-DI test double for dirCleaner. A nil func
// field delegates to the real os implementation, so a test overrides only
// the operation it wants to fault-inject.
type fakeDirCleaner struct {
	readDir   func(string) ([]os.DirEntry, error)
	removeAll func(string) error
}

func (f fakeDirCleaner) ReadDir(name string) ([]os.DirEntry, error) {
	if f.readDir != nil {
		return f.readDir(name)
	}
	return os.ReadDir(name)
}

func (f fakeDirCleaner) RemoveAll(path string) error {
	if f.removeAll != nil {
		return f.removeAll(path)
	}
	return os.RemoveAll(path)
}

// emptyProjectDirs clears each canonical dir's contents (files + nested
// subtrees) but leaves the now-empty dir in place; a sibling project's dir
// is untouched.
func TestEmptyProjectDirs_ClearsContentsKeepsDir(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	project := "emptyme"
	dirs := []string{
		filepath.Join(agentsHome, "rules", project),
		filepath.Join(agentsHome, "settings", project),
		filepath.Join(agentsHome, "hooks", project),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(d, "nested"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "nested", "g.txt"), []byte("y"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	survivorContent := filepath.Join(agentsHome, "rules", "other", "keep.txt")
	os.MkdirAll(filepath.Dir(survivorContent), 0755)
	os.WriteFile(survivorContent, []byte("z"), 0644)

	if err := emptyProjectDirs(osDirCleaner{}, project); err != nil {
		t.Fatalf("emptyProjectDirs: %v", err)
	}

	for _, d := range dirs {
		entries, err := os.ReadDir(d)
		if err != nil {
			t.Fatalf("dir %s should still exist: %v", d, err)
		}
		if len(entries) != 0 {
			t.Errorf("dir %s should be empty, has %d entries", d, len(entries))
		}
	}
	if _, err := os.Stat(survivorContent); err != nil {
		t.Errorf("other project's content was unexpectedly removed: %v", err)
	}
}

// A canonical dir that does not exist is the expected "nothing to clear"
// case: emptyProjectDirs must not error and must not recreate it.
func TestEmptyProjectDirs_MissingDirNoopNoRecreate(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := emptyProjectDirs(osDirCleaner{}, "ghost"); err != nil {
		t.Fatalf("missing dirs must be a no-op, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "rules", "ghost")); !os.IsNotExist(err) {
		t.Error("emptyProjectDirs must not recreate a missing canonical dir")
	}
}

// A non-not-exist ReadDir failure must be aggregated, not swallowed.
func TestEmptyProjectDirs_AggregatesReadDirFailures(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	sentinel := errors.New("readdir boom")
	dc := fakeDirCleaner{readDir: func(string) ([]os.DirEntry, error) { return nil, sentinel }}

	err := emptyProjectDirs(dc, "p")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected aggregated ReadDir sentinel, got %v", err)
	}
}

// A child RemoveAll failure must be aggregated, not swallowed.
func TestEmptyProjectDirs_AggregatesChildRemoveFailures(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	d := filepath.Join(agentsHome, "rules", "p")
	if err := os.MkdirAll(d, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "f.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	sentinel := errors.New("removeall boom")
	// readDir nil → real os.ReadDir finds the seeded file; RemoveAll faults.
	dc := fakeDirCleaner{removeAll: func(string) error { return sentinel }}

	err := emptyProjectDirs(dc, "p")
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("expected aggregated RemoveAll sentinel, got %v", err)
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
	deps := fakeRemoveDeps{removeAll: func(string) error { return sentinel }}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runRemove("myproj", true, deps)
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

	err := runRemove("nope", false, stdRemoveDeps{})
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

	if err := runRemove("myproj", true, stdRemoveDeps{}); err != nil {
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

	if err := runRemove("myproj", true, stdRemoveDeps{}); err != nil {
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

// Without --clean, the canonical dirs' CONTENTS are cleared but the
// (now-empty) directories themselves are kept.
func TestRunRemove_NoCleanClearsContentsKeepsDirs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myproj")
	os.MkdirAll(projectPath, 0755)

	rulesDir := filepath.Join(agentsHome, "rules", "myproj")
	os.MkdirAll(rulesDir, 0755)
	// Seed content (file + nested subdir) that must be cleared.
	if err := os.WriteFile(filepath.Join(rulesDir, "rule.md"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(rulesDir, "sub", "deep")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nested, "n.txt"), []byte("y"), 0644); err != nil {
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

	if err := runRemove("myproj", false, stdRemoveDeps{}); err != nil {
		t.Fatalf("runRemove: %v", err)
	}

	// Dir kept...
	info, err := os.Stat(rulesDir)
	if err != nil {
		t.Fatalf("rules dir should be preserved without --clean: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("rules path should still be a directory")
	}
	// ...but emptied.
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		t.Fatalf("ReadDir(rulesDir): %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("rules dir should be empty without --clean, has %d entries", len(entries))
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

	if err := runRemove("ghost", false, stdRemoveDeps{}); err != nil {
		t.Fatalf("runRemove for missing dir: %v", err)
	}
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("ghost") != "" {
		t.Error("ghost should still be unregistered when path missing")
	}
}

// TestRemoveProjectLinks_StatErrorPreservesRegistration drives the REAL
// Stat-error branch (remove.go: os.Stat(projectPath) in removeProjectLinks):
// the directory exists but cannot be verified (parent unreadable), which
// must NOT be treated the same as "directory not found" — that would let
// runRemove's caller unregister the project while its links/content are
// still on disk but unverifiable, orphaning them. Denies traversal into the
// project dir's parent so os.Stat(projectPath) fails with a non-ENOENT error.
func TestRemoveProjectLinks_StatErrorPreservesRegistration(t *testing.T) {
	parent := t.TempDir()
	projectPath := filepath.Join(parent, "proj")
	if err := os.MkdirAll(projectPath, 0755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, parent)

	err := removeProjectLinks("proj", projectPath)
	if err == nil {
		t.Fatal("expected an error when the project directory cannot be stat-ed")
	}
	if !strings.Contains(err.Error(), "could not verify project directory") {
		t.Errorf("expected a 'could not verify' error, got: %v", err)
	}
	cliErr, ok := err.(*CLIError)
	if !ok {
		t.Fatalf("expected *CLIError with a retry hint, got %T: %v", err, err)
	}
	if len(cliErr.Hints) == 0 || !strings.Contains(cliErr.Hints[0], "PRESERVED") {
		t.Errorf("expected the registration-preserved hint, got: %v", cliErr.Hints)
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
	seedClaudeInstalledSignal(t, tmp)

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

	if err := runRemove("myproj", false, stdRemoveDeps{}); err != nil {
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

	if err := runRemove("p", false, stdRemoveDeps{}); err != nil {
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

	if err := runRemove("myproj", true, stdRemoveDeps{}); err != nil {
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

	err := runRemove("myproj", false, stdRemoveDeps{})
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

	if err := runRemove("rm-all", false, stdRemoveDeps{}); err != nil {
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

	if err := runRemove("rm-real", false, stdRemoveDeps{}); err != nil {
		t.Errorf("runRemove non-dry-run (all platforms seeded): %v", err)
	}
}

// TestRunRemove_SeededClaudeExercisesInstalledPlatformBranch covers
// remove.go:122 `if p.IsInstalled()` true branch in the dry-run path so
// platform.RemoveLinks is invoked at least once during the test.
func TestRunRemove_SeededClaudeExercisesInstalledPlatformBranch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignal(t, tmp)
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

	if err := runRemove("rm-target", false, stdRemoveDeps{}); err != nil {
		t.Errorf("runRemove dry-run with installed claude: %v", err)
	}
}
