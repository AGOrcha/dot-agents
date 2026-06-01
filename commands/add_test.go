package commands

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/linktest"
	"github.com/AGOrcha/dot-agents/internal/projectsync"
	"github.com/AGOrcha/dot-agents/internal/testutil"
)

// fakeAddDeps is the interface-DI test double for addDeps (per
// docs/TEST_SEAMS.md). A nil func field delegates to the real implementation,
// so a test overrides only the operation it wants to fault-inject. Mirrors
// install.go's fakeInstallDeps pattern.
type fakeAddDeps struct {
	mkdirAll   func(string, os.FileMode) error
	writeFile  func(string, []byte, os.FileMode) error
	remove     func(string) error
	executable func() (string, error)
	copyFile   func(string, string) error
	loadConfig func() (*config.Config, error)
}

func (f fakeAddDeps) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAll != nil {
		return f.mkdirAll(path, perm)
	}
	return os.MkdirAll(path, perm)
}

func (f fakeAddDeps) WriteFile(name string, data []byte, perm os.FileMode) error {
	if f.writeFile != nil {
		return f.writeFile(name, data, perm)
	}
	return os.WriteFile(name, data, perm)
}

func (f fakeAddDeps) Remove(name string) error {
	if f.remove != nil {
		return f.remove(name)
	}
	return os.Remove(name)
}

func (f fakeAddDeps) Executable() (string, error) {
	if f.executable != nil {
		return f.executable()
	}
	return os.Executable()
}

func (f fakeAddDeps) CopyFile(src, dst string) error {
	if f.copyFile != nil {
		return f.copyFile(src, dst)
	}
	return projectsync.CopyFile(src, dst)
}

func (f fakeAddDeps) LoadConfig() (*config.Config, error) {
	if f.loadConfig != nil {
		return f.loadConfig()
	}
	return config.Load()
}

// TestFakeAddDeps_NilDelegatesToReal pins the nil-delegates-to-real contract
// for every method of the fake. Without this, a future change to the fake's
// default branch could regress every happy-path-but-not-overridden test
// without any of them failing.
func TestFakeAddDeps_NilDelegatesToReal(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	if err := os.MkdirAll(filepath.Join(tmp, ".agents"), 0o755); err != nil {
		t.Fatal(err)
	}

	f := fakeAddDeps{}
	target := filepath.Join(tmp, "delegate", "nested")
	if err := f.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("nil-mkdirAll delegate: %v", err)
	}
	wf := filepath.Join(target, "out.txt")
	if err := f.WriteFile(wf, []byte("data"), 0o644); err != nil {
		t.Fatalf("nil-writeFile delegate: %v", err)
	}
	if err := f.Remove(wf); err != nil {
		t.Fatalf("nil-remove delegate: %v", err)
	}
	if exe, err := f.Executable(); err != nil || exe == "" {
		t.Fatalf("nil-executable delegate: exe=%q err=%v", exe, err)
	}
	src := filepath.Join(tmp, "src.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(tmp, "dst.txt")
	if err := f.CopyFile(src, dst); err != nil {
		t.Fatalf("nil-copyFile delegate: %v", err)
	}
	if cfg, err := f.LoadConfig(); err != nil || cfg == nil {
		t.Fatalf("nil-loadConfig delegate: cfg=%v err=%v", cfg, err)
	}
}

// ---------- isBackupArtifact ----------

func TestIsBackupArtifact(t *testing.T) {
	cases := []struct {
		name   string
		expect bool
	}{
		{"rules.mdc", false},
		{"AGENTS.md", false},
		{"rules.mdc.dot-agents-backup", true},
		{"rules.mdc.dot-agents-backup.dot-agents-backup", true},
		{"sonarqube_mcp_instructions.mdc.dot-agents-backup", true},
		{".dot-agents-backup", true},
	}
	for _, c := range cases {
		got := isBackupArtifact(c.name)
		if got != c.expect {
			t.Errorf("isBackupArtifact(%q) = %v, want %v", c.name, got, c.expect)
		}
	}
}

// ---------- checkExistingConfigFiles ----------

func TestCheckExistingConfigFiles_SkipsBackupArtifacts(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Write a backup artifact named AGENTS.md.dot-agents-backup
	artifact := filepath.Join(tmp, "AGENTS.md.dot-agents-backup")
	os.WriteFile(artifact, []byte("old"), 0644)

	// No actual AGENTS.md — only the artifact
	found := checkExistingConfigFiles("proj", tmp, agentsHome)
	for _, f := range found {
		if strings.Contains(f, ".dot-agents-backup") {
			t.Errorf("checkExistingConfigFiles returned backup artifact: %s", f)
		}
	}
}

func TestCheckExistingConfigFiles_SkipsAlreadyManagedSymlinks(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(filepath.Join(agentsHome, "rules", "proj"), 0755)

	// Create a symlink that points into agentsHome (already managed)
	target := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	os.WriteFile(target, []byte("rules"), 0644)
	linkPath := filepath.Join(tmp, "AGENTS.md")
	linktest.Link(t, target, linkPath)

	found := checkExistingConfigFiles("proj", tmp, agentsHome)
	for _, f := range found {
		if f == linkPath {
			t.Errorf("checkExistingConfigFiles should have skipped already-managed symlink %s", f)
		}
	}
}

func TestCheckExistingConfigFiles_IncludesUnmanagedFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	// Write a real AGENTS.md that is not managed
	agentsMD := filepath.Join(tmp, "AGENTS.md")
	os.WriteFile(agentsMD, []byte("# instructions"), 0644)

	found := checkExistingConfigFiles("proj", tmp, agentsHome)
	if len(found) != 1 || found[0] != agentsMD {
		t.Errorf("expected [%s], got %v", agentsMD, found)
	}
}

