package commands

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
)

// fakeInitDeps is the interface-DI test double for initDeps (per
// docs/TEST_SEAMS.md). A nil func field delegates to the real os
// implementation, so a test overrides only the operation it wants to
// fault-inject.
type fakeInitDeps struct {
	mkdirAll func(string, os.FileMode) error
}

func (f fakeInitDeps) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAll != nil {
		return f.mkdirAll(path, perm)
	}
	return os.MkdirAll(path, perm)
}

// TestFakeInitDeps_NilDelegatesToReal pins the nil-delegates-to-real
// contract documented on fakeInitDeps so a test that omits mkdirAll
// genuinely creates the dir (rather than silently succeeding without
// touching the filesystem). Without this, a future change to the fake's
// default branch could regress every happy-path-but-not-overridden test
// without any of them failing.
func TestFakeInitDeps_NilDelegatesToReal(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "delegate", "nested")
	if err := (fakeInitDeps{}).MkdirAll(target, 0o755); err != nil {
		t.Fatalf("nil-mkdirAll delegate: %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("real MkdirAll did not create %s: %v", target, err)
	}
	if !info.IsDir() {
		t.Errorf("delegated MkdirAll produced non-dir at %s", target)
	}
}

func TestNewInitCmd_Metadata(t *testing.T) {
	cmd := NewInitCmd()
	if cmd.Use != "init" {
		t.Errorf("expected Use=init, got %q", cmd.Use)
	}
	if cmd.RunE == nil {
		t.Error("expected RunE to be set")
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Errorf("init expects no args, but got: %v", err)
	}
	if err := cmd.Args(cmd, []string{"x"}); err == nil {
		t.Error("init should reject positional args")
	}
}

func TestRunInit_DryRunMakesNoChanges(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{DryRun: true, Yes: true}
	defer func() { Flags = saved }()

	if err := runInit(NewInitCmd(), nil, stdInitDeps{}); err != nil {
		t.Fatalf("runInit dry-run: %v", err)
	}
	if _, err := os.Stat(agentsHome); !os.IsNotExist(err) {
		t.Error("dry-run should not create ~/.agents/")
	}
}

func TestRunInit_ExistingHomeWithoutForceIsNoop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(agentsHome, "preserved.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runInit(NewInitCmd(), nil, stdInitDeps{}); err != nil {
		t.Fatalf("runInit on existing home: %v", err)
	}
	// Sentinel should be intact
	if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
		t.Errorf("existing files should be preserved without --force; got data=%q err=%v", string(data), err)
	}
	// No new config.json should have been written (init returned early)
	if _, err := os.Stat(filepath.Join(agentsHome, "config.json")); err == nil {
		t.Error("init should have been a no-op (config.json appeared)")
	}
}

func TestRunInit_FreshInstallCreatesStructure(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runInit(NewInitCmd(), nil, stdInitDeps{}); err != nil {
		t.Fatalf("runInit: %v", err)
	}

	// Core dirs
	for _, sub := range []string{
		"resources",
		"rules/global",
		"settings/global",
		"mcp/global",
		"skills/global",
		"agents/global",
		"hooks/global",
		"scripts",
		"local",
	} {
		if _, err := os.Stat(filepath.Join(agentsHome, sub)); err != nil {
			t.Errorf("expected %s to exist: %v", sub, err)
		}
	}

	// config.json should be initialized
	if _, err := os.Stat(filepath.Join(agentsHome, "config.json")); err != nil {
		t.Errorf("config.json should exist: %v", err)
	}
	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Version != 1 {
		t.Errorf("expected config version 1, got %d", loaded.Version)
	}
	if loaded.Projects == nil {
		t.Error("expected initialized projects map")
	}
}