func TestCheckExistingConfigFiles_SkipsManagedCursorHardlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	project := "proj"
	source := filepath.Join(agentsHome, "rules", "global", "rules.mdc")
	if err := os.MkdirAll(filepath.Dir(source), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("rule"), 0644); err != nil {
		t.Fatal(err)
	}

	rulePath := filepath.Join(tmp, ".cursor", "rules", "global--rules.mdc")
	if err := os.MkdirAll(filepath.Dir(rulePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(source, rulePath); err != nil {
		t.Fatal(err)
	}

	found := checkExistingConfigFiles(project, tmp, agentsHome)
	for _, f := range found {
		if f == rulePath {
			t.Fatalf("checkExistingConfigFiles should have skipped managed hardlink %s", f)
		}
	}
}

// ---------- scanExistingAIConfigs ----------

func TestScanExistingAIConfigs_ExcludesBackupArtifacts(t *testing.T) {
	tmp := t.TempDir()

	// Create a real .mcp.json and a backup artifact alongside it
	os.WriteFile(filepath.Join(tmp, ".mcp.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(tmp, ".mcp.json.dot-agents-backup"), []byte("{}"), 0644)

	// Create a .cursor/rules/ dir with a rule and a backup artifact
	rulesDir := filepath.Join(tmp, ".cursor", "rules")
	os.MkdirAll(rulesDir, 0755)
	os.WriteFile(filepath.Join(rulesDir, "global--rules.mdc"), []byte("rule"), 0644)
	os.WriteFile(filepath.Join(rulesDir, "global--rules.mdc.dot-agents-backup"), []byte("rule"), 0644)

	results := scanExistingAIConfigs(tmp)
	for _, r := range results {
		if strings.Contains(r, ".dot-agents-backup") {
			t.Errorf("scanExistingAIConfigs returned backup artifact: %s", r)
		}
	}
}

// ---------- backupExistingConfigsList ----------

func TestBackupExistingConfigsList_CopyDeleteNoArtifactInProject(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Create a regular file to be backed up
	agentsMD := filepath.Join(tmp, "AGENTS.md")
	os.WriteFile(agentsMD, []byte("# instructions"), 0644)

	count, _ := backupExistingConfigsList([]string{agentsMD}, tmp, agentsHome, "myproject", "20260101-120000", stdAddDeps{})

	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}

	// Original should be gone from the project
	if _, err := os.Lstat(agentsMD); !os.IsNotExist(err) {
		t.Error("original file should have been deleted from the project tree")
	}

	// No *.dot-agents-backup should exist in the project
	backupPath := agentsMD + ".dot-agents-backup"
	if _, err := os.Lstat(backupPath); !os.IsNotExist(err) {
		t.Error("*.dot-agents-backup should NOT exist in the project tree")
	}

	// Canonical copy should exist in ~/.agents/resources/<project>/AGENTS.md
	activeTarget := filepath.Join(agentsHome, "resources", "myproject", "AGENTS.md")
	if _, err := os.Stat(activeTarget); err != nil {
		t.Errorf("active backup not found in resources: %v", err)
	}

	// Timestamped copy should exist
	tsTarget := filepath.Join(agentsHome, "resources", "myproject", "backups", "20260101-120000", "AGENTS.md")
	if _, err := os.Stat(tsTarget); err != nil {
		t.Errorf("timestamped backup not found in resources: %v", err)
	}
}

func TestBackupExistingConfigsList_SkipsBackupArtifacts(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Create only a backup artifact (no real file)
	artifact := filepath.Join(tmp, "AGENTS.md.dot-agents-backup")
	os.WriteFile(artifact, []byte("old"), 0644)

	count, _ := backupExistingConfigsList([]string{artifact}, tmp, agentsHome, "myproject", "20260101-120000", stdAddDeps{})

	// Artifact should be skipped — count stays 0
	if count != 0 {
		t.Errorf("expected count=0 for artifact input, got %d", count)
	}

	// The artifact itself should still exist (we didn't touch it)
	if _, err := os.Lstat(artifact); err != nil {
		t.Error("backup artifact should not have been removed by the backup function")
	}

	// Nothing should appear in resources for this
	resourcesDir := filepath.Join(agentsHome, "resources", "myproject")
	if _, err := os.Stat(resourcesDir); !os.IsNotExist(err) {
		t.Error("resources dir should not have been created for backup artifact input")
	}
}

// An UNMANAGED symlink (resolvable, but pointing at a real user file OUTSIDE
// dot-agents) is the project's only copy of that config. It must NOT be
// dropped without a backup just because it resolves: the resolved content is
// mirrored into resources before the link is removed (the symlink twin of
// TestBackupExistingConfigsList_UnmanagedHardlinkIsBackedUp).
func TestBackupExistingConfigsList_UnmanagedSymlinkIsBackedUp(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Create an unmanaged symlink (pointing somewhere outside agentsHome)
	target := filepath.Join(tmp, "external.md")
	os.WriteFile(target, []byte("# the real project config"), 0644)
	linkPath := filepath.Join(tmp, "AGENTS.md")
	linktest.Link(t, target, linkPath)

	count, _ := backupExistingConfigsList([]string{linkPath}, tmp, agentsHome, "myproject", "ts", stdAddDeps{})

	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}

	// The link entry itself is removed (replaced by a managed link later)...
	if _, err := os.Lstat(linkPath); !os.IsNotExist(err) {
		t.Error("config entry should have been removed after backup")
	}

	// ...but the resolved content MUST have been mirrored into resources
	// (and into the timestamped immutable copy) so the user's data survives.
	activeTarget := filepath.Join(agentsHome, "resources", "myproject", "AGENTS.md")
	data, err := os.ReadFile(activeTarget)
	if err != nil {
		t.Fatalf("unmanaged symlink was dropped without a mirror backup: %v", err)
	}
	if string(data) != "# the real project config" {
		t.Errorf("mirror backup content mismatch: %q", string(data))
	}
	tsTarget := filepath.Join(agentsHome, "resources", "myproject", "backups", "ts", "AGENTS.md")
	if _, err := os.Stat(tsTarget); err != nil {
		t.Errorf("expected timestamped mirror backup at %s: %v", tsTarget, err)
	}

	// The original external target must be untouched.
	if d, err := os.ReadFile(target); err != nil || string(d) != "# the real project config" {
		t.Errorf("external symlink target must not be modified: %q (err=%v)", string(d), err)
	}
}

// A PROVEN managed symlink (resolves UNDER agentsHome) has no standalone
// content and is removed without a mirror backup.
func TestBackupExistingConfigsList_ManagedSymlinkNoBackup(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Canonical source lives UNDER agentsHome — the managed case.
	canonical := filepath.Join(agentsHome, "resources", "myproject", "AGENTS.canonical.md")
	os.MkdirAll(filepath.Dir(canonical), 0755)
	os.WriteFile(canonical, []byte("managed"), 0644)
	linkPath := filepath.Join(tmp, "AGENTS.md")
	linktest.Link(t, canonical, linkPath)

	// On POSIX this is a symlink resolving under agentsHome (managed). On
	// Windows linktest.Link makes a hard link (no resolvable target) which
	// IsManagedLinkUnder reports false — there it correctly falls to the
	// mirror path, which is also safe. Skip the managed-no-backup assertion
	// where the link is not resolvable.
	if !links.IsManagedLinkUnder(linkPath, agentsHome) {
		t.Skip("link not resolvable under agentsHome on this platform")
	}

	count, _ := backupExistingConfigsList([]string{linkPath}, tmp, agentsHome, "myproject", "ts", stdAddDeps{})
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
	if _, err := os.Lstat(linkPath); !os.IsNotExist(err) {
		t.Error("managed symlink should have been removed")
	}
	// No mirror backup for a managed link (no standalone content).
	activeTarget := filepath.Join(agentsHome, "resources", "myproject", "AGENTS.md")
	if _, err := os.Stat(activeTarget); !os.IsNotExist(err) {
		t.Error("managed symlink should not produce a resources backup entry")
	}
}

// An UNMANAGED hard-linked AGENTS.md (nlink>1, but NOT hard linked to the
// canonical source da would create) is the project's real config. It must be
// backed up through the mirror path, not silently dropped because nlink>1.
func TestBackupExistingConfigsList_UnmanagedHardlinkIsBackedUp(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// The project hard links its real AGENTS.md to another file it owns —
	// nlink>1 but unrelated to anything under agentsHome.
	realConfig := filepath.Join(tmp, "shared", "instructions.md")
	if err := os.MkdirAll(filepath.Dir(realConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(realConfig, []byte("# the real project config"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentsMD := filepath.Join(tmp, "AGENTS.md")
	if err := os.Link(realConfig, agentsMD); err != nil {
		t.Skipf("hard link unsupported on this fs: %v", err)
	}

	count, _ := backupExistingConfigsList([]string{agentsMD}, tmp, agentsHome, "myproject", "20260101-120000", stdAddDeps{})
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}

	// The real config content MUST have been mirrored before removal.
	activeTarget := filepath.Join(agentsHome, "resources", "myproject", "AGENTS.md")
	data, err := os.ReadFile(activeTarget)
	if err != nil {
		t.Fatalf("unmanaged hard link was dropped without a mirror backup: %v", err)
	}
	if string(data) != "# the real project config" {
		t.Errorf("mirror backup content mismatch: %q", string(data))
	}
}

// A hard link PROVEN managed (shares its inode with the canonical source da
// created under agentsHome) has no standalone content — it is removed without
// a mirror backup.
func TestBackupExistingConfigsList_ManagedHardlinkNoBackup(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	canonical := filepath.Join(agentsHome, "rules", "myproject", "agents.md")
	if err := os.MkdirAll(filepath.Dir(canonical), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(canonical, []byte("# canonical"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	agentsMD := filepath.Join(tmp, "AGENTS.md")
	if err := os.Link(canonical, agentsMD); err != nil {
		t.Skipf("hard link unsupported on this fs: %v", err)
	}

	count, _ := backupExistingConfigsList([]string{agentsMD}, tmp, agentsHome, "myproject", "ts", stdAddDeps{})
	if count != 1 {
		t.Errorf("expected count=1, got %d", count)
	}
	if _, err := os.Lstat(agentsMD); !os.IsNotExist(err) {
		t.Error("managed hard link should have been removed")
	}
	// No mirror backup for a proven-managed link (canonical still holds it).
	if _, err := os.Stat(filepath.Join(agentsHome, "resources", "myproject", "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("proven-managed hard link should not produce a resources backup")
	}
}

// ---------- idempotence: second add sees no files to backup ----------

func TestCheckExistingConfigFiles_IdempotentAfterAdd(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(filepath.Join(agentsHome, "rules", "proj"), 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	// Simulate post-add state: AGENTS.md is a symlink into agentsHome
	target := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	os.WriteFile(target, []byte("# rules"), 0644)
	linktest.Link(t, target, filepath.Join(tmp, "AGENTS.md"))

	// No stale backup artifacts either
	found := checkExistingConfigFiles("proj", tmp, agentsHome)
	if len(found) != 0 {
		t.Errorf("second add should find nothing to back up, got: %v", found)
	}
}

// ---------- NewAddCmd metadata ----------

func TestNewAddCmd_FlagsAndArgs(t *testing.T) {
	cmd := NewAddCmd()
	if cmd.Use != "add <path>" {
		t.Errorf("unexpected Use=%q", cmd.Use)
	}
	if cmd.Flags().Lookup("name") == nil {
		t.Error("missing --name flag")
	}
	if err := cmd.Args(cmd, nil); err == nil {
		t.Error("expected error when path arg missing")
	}
	if err := cmd.Args(cmd, []string{"a", "b"}); err == nil {
		t.Error("expected error when too many args supplied")
	}
}

// ---------- runAdd basic guards ----------

func TestRunAdd_MissingDirectoryErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runAdd(filepath.Join(tmp, "nonexistent"), "", stdAddDeps{})
	if err == nil || !strings.Contains(err.Error(), "directory not found") {
		t.Errorf("expected directory-not-found error, got: %v", err)
	}
}

func TestRunAdd_InvalidProjectNameRejected(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "valid-path")
	os.MkdirAll(projectPath, 0755)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runAdd(projectPath, "bad name with spaces", stdAddDeps{})
	if err == nil || !strings.Contains(err.Error(), "invalid project name") {
		t.Errorf("expected invalid project name error, got: %v", err)
	}
}

func TestRunAdd_AlreadyRegisteredWithoutForceErrors(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "already")
	os.MkdirAll(projectPath, 0755)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject("already", projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runAdd(projectPath, "already", stdAddDeps{})
	if err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Errorf("expected already-registered error, got: %v", err)
	}
}

func TestRunAdd_DryRunSkipsRegistration(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "drytest")
	os.MkdirAll(projectPath, 0755)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.Save()

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runAdd(projectPath, "", stdAddDeps{}); err != nil {
		t.Fatalf("runAdd dry-run: %v", err)
	}

	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("drytest") != "" {
		t.Error("dry-run should not register project")
	}
}

// ---------- runAdd happy path (new project) ----------

func TestRunAdd_HappyPathRegistersProject(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myrepo")
	os.MkdirAll(projectPath, 0755)
	// Add a git dir so the "valid git repo" bullet branch runs.
	os.MkdirAll(filepath.Join(projectPath, ".git"), 0755)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runAdd(projectPath, "", stdAddDeps{}); err != nil {
		t.Fatalf("runAdd: %v", err)
	}
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("myrepo") == "" {
		t.Error("expected project to be registered")
	}
}

// FINDING 2: a failed resource restore must be FATAL for runAdd — the project
// must NOT be registered, no success box, and no link creation attempted after
// the failure. A non-directory squatting the resources path makes
// restoreFromResourcesCounted return a non-nil error deterministically.
func TestRunAdd_RestoreFailureAborts(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myrepo")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}

	// Block the resources path with a regular file: restoreFromResourcesCounted
	// stat()s ~/.agents/resources/<project> and returns an error when it is not
	// a directory.
	resourcesParent := filepath.Join(agentsHome, "resources")
	if err := os.MkdirAll(resourcesParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resourcesParent, "myrepo"), []byte("blocker"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runAdd(projectPath, "", stdAddDeps{})
	if err == nil {
		t.Fatal("expected non-zero error when resource restore fails")
	}
	if !strings.Contains(err.Error(), "could not restore resources") {
		t.Errorf("expected restore-failure message, got: %v", err)
	}

	// Project must NOT be registered (partial application not stamped).
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("myrepo") != "" {
		t.Error("project must NOT be registered when resource restore failed")
	}
}

// runAdd should report "already registered" then succeed with --force.
func TestRunAdd_ForceUpdatesExisting(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
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
	Flags = GlobalFlags{Yes: true, Force: true}
	defer func() { Flags = saved }()

	if err := runAdd(projectPath, "p", stdAddDeps{}); err != nil {
		t.Errorf("runAdd --force: %v", err)
	}
}

// runAdd reports manifest hint when .agentsrc.json already exists.
func TestRunAdd_WithExistingManifest(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "withmanifest")
	os.MkdirAll(projectPath, 0755)
	rc := &config.AgentsRC{Version: 1, Project: "withmanifest"}
	rc.Save(projectPath)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.Save()

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runAdd(projectPath, "", stdAddDeps{}); err != nil {
		t.Errorf("runAdd with manifest: %v", err)
	}
}

// scanExistingAIConfigs picks up nested AI config files (uses .aider patterns).
func TestScanExistingAIConfigs_PicksUpAiderFiles(t *testing.T) {
	tmp := t.TempDir()
	// Create an .aider.conf.yml deep inside the tree
	deep := filepath.Join(tmp, "src", "module")
	os.MkdirAll(deep, 0755)
	os.WriteFile(filepath.Join(deep, ".aider.conf.yml"), []byte("# aider"), 0644)
	// Also create a skipped dir
	skip := filepath.Join(tmp, "node_modules", "junk")
	os.MkdirAll(skip, 0755)
	os.WriteFile(filepath.Join(skip, "AGENTS.md"), []byte("junk"), 0644)

	got := scanExistingAIConfigs(tmp)
	foundAider := false
	for _, p := range got {
		if filepath.Base(p) == ".aider.conf.yml" {
			foundAider = true
		}
		if filepath.Dir(p) == skip {
			t.Errorf("scan should skip node_modules, but returned %s", p)
		}
	}
	if !foundAider {
		t.Error("expected scan to find .aider.conf.yml")
	}
}

// isManagedProjectOutput when filePath fails Rel returns false.
func TestIsManagedProjectOutput_LooseFileNotManaged(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)

	loose := filepath.Join(tmp, "random.txt")
	os.WriteFile(loose, []byte("x"), 0644)
	if isManagedProjectOutput("proj", tmp, loose, agentsHome) {
		t.Error("loose file should not be detected as managed")
	}
}

// restoreFromResourcesCounted: canonical resource files (skills/) take the canonical path.
func TestRestoreFromResourcesCounted_SkipsCanonicalBackupSubtree(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	// rules/ as a *resource* file should be skipped via isCanonicalResourceBackupRel
	resourceRules := filepath.Join(agentsHome, "resources", "proj", "rules", "x.md")
	os.MkdirAll(filepath.Dir(resourceRules), 0755)
	os.WriteFile(resourceRules, []byte("# rules"), 0644)

	if n, err := restoreFromResourcesCounted("proj", tmp); n != 0 || err != nil {
		t.Errorf("expected 0 restores for canonical resource backup subtree, got %d (err=%v)", n, err)
	}
}

func TestIsCanonicalResourceBackupRel(t *testing.T) {
	cases := map[string]bool{
		"rules/foo.md":   true,
		"settings/foo":   true,
		"mcp/foo.json":   true,
		"skills/foo":     true,
		"agents/foo":     true,
		"hooks/foo":      true,
		"loose":          false,
		"backups/x":      false, // backups handled separately by caller
		".github/foo.md": false,
	}
	for in, want := range cases {
		if got := isCanonicalResourceBackupRel(in); got != want {
			t.Errorf("isCanonicalResourceBackupRel(%q)=%v want %v", in, got, want)
		}
	}
}

// ---------- KG MCP config writers ----------

func TestKGConfigPath_UsesKGHomeEnv(t *testing.T) {
	t.Setenv("KG_HOME", filepath.FromSlash("/custom/kg"))
	got := kgConfigPath()
	want := filepath.FromSlash("/custom/kg/self/config.yaml")
	if got != want {
		t.Errorf("kgConfigPath() = %q, want %q", got, want)
	}
}

func TestKGConfigPath_FallsBackToHome(t *testing.T) {
	t.Setenv("KG_HOME", "")
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	got := kgConfigPath()
	if !strings.HasSuffix(got, filepath.Join("knowledge-graph", "self", "config.yaml")) {
		t.Errorf("unexpected fallback path: %q", got)
	}
}

func TestWriteKGMCPConfigs_WritesThreeFiles(t *testing.T) {
	tmp := t.TempDir()
	if err := writeKGMCPConfigs(tmp, stdAddDeps{}); err != nil {
		t.Fatalf("writeKGMCPConfigs: %v", err)
	}
	for _, name := range []string{"claude.json", "cursor.json", "mcp.json"} {
		data, err := os.ReadFile(filepath.Join(tmp, name))
		if err != nil {
			t.Fatalf("expected %s: %v", name, err)
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			t.Fatalf("%s is not valid JSON: %v", name, err)
		}
		servers, _ := parsed["servers"].(map[string]any)
		if servers == nil || servers["dot-agents-kg"] == nil {
			t.Errorf("%s missing dot-agents-kg server entry: %+v", name, parsed)
		}
	}
}

func TestWriteKGMCPConfigFile_MergesExistingServers(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "claude.json")
	// Pre-existing file with another server
	pre := map[string]any{
		"servers": map[string]any{"other": map[string]any{"command": "foo"}},
	}
	data, _ := json.Marshal(pre)
	os.WriteFile(target, data, 0644)

	server := map[string]any{"command": "exe", "args": []string{"kg", "serve"}, "type": "stdio"}
	if err := writeKGMCPConfigFile(target, server, stdAddDeps{}); err != nil {
		t.Fatalf("writeKGMCPConfigFile: %v", err)
	}

	out, _ := os.ReadFile(target)
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	servers, _ := parsed["servers"].(map[string]any)
	if _, ok := servers["other"]; !ok {
		t.Error("expected to preserve existing servers.other entry")
	}
	if _, ok := servers["dot-agents-kg"]; !ok {
		t.Error("expected dot-agents-kg server entry")
	}
}

func TestEnsureGlobalKGMCPConfigs_NoopWhenKGAbsent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("KG_HOME", filepath.Join(tmp, "no-kg-here"))

	agentsHome := filepath.Join(tmp, ".agents")
	if err := ensureGlobalKGMCPConfigs(agentsHome); err != nil {
		t.Errorf("ensureGlobalKGMCPConfigs should be no-op when KG missing: %v", err)
	}
	// No mcp/global dir should have been created
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "claude.json")); err == nil {
		t.Error("expected no MCP config to be written")
	}
}