func TestRunInit_ForceReinitializes(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	// Existing pre-init residue
	os.WriteFile(filepath.Join(agentsHome, "config.json"), []byte("{}"), 0644)

	saved := Flags
	Flags = GlobalFlags{Yes: true, Force: true}
	defer func() { Flags = saved }()

	if err := runInit(NewInitCmd(), nil, stdInitDeps{}); err != nil {
		t.Fatalf("runInit --force: %v", err)
	}

	// After force re-init, expected canonical dirs should be present.
	for _, sub := range []string{"rules/global", "settings/global", "mcp/global"} {
		if _, err := os.Stat(filepath.Join(agentsHome, sub)); err != nil {
			t.Errorf("expected %s to exist after --force: %v", sub, err)
		}
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Version != 1 {
		t.Errorf("expected config version 1 after force, got %d", loaded.Version)
	}
}

// init --force over an existing UNMANAGED ~/.claude/settings.json must
// preserve it as a sidecar <path>.dot-agents-backup and install the managed
// link — never destroy the user's file and never report false success.
func TestRunInit_ForcePreservesUnmanagedClaudeSettings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	// Make claude "installed" via the ~/.claude directory probe.
	claudeDir := filepath.Join(tmp, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Pre-existing UNMANAGED user settings.json (a real regular file).
	claudeSettings := filepath.Join(claudeDir, "settings.json")
	userData := []byte(`{"user":"do-not-lose-me"}`)
	if err := os.WriteFile(claudeSettings, userData, 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}

	saved := Flags
	Flags = GlobalFlags{Yes: true, Force: true}
	defer func() { Flags = saved }()

	if err := runInit(NewInitCmd(), nil, stdInitDeps{}); err != nil {
		t.Fatalf("runInit --force: %v", err)
	}

	// The user's original bytes must survive as a sidecar backup.
	bak := claudeSettings + ".dot-agents-backup"
	got, err := os.ReadFile(bak)
	if err != nil {
		t.Fatalf("unmanaged user settings.json was not preserved as %s: %v", bak, err)
	}
	if string(got) != string(userData) {
		t.Errorf("sidecar backup content mismatch: %q", string(got))
	}

	// settings.json must now be a managed link whose target resolves under
	// the canonical agents root (not the old user regular file).
	if !links.IsManagedLinkUnder(claudeSettings, agentsHome) {
		// Windows hard-link model has no resolvable target; fall back to
		// asserting it is at least a managed link distinct from the old
		// user bytes.
		if !links.IsManagedFileLink(claudeSettings) {
			t.Error("expected ~/.claude/settings.json to be a managed link after --force")
		}
		if d, err := os.ReadFile(claudeSettings); err == nil && string(d) == string(userData) {
			t.Error("settings.json still holds the old unmanaged user bytes — link not installed")
		}
	}
}

func TestSidecarBackupFile(t *testing.T) {
	tmp := t.TempDir()

	// Read failure: source does not exist.
	if err := sidecarBackupFile(filepath.Join(tmp, "missing")); err == nil {
		t.Error("expected error backing up a missing file")
	}

	// Happy path: bytes copied to <path>.dot-agents-backup.
	src := filepath.Join(tmp, "settings.json")
	if err := os.WriteFile(src, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := sidecarBackupFile(src); err != nil {
		t.Fatalf("sidecarBackupFile: %v", err)
	}
	got, err := os.ReadFile(src + ".dot-agents-backup")
	if err != nil || string(got) != "keep" {
		t.Errorf("backup content mismatch: %q (err=%v)", string(got), err)
	}

	assertSidecarBackupWriteFailureSurfaces(t, tmp)
}

// assertSidecarBackupWriteFailureSurfaces verifies sidecarBackupFile
// propagates a write error when the backup destination directory is
// unwritable. On Windows os.Chmod(0555) only sets the read-only attribute
// on the directory itself, which does not prevent creating/writing
// children, so this fault cannot be injected there (covered on POSIX);
// likewise root bypasses permission bits.
func assertSidecarBackupWriteFailureSurfaces(t *testing.T, tmp string) {
	t.Helper()
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		return
	}
	ro := filepath.Join(tmp, "ro")
	if err := os.MkdirAll(ro, 0755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0755) })
	roSrc := filepath.Join(ro, "f")
	if err := os.WriteFile(roSrc, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(ro, 0555); err != nil {
		t.Fatal(err)
	}
	if err := sidecarBackupFile(roSrc); err == nil {
		t.Error("expected error writing backup into a read-only directory")
	}
}

func TestScaffoldStarterHomeAssets_CreatesContent(t *testing.T) {
	tmp := t.TempDir()
	if err := scaffoldStarterHomeAssets(tmp); err != nil {
		t.Fatalf("scaffoldStarterHomeAssets: %v", err)
	}
	// Ensure at least one entry was scaffolded
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Error("expected scaffold to write at least one entry")
	}
}

// ---------- additional coverage ----------

// scaffoldStarterHomeAssets returns nil when called on a populated directory (idempotent).
func TestScaffoldStarterHomeAssets_Idempotent(t *testing.T) {
	tmp := t.TempDir()
	if err := scaffoldStarterHomeAssets(tmp); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := scaffoldStarterHomeAssets(tmp); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}
}

// scaffoldWorkflowAssets must also create hooks dir if missing.
func TestScaffoldWorkflowAssets_NoHooksDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	// Do NOT pre-create hooks/global - exercises the auto-create branch.
	if err := scaffoldWorkflowAssets(agentsHome, stdInitDeps{}); err != nil {
		t.Fatalf("scaffoldWorkflowAssets without pre-existing hooks dir: %v", err)
	}
}