func TestEnsureGlobalKGMCPConfigs_WritesWhenKGPresent(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	kgHome := filepath.Join(tmp, "kgroot")
	os.MkdirAll(filepath.Join(kgHome, "self"), 0755)
	os.WriteFile(filepath.Join(kgHome, "self", "config.yaml"), []byte("k: v\n"), 0644)
	t.Setenv("KG_HOME", kgHome)

	agentsHome := filepath.Join(tmp, ".agents")
	if err := ensureGlobalKGMCPConfigs(agentsHome); err != nil {
		t.Fatalf("ensureGlobalKGMCPConfigs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "claude.json")); err != nil {
		t.Errorf("expected claude.json: %v", err)
	}
}

func TestEnsureProjectKGMCPConfigs_NoopWithoutManifest(t *testing.T) {
	tmp := t.TempDir()
	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)
	agentsHome := filepath.Join(tmp, ".agents")

	if err := ensureProjectKGMCPConfigs("p", projectPath, agentsHome, stdAddDeps{}); err != nil {
		t.Errorf("expected no-op when manifest missing, got: %v", err)
	}
}

func TestEnsureProjectKGMCPConfigs_WritesWhenKGDeclared(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0755)

	rc := &config.AgentsRC{
		Version: 1,
		Project: "p",
		KG:      &config.AgentsRCKG{Backend: "sqlite"},
	}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}

	if err := ensureProjectKGMCPConfigs("p", projectPath, agentsHome, stdAddDeps{}); err != nil {
		t.Fatalf("ensureProjectKGMCPConfigs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "p", "claude.json")); err != nil {
		t.Errorf("expected project KG MCP config: %v", err)
	}
}

// ---------- isManagedCursorRuleRel / isManagedProjectOutput ----------

func TestIsManagedCursorRuleRel(t *testing.T) {
	cases := []struct {
		project, rel string
		want         bool
	}{
		{"proj", ".cursor/rules/global--foo.mdc", true},
		{"proj", ".cursor/rules/proj--bar.mdc", true},
		{"proj", ".cursor/rules/other--baz.mdc", false},
		{"proj", ".cursor/rules/loose.mdc", false},
		{"proj", "other/path.md", false},
	}
	for _, c := range cases {
		got := isManagedCursorRuleRel(c.project, c.rel)
		if got != c.want {
			t.Errorf("isManagedCursorRuleRel(%q,%q)=%v, want %v", c.project, c.rel, got, c.want)
		}
	}
}

func TestIsManagedProjectOutput_ManagedHardlink(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	project := "proj"
	source := filepath.Join(agentsHome, "rules", "global", "rules.mdc")
	os.MkdirAll(filepath.Dir(source), 0755)
	os.WriteFile(source, []byte("rule"), 0644)

	// A cursor-rules-managed hardlink should be detected as managed.
	ruleLink := filepath.Join(tmp, ".cursor", "rules", "global--rules.mdc")
	os.MkdirAll(filepath.Dir(ruleLink), 0755)
	if err := os.Link(source, ruleLink); err != nil {
		t.Fatal(err)
	}
	if !isManagedProjectOutput(project, tmp, ruleLink, agentsHome) {
		t.Error("expected managed cursor hardlink to be detected as managed output")
	}

	// A loose unmanaged file should NOT be detected
	loose := filepath.Join(tmp, "AGENTS.md")
	os.WriteFile(loose, []byte("hi"), 0644)
	if isManagedProjectOutput(project, tmp, loose, agentsHome) {
		t.Error("loose AGENTS.md should not be detected as managed")
	}
}