func TestScaffoldWorkflowAssets_CreatesHookBundleRoot(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := os.MkdirAll(filepath.Join(agentsHome, "hooks", "global"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := scaffoldWorkflowAssets(agentsHome, stdInitDeps{}); err != nil {
		t.Fatalf("scaffoldWorkflowAssets: %v", err)
	}
	// Context dir is required side-effect
	if _, err := os.Stat(config.AgentsContextDir()); err != nil {
		t.Errorf("context dir should be created: %v", err)
	}
}

// TestRunInit_AllPlatformsInstalledSeeded exercises the init.go IsInstalled
// branches for claude AND cursor (the two platforms init checks directly),
// AND the platform.All() loop at init.go:103-115 which records detected
// platforms in config.json.
func TestRunInit_AllPlatformsInstalledSeeded(t *testing.T) {
	tmp := seedAllPlatformInstallSignals(t)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runInit(NewInitCmd(), nil, stdInitDeps{}); err != nil {
		t.Fatalf("runInit (all platforms seeded): %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, id := range []string{"claude", "cursor", "codex", "opencode", "copilot"} {
		if !cfg.IsPlatformEnabled(id) {
			t.Errorf("expected %s enabled after init with all signals seeded", id)
		}
	}
}

// TestRunInit_SeededClaudeExercisesClaudeSettingsBranch covers the
// init.go:142-153 block where claudePlatform.IsInstalled() == true and the
// global ~/.claude/settings.json symlink is created. It also exercises the
// Lstat-exists no-force branch (line 152) on a second --force=false run.
func TestRunInit_SeededClaudeExercisesClaudeSettingsBranch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runInit(NewInitCmd(), nil, stdInitDeps{}); err != nil {
		t.Fatalf("runInit (claude seeded): %v", err)
	}

	claudeSettings := filepath.Join(tmp, ".claude", "settings.json")
	if _, err := os.Lstat(claudeSettings); err != nil {
		t.Errorf("expected ~/.claude/settings.json after init with claude installed: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.IsPlatformEnabled("claude") {
		t.Errorf("expected claude to be enabled in config after init")
	}
}

// TestRunInit_ForceWithSeededClaudeOverwritesSettings exercises the
// init.go:148 Force branch (existing settings.json + Force -> re-symlink).
func TestRunInit_ForceWithSeededClaudeOverwritesSettings(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(tmp, ".claude", "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true, Force: true}
	defer func() { Flags = saved }()

	if err := runInit(NewInitCmd(), nil, stdInitDeps{}); err != nil {
		t.Fatalf("runInit --force (claude seeded): %v", err)
	}
}

// TestRunInit_SeededClaudeAndExistingSettingsSkipsWithoutForce covers the
// init.go:151-153 else-branch (settings exists, no --force, skip with bullet).
func TestRunInit_SeededClaudeAndExistingSettingsSkipsWithoutForce(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(tmp, ".claude", "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runInit(NewInitCmd(), nil, stdInitDeps{}); err != nil {
		t.Fatalf("runInit (skip claude settings without force): %v", err)
	}

	info, err := os.Lstat(filepath.Join(tmp, ".claude", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("expected pre-existing settings.json to be preserved (no --force)")
	}
}

// TestRunInit_HooksSrcExistsRedirectsSettingsPath covers init.go:139-141
// (when hooks/global/claude-code.json exists, claudeSettingsPath is repointed
// to the hooks source). Run a first init to scaffold the home, then write a
// hooks source file, then re-init with --force to trigger the redirect branch.
func TestRunInit_HooksSrcExistsRedirectsSettingsPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runInit(NewInitCmd(), nil, stdInitDeps{}); err != nil {
		t.Fatalf("runInit first pass: %v", err)
	}

	hooksSrc := filepath.Join(agentsHome, "hooks", "global", "claude-code.json")
	if err := os.MkdirAll(filepath.Dir(hooksSrc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksSrc, []byte(`{"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	Flags = GlobalFlags{Yes: true, Force: true}

	if err := runInit(NewInitCmd(), nil, stdInitDeps{}); err != nil {
		t.Fatalf("runInit --force after hooks seed: %v", err)
	}
}

// TestRunInit_MkdirOnClaudeBranchSucceedsOnEmptyDir is a thin coverage test
// to ensure the seeded-claude init flow does not regress when ~/.claude is
// initially empty (no settings.json present yet — IsNotExist branch).
func TestRunInit_MkdirOnClaudeBranchSucceedsOnEmptyDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	if err := runInit(NewInitCmd(), nil, stdInitDeps{}); err != nil {
		t.Fatalf("runInit IsNotExist branch: %v", err)
	}

	settingsLink := filepath.Join(tmp, ".claude", "settings.json")
	if _, err := os.Lstat(settingsLink); err != nil {
		t.Fatalf("expected ~/.claude/settings.json: %v", err)
	}

	target := filepath.Join(agentsHome, "settings", "global", "claude-code.json")
	if hooksSrc := filepath.Join(agentsHome, "hooks", "global", "claude-code.json"); func() bool {
		_, err := os.Stat(hooksSrc)
		return err == nil
	}() {
		target = hooksSrc
	}
	if !links.IsManagedLink(settingsLink, target) {
		t.Errorf("expected settings.json to be a managed link to %s after init", target)
	}
}