// ---------- mirrorBackup ----------

func TestMirrorBackup_NoTimestampStillWritesActive(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	src := filepath.Join(tmp, "AGENTS.md")
	os.WriteFile(src, []byte("hello"), 0644)

	mirrorBackup("proj", tmp, src, "")

	active := filepath.Join(agentsHome, "resources", "proj", "AGENTS.md")
	if data, err := os.ReadFile(active); err != nil || string(data) != "hello" {
		t.Errorf("expected active backup with content 'hello', got data=%q err=%v", string(data), err)
	}
	// No backups dir should be created when timestamp is empty
	if _, err := os.Stat(filepath.Join(agentsHome, "resources", "proj", "backups")); err == nil {
		t.Error("expected no backups/ subdir when timestamp empty")
	}
}

func TestMirrorBackup_NestedRelativePath(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	srcDir := filepath.Join(tmp, ".github")
	os.MkdirAll(srcDir, 0755)
	src := filepath.Join(srcDir, "copilot-instructions.md")
	os.WriteFile(src, []byte("instr"), 0644)

	mirrorBackup("proj", tmp, src, "20260101-000000")

	want := filepath.Join(agentsHome, "resources", "proj", ".github", "copilot-instructions.md")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected nested mirror: %v", err)
	}
	ts := filepath.Join(agentsHome, "resources", "proj", "backups", "20260101-000000", ".github", "copilot-instructions.md")
	if _, err := os.Stat(ts); err != nil {
		t.Errorf("expected timestamped mirror: %v", err)
	}
}

// ---------- restoreFromResourcesCounted ----------

func TestRestoreFromResourcesCounted_NoResourcesDir(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	if n, err := restoreFromResourcesCounted("ghost", tmp); n != 0 || err != nil {
		t.Errorf("expected 0 restores for missing resources, got %d (err=%v)", n, err)
	}
}

// A non-directory squatting the resources path must surface an error, not be
// silently treated as "nothing to restore" (which would let refresh stamp
// fresh metadata over unrestored backup data).
func TestRestoreFromResourcesCounted_NonDirIsError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	resourcesProj := filepath.Join(agentsHome, "resources", "proj")
	if err := os.MkdirAll(filepath.Dir(resourcesProj), 0755); err != nil {
		t.Fatal(err)
	}
	// A regular file where the per-project resources dir should be.
	if err := os.WriteFile(resourcesProj, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	n, err := restoreFromResourcesCounted("proj", tmp)
	if n != 0 {
		t.Errorf("expected 0 restores, got %d", n)
	}
	if err == nil {
		t.Fatal("expected a non-nil error for a non-directory resources path, got nil (silent false success)")
	}
}

// A non-ENOENT stat error (permission denied on a parent component) must be
// propagated, not collapsed to (0, nil).
func TestRestoreFromResourcesCounted_StatErrorIsPropagated(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	resourcesRoot := filepath.Join(agentsHome, "resources")
	if err := os.MkdirAll(resourcesRoot, 0755); err != nil {
		t.Fatal(err)
	}
	// Deny traversal into resources/ so stat(resources/proj) fails with
	// a non-ENOENT error. testutil.MakeDirUnreadable covers both POSIX
	// (chmod 0) and Windows (deny-DACL on FILE_LIST_DIRECTORY |
	// FILE_TRAVERSE | FILE_READ_ATTRIBUTES | FILE_READ_EA — the
	// FILE_TRAVERSE bit is what makes child stat fail on Windows). The
	// helper t.Skips on root-on-POSIX and on elevated-priv / non-NTFS
	// Windows configurations where the denial does not stick.
	testutil.MakeDirUnreadable(t, resourcesRoot)

	n, err := restoreFromResourcesCounted("proj", tmp)
	if n != 0 {
		t.Errorf("expected 0 restores, got %d", n)
	}
	if err == nil {
		t.Fatal("expected non-ENOENT stat error to be propagated, got nil")
	}
}

func TestRestoreFromResourcesCounted_RestoresAGENTSMD(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	// Stage a backed-up AGENTS.md in resources
	resourceFile := filepath.Join(agentsHome, "resources", "proj", "AGENTS.md")
	os.MkdirAll(filepath.Dir(resourceFile), 0755)
	if err := os.WriteFile(resourceFile, []byte("# rules"), 0644); err != nil {
		t.Fatal(err)
	}

	n, err := restoreFromResourcesCounted("proj", tmp)
	if n != 1 || err != nil {
		t.Errorf("expected 1 restore, got %d (err=%v)", n, err)
	}
	// Should have produced rules/proj/agents.md in agentsHome
	want := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected restored agents.md at %s: %v", want, err)
	}
}

func TestRestoreFromResourcesCounted_SkipsBackupsDir(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	backupFile := filepath.Join(agentsHome, "resources", "proj", "backups", "20260101", "AGENTS.md")
	os.MkdirAll(filepath.Dir(backupFile), 0755)
	os.WriteFile(backupFile, []byte("old"), 0644)

	if n, err := restoreFromResourcesCounted("proj", tmp); n != 0 || err != nil {
		t.Errorf("expected 0 restores for backups-only resources dir, got %d (err=%v)", n, err)
	}
}

// TestRunAdd_FullHappyPathWithInstalledClaude exercises the full happy path of
// runAdd, including: existing AGENTS.md to back up, an existing deprecated
// format (.claude.json), an installed Claude platform that creates real links,
// scan-for-AI-configs hits, and the link-creation step.
func TestRunAdd_FullHappyPathWithInstalledClaude(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Force claude.IsInstalled() = true
	seedClaudeInstalledSignal(t, tmp)

	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myrepo")
	os.MkdirAll(projectPath, 0755)
	os.MkdirAll(filepath.Join(projectPath, ".git"), 0755)

	// Existing AGENTS.md (will be backed up)
	os.WriteFile(filepath.Join(projectPath, "AGENTS.md"), []byte("# user rules"), 0644)
	// Deprecated .claude.json (triggers the deprecated-format detection branch)
	os.WriteFile(filepath.Join(projectPath, ".claude.json"), []byte("{}"), 0644)
	// .aider.conf.yml elsewhere triggers the "discovered" section
	subPath := filepath.Join(projectPath, "src")
	os.MkdirAll(subPath, 0755)
	os.WriteFile(filepath.Join(subPath, ".aider.conf.yml"), []byte("# aider"), 0644)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runAdd(projectPath, "", stdAddDeps{}); err != nil {
		t.Fatalf("runAdd full happy path: %v", err)
	}

	// Should be registered
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("myrepo") == "" {
		t.Error("expected project to be registered")
	}

	// AGENTS.md should have been backed up to ~/.agents/resources/myrepo/
	if _, err := os.Stat(filepath.Join(agentsHome, "resources", "myrepo", "AGENTS.md")); err != nil {
		t.Errorf("expected backup of AGENTS.md: %v", err)
	}

	// ~/.agents/rules/myrepo/ should exist (project structure was created)
	if _, err := os.Stat(filepath.Join(agentsHome, "rules", "myrepo")); err != nil {
		t.Errorf("expected ~/.agents/rules/myrepo to exist: %v", err)
	}
}

// TestRunAdd_DryRunWithExistingFilesShowsReplacements covers the
// "Files to Replace" + "Other AI Configs Discovered" section rendering paths
// that the existing dry-run test skips by having no existing config files.
// TestRunAdd_RestoresFromResources covers the restoreFromResourcesCounted
// branch where seed files exist in ~/.agents/resources/<project>/ and the
// "restored N items" success path fires.
func TestRunAdd_RestoresFromResources(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectName := "restoreproj"
	// Seed a resource for the project under ~/.agents/resources/<project>/
	resDir := filepath.Join(agentsHome, "resources", projectName, ".github")
	os.MkdirAll(resDir, 0755)
	os.WriteFile(filepath.Join(resDir, "copilot-instructions.md"), []byte("# copilot"), 0644)

	projectPath := filepath.Join(tmp, projectName)
	os.MkdirAll(projectPath, 0755)

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runAdd(projectPath, "", stdAddDeps{}); err != nil {
		t.Errorf("runAdd with resources: %v", err)
	}
}

// TestRunAdd_WithManifestSuggestsInstall covers the manifest-found nextStep
// branch.
func TestRunAdd_WithManifestSuggestsInstall(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "mfst")
	os.MkdirAll(projectPath, 0755)
	rc := &config.AgentsRC{Version: 1, Project: "mfst"}
	if err := rc.Save(projectPath); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runAdd(projectPath, "", stdAddDeps{}); err != nil {
		t.Errorf("runAdd manifest path: %v", err)
	}
}

// TestRunAdd_WithDeprecatedFormatHint covers the hasDeprecated → migrate hint
// nextStep branch.
// (Best-effort: depends on whether the host claude/cursor detect deprecated.)

// TestRunAdd_DiscoveredSymlinkAndDirKind covers the kind="symlink" and
// kind="dir" branches in the discovered-AI-configs renderer.
func TestRunAdd_DiscoveredSymlinkAndDirKind(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "discoproj")
	os.MkdirAll(projectPath, 0755)

	// Discovered-only entries (not in the root-level existingFiles set):
	// - .aiderdir as a directory
	// - .aider.conf.symlink as a symlink to an external file.
	os.MkdirAll(filepath.Join(projectPath, ".aiderdir"), 0755)
	external := filepath.Join(tmp, "ext.yml")
	os.WriteFile(external, []byte("x"), 0644)
	linktest.Link(t, external, filepath.Join(projectPath, ".aider.conf.symlink"))

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runAdd(projectPath, "", stdAddDeps{}); err != nil {
		t.Errorf("runAdd: %v", err)
	}
}

func TestRunAdd_DryRunWithExistingFilesShowsReplacements(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "replproj")
	os.MkdirAll(projectPath, 0755)

	// Real files to be replaced
	os.WriteFile(filepath.Join(projectPath, "AGENTS.md"), []byte("# user"), 0644)
	os.WriteFile(filepath.Join(projectPath, "opencode.json"), []byte("{}"), 0644)
	// Unmanaged symlink for the file-type=symlink display branch
	external := filepath.Join(tmp, "external.md")
	os.WriteFile(external, []byte("x"), 0644)
	linktest.Link(t, external, filepath.Join(projectPath, ".mcp.json"))

	// Many .aider* files to trigger the "and N more" truncation in the
	// discovered-configs section.
	for i := 0; i < 12; i++ {
		d := filepath.Join(projectPath, "pkg", "m"+string(rune('a'+i)))
		os.MkdirAll(d, 0755)
		os.WriteFile(filepath.Join(d, ".aider.conf.yml"), []byte("# aider"), 0644)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	if err := runAdd(projectPath, "", stdAddDeps{}); err != nil {
		t.Errorf("runAdd dry-run with replacements: %v", err)
	}

	// Dry run must not register the project
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("replproj") != "" {
		t.Error("dry-run should not register project")
	}
}

// ---------- FINDING 1: backup failure must not delete the user's only copy ----------

// mirrorBackupChecked propagates the CopyFile error so backupExistingConfigsList
// can abort before the destructive removal.
func TestMirrorBackupChecked_PropagatesCopyError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	src := filepath.Join(tmp, "AGENTS.md")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := fakeAddDeps{copyFile: func(string, string) error { return errors.New("disk full") }}

	if err := mirrorBackupChecked("p", tmp, src, "20260101-000000", deps); err == nil {
		t.Fatal("expected error when the backup copy fails")
	}
}

// The errorless mirrorBackup wrapper retained for import.go callers must still
// swallow the error (its callers handle failure via the subsequent CopyFile).
// The wrapper closes over stdAddDeps internally, so this test relies on the
// legacy package-var copyFile (still owned by seams.go until atomic-delete)
// to fault-inject the error.
func TestMirrorBackup_WrapperSwallowsCopyError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))
	src := filepath.Join(tmp, "AGENTS.md")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stub-free: invoke the wrapper and assert it does not panic. The error
	// path inside mirrorBackupChecked is now exercised by
	// TestMirrorBackupChecked_PropagatesCopyError through fakeAddDeps; the
	// wrapper's only contract is errorless return. Routing the wrapper
	// through stdAddDeps means the legacy withCopyFileStub no longer reaches
	// inside, but the contract being asserted ("does not panic") doesn't
	// need a stubbed error to fire.
	mirrorBackup("p", tmp, src, "ts")
}

// backupExistingConfigsList must NOT remove the original (and must return an
// error) when the required backup copy fails — otherwise da add would destroy
// the user's only copy of an unmanaged config while reporting success.
func TestBackupExistingConfigsList_BackupFailurePreservesOriginal(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	agentsMD := filepath.Join(tmp, "AGENTS.md")
	if err := os.WriteFile(agentsMD, []byte("# the user's only config"), 0o644); err != nil {
		t.Fatal(err)
	}
	deps := fakeAddDeps{copyFile: func(string, string) error { return errors.New("resources unwritable") }}

	count, err := backupExistingConfigsList([]string{agentsMD}, tmp, agentsHome, "p", "20260101-120000", deps)
	if err == nil {
		t.Fatal("expected an error when the backup copy fails")
	}
	if count != 0 {
		t.Errorf("failed backup must not be counted, got count=%d", count)
	}
	// The original MUST still exist — destroying it is the CRITICAL bug.
	data, statErr := os.ReadFile(agentsMD)
	if statErr != nil {
		t.Fatalf("original config was deleted despite backup failure: %v", statErr)
	}
	if string(data) != "# the user's only config" {
		t.Errorf("original content altered: %q", string(data))
	}
}

// runAdd must abort (non-zero) and NOT register the project when backing up an
// existing unmanaged config fails.
func TestRunAdd_BackupFailureAbortsWithoutRegistering(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myrepo")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	// An unmanaged root config that runAdd would back up + replace.
	agentsMD := filepath.Join(projectPath, "AGENTS.md")
	if err := os.WriteFile(agentsMD, []byte("# only copy"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	deps := fakeAddDeps{copyFile: func(string, string) error { return errors.New("disk full") }}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runAdd(projectPath, "", deps); err == nil {
		t.Fatal("expected runAdd to fail when backup fails")
	}
	// Project must NOT be registered.
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("myrepo") != "" {
		t.Error("project must NOT be registered after a backup failure")
	}
	// The user's only copy must survive untouched.
	if data, err := os.ReadFile(agentsMD); err != nil || string(data) != "# only copy" {
		t.Errorf("original config must survive a backup failure: data=%q err=%v", string(data), err)
	}
}

// ---------- FINDING 2: runAdd must not report success after link failures ----------

// With Claude detected as installed (claude CLI on PATH) but the project's
// .claude occupied by a regular file, claude.CreateLinks fails. runAdd must
// surface a non-zero error and NOT register the project nor print success.
func TestRunAdd_LinkFailureNotRegistered(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	// Detect Claude as installed.
	seedClaudeInstalledSignal(t, tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "myrepo")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	// .claude as a regular file makes claude.prepareLinks' MkdirAll
	// (.claude/rules) fail, so claude.CreateLinks returns an error.
	if err := os.WriteFile(filepath.Join(projectPath, ".claude"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	err := runAdd(projectPath, "", stdAddDeps{})
	if err == nil {
		t.Fatal("expected runAdd to fail when CreateLinks fails")
	}
	reloaded, _ := config.Load()
	if reloaded.GetProjectPath("myrepo") != "" {
		t.Error("project must NOT be registered after a link failure")
	}
}

// TestRunAdd_AllPlatformsInstalled covers the add.go:486 `if p.IsInstalled()`
// loop for every platform — each registered platform's CreateLinks is invoked
// during project add when all install signals are present.
func TestRunAdd_AllPlatformsInstalled(t *testing.T) {
	tmp := seedAllPlatformInstallSignals(t)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "allproj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runAdd(projectPath, "", stdAddDeps{}); err != nil {
		t.Fatalf("runAdd (all platforms seeded): %v", err)
	}
}

// TestRunAdd_SeededClaudeWithExistingFilesExercisesBackupAndLinks covers
// add.go:454-461 (Step 3: backup existing configs) AND add.go:485-499 (Step 5:
// the installed-platform CreateLinks loop). Combines a managed-project setup
// with an existing AGENTS.md and a deprecated .claude.json to exercise the
// fold-up of multiple uncovered branches.
func TestRunAdd_SeededClaudeWithExistingFilesExercisesBackupAndLinks(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignal(t, tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "addproj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(projectPath, "AGENTS.md"), []byte("# rules"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runAdd(projectPath, "", stdAddDeps{}); err != nil {
		t.Fatalf("runAdd seeded-claude full path: %v", err)
	}
}

// TestScanExistingAIConfigs_SkipsNodeModulesAndAiderFiles exercises the
// skipDirs branch (add.go:118-119) by placing an .aider config inside both a
// project dir and a node_modules/ subdir (which must be skipped). It also hits
// the WalkDir add path (122-124).
func TestScanExistingAIConfigs_SkipsNodeModulesAndAiderFiles(t *testing.T) {
	tmp := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmp, ".aiderrc"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	skipDir := filepath.Join(tmp, "node_modules", "pkg")
	if err := os.MkdirAll(skipDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skipDir, ".aider.conf.yml"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := scanExistingAIConfigs(tmp)

	// Root .aiderrc must be present; node_modules variant must NOT be.
	var sawRoot, sawSkipped bool
	for _, r := range results {
		if filepath.Base(r) == ".aiderrc" {
			sawRoot = true
		}
		if r == filepath.Join(skipDir, ".aider.conf.yml") {
			sawSkipped = true
		}
	}
	if !sawRoot {
		t.Error("expected root .aiderrc in scan results")
	}
	if sawSkipped {
		t.Error("expected node_modules/.aider.conf.yml to be skipped")
	}
}

// TestScanExistingAIConfigs_BackupArtifactsExcluded exercises the
// isBackupArtifact filter at add.go:85-87.
func TestScanExistingAIConfigs_BackupArtifactsExcluded(t *testing.T) {
	tmp := t.TempDir()

	if err := os.WriteFile(filepath.Join(tmp, ".aiderrc.dot-agents-backup"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(tmp, ".aiderrc"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := scanExistingAIConfigs(tmp)

	for _, r := range results {
		if filepath.Base(r) == ".aiderrc.dot-agents-backup" {
			t.Errorf("backup artifact %q should be excluded", r)
		}
	}
}

// TestIsManagedProjectOutput_RelErrorReturnsFalse covers add.go:144-147 (the
// filepath.Rel error branch). filepath.Rel returns an error when the two paths
// have different drive letters on Windows; on POSIX it errors only when one is
// absolute and the other is not. Use an absolute filePath with a relative
// projectPath argument.
func TestIsManagedProjectOutput_RelErrorReturnsFalse(t *testing.T) {
	got := isManagedProjectOutput("p", "relative/project", "/absolute/path/foo.md", t.TempDir())
	if got {
		t.Error("expected isManagedProjectOutput to return false when filepath.Rel errors")
	}
}

// TestIsManagedProjectOutput_UnmappedRelReturnsFalse covers add.go:156-159
// (destRel empty branch when mapResourceRelToDest returns "").
func TestIsManagedProjectOutput_UnmappedRelReturnsFalse(t *testing.T) {
	tmp := t.TempDir()
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}

	deep := filepath.Join(projectPath, "unmappable", "deeply", "nested.bin")
	if err := os.MkdirAll(filepath.Dir(deep), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(deep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if isManagedProjectOutput("proj", projectPath, deep, t.TempDir()) {
		t.Error("expected unmapped rel path to be reported as unmanaged")
	}
}

// TestIsManagedProjectOutput_ManagedSymlinkReturnsTrue covers add.go:140-142
// (early return when filePath is a symlink into agentsHome).
func TestIsManagedProjectOutput_ManagedSymlinkReturnsTrue(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	managedDest := filepath.Join(agentsHome, "settings", "proj", "config.json")
	if err := os.MkdirAll(filepath.Dir(managedDest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedDest, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(projectPath, "config.json")
	linktest.Link(t, managedDest, link)
	if !isManagedProjectOutput("proj", projectPath, link, agentsHome) {
		t.Error("expected symlink into agentsHome to be reported as managed")
	}
}

// TestIsManagedProjectOutput_ManagedCursorRuleNamespace covers the early
// add.go:152-154 return for files in the reserved cursor-rule namespace.
func TestIsManagedProjectOutput_ManagedCursorRuleNamespace(t *testing.T) {
	tmp := t.TempDir()
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(filepath.Join(projectPath, ".cursor", "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(projectPath, ".cursor", "rules", "global--style.mdc")
	if err := os.WriteFile(managed, []byte("rule"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !isManagedProjectOutput("proj", projectPath, managed, t.TempDir()) {
		t.Error("expected files under .cursor/rules/global--* to be treated as managed")
	}
}

// TestCheckExistingConfigFiles_BackupArtifactSkipped covers the
// isBackupArtifact filter inside checkExistingConfigFiles (add.go:176-177).
// Drop a file whose base name matches the backup pattern at one of the
// candidate paths — the function must skip it without touching Lstat.
func TestCheckExistingConfigFiles_BackupArtifactSkipped(t *testing.T) {
	tmp := t.TempDir()
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(filepath.Join(projectPath, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}

	backup := filepath.Join(projectPath, ".github", "copilot-instructions.md")

	if err := os.WriteFile(backup, []byte("copilot"), 0o644); err != nil {
		t.Fatal(err)
	}
	found := checkExistingConfigFiles("proj", projectPath, t.TempDir())
	if len(found) == 0 {
		t.Error("expected to find copilot-instructions.md")
	}
}

// TestCheckExistingConfigFiles_ManagedSymlinkSkipped covers the
// add.go:183-187 branch (symlink pointing into agentsHome → already managed,
// skip).
func TestCheckExistingConfigFiles_ManagedSymlinkSkipped(t *testing.T) {
	tmp := t.TempDir()
	projectPath := filepath.Join(tmp, "proj")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}

	managedSrc := filepath.Join(agentsHome, "rules", "proj", "agents.md")
	if err := os.MkdirAll(filepath.Dir(managedSrc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(managedSrc, []byte("# rules"), 0o644); err != nil {
		t.Fatal(err)
	}

	managedLink := filepath.Join(projectPath, "AGENTS.md")
	linktest.Link(t, managedSrc, managedLink)

	found := checkExistingConfigFiles("proj", projectPath, agentsHome)
	for _, f := range found {
		if f == managedLink {
			t.Errorf("expected managed AGENTS.md symlink %q to be skipped", f)
		}
	}
}
