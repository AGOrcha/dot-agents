package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/AGOrcha/dot-agents/internal/testutil"
	"github.com/spf13/cobra"
)

// newInitCmdForTest constructs a minimal *cobra.Command that mirrors the
// production shim's wiring (Use/Short/Long/Example/Args/RunE) so the
// in-package tests can exercise NewInitCmd-equivalent behavior without
// the package itself exporting NewInitCmd — the shim in
// commands/init.go owns NewInitCmd so the RunE closure resolves under
// the commands package symbol globalflagcov already indexes.
func newInitCmdForTest() *cobra.Command {
	cmd := &cobra.Command{
		Use:     InitCmdUse,
		Short:   InitCmdShort,
		Long:    InitCmdLong,
		Example: InitCmdExample,
		Args:    InitNoArgs(InitCmdNoArgsHint),
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunInit(cmd, args)
		},
	}
	return cmd
}

// seedAllPlatformInstallSignalsLifecycle sets up HOME and PATH so every
// platform's IsInstalled() returns true. The bin directory is automatically
// cleaned by t.TempDir(). Returns the temp HOME path.
//
// Mirrors the parent-package commands.seedAllPlatformInstallSignals
// helper. Duplicated rather than shared because t05's bundle write_scope
// limits this task to the init files only; a shared
// commands/lifecycle/testutil_test.go lands as part of t11's seams_test
// split. Keep the two copies behavior-identical until then.
func seedAllPlatformInstallSignalsLifecycle(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PATH/shim seeding semantics differ on Windows; skip there")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// ~/.claude is no longer the claude install signal (that is the PATH shim
	// seeded below), but managed-settings logic reads files under it, so keep
	// the directory present.
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	copilotExt := filepath.Join(tmp, ".vscode", "extensions", "github.copilot-1.0.0")
	if err := os.MkdirAll(copilotExt, 0o755); err != nil {
		t.Fatal(err)
	}

	seedCLIShimsOnPathLifecycle(t, tmp, "claude", "agent", "codex", "opencode")

	return tmp
}

// seedCLIShimsOnPathLifecycle writes a POSIX shim for each named CLI into a
// fakebin directory under root and prepends it to PATH so exec.LookPath(name)
// resolves for the duration of the test. Each shim prints "<name> 0.0.0" so a
// --version probe also succeeds. Skips on Windows, where the shim contract
// differs.
//
// Package-local twin of commands.seedCLIShimsOnPath — the lifecycle tests
// cannot import the parent package's test helper. It is the single place in
// this package that fabricates a CLI on PATH so the shim-writing logic is never
// duplicated across the init/install/doctor/status install-signal tests.
func seedCLIShimsOnPathLifecycle(t *testing.T, root string, names ...string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PATH/shim seeding semantics differ on Windows; skip there")
	}
	binDir := filepath.Join(root, "fakebin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := "#!/bin/sh\necho \"$(basename \"$0\") 0.0.0\"\n"
	for _, name := range names {
		p := filepath.Join(binDir, name)
		if err := os.WriteFile(p, []byte(shim), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// seedClaudeInstalledSignalLifecycle makes claude.IsInstalled() report true by
// placing a `claude` CLI shim on PATH (the detection seam since ~/.claude
// stopped being an install signal). Tests that previously created ~/.claude
// purely to be detected as installed call this instead.
func seedClaudeInstalledSignalLifecycle(t *testing.T, root string) {
	t.Helper()
	seedCLIShimsOnPathLifecycle(t, root, "claude")
}

// resetInitSeams restores initForce/initDryRun/initYes to zero after a
// test mutates them. Replaces the saved-Flags / defer-restore idiom used
// when these were fields on commands.Flags. The getter Fns are
// untouched here — tests in this package don't repoint them (only the
// commands/init.go shim does), and the defaults read the unexported
// vars this function resets.
func resetInitSeams() {
	initForce = false
	initDryRun = false
	initYes = false
}

// fakeInitDirMaker is the interface-DI test double for initDeps (per
// docs/TEST_SEAMS.md). A nil func field delegates to the real os
// implementation, so a test overrides only the operation it wants to
// fault-inject.
type fakeInitDirMaker struct {
	mkdirAll func(string, os.FileMode) error
}

func (f fakeInitDirMaker) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAll != nil {
		return f.mkdirAll(path, perm)
	}
	return os.MkdirAll(path, perm)
}

// TestFakeInitDeps_NilDelegatesToReal pins the nil-delegates-to-real
// contract documented on fakeInitDirMaker so a test that omits mkdirAll
// genuinely creates the dir (rather than silently succeeding without
// touching the filesystem). Without this, a future change to the fake's
// default branch could regress every happy-path-but-not-overridden test
// without any of them failing.
func TestFakeInitDeps_NilDelegatesToReal(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "delegate", "nested")
	if err := (fakeInitDirMaker{}).MkdirAll(target, 0o755); err != nil {
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

// TestLinkCursorGlobalHooks_SeededInstallCreatesHardlink covers
// linkCursorGlobalHooks's happy path — Cursor is detected as installed,
// the canonical hooks/global/cursor.json source exists, and the
// hardlink to ~/.cursor/hooks.json is created. Without this, the helper
// only ever sees its early-return-not-installed branch and stays at
// ~30% coverage on the new code.
func TestLinkCursorGlobalHooks_SeededInstallCreatesHardlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("seed helper skips on Windows")
	}
	tmp := seedAllPlatformInstallSignalsLifecycle(t)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "hooks", "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	cursorSrc := filepath.Join(agentsHome, "hooks", "global", "cursor.json")
	if err := os.WriteFile(cursorSrc, []byte(`{"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	initYes = true

	if err := linkCursorGlobalHooks(agentsHome, stdInitDirMaker{}); err != nil {
		t.Fatalf("linkCursorGlobalHooks: %v", err)
	}
	cursorDst := filepath.Join(tmp, ".cursor", "hooks.json")
	if _, err := os.Lstat(cursorDst); err != nil {
		t.Fatalf("expected ~/.cursor/hooks.json to be created: %v", err)
	}
	// Second invocation hits the "exists, no --force" skip branch.
	if err := linkCursorGlobalHooks(agentsHome, stdInitDirMaker{}); err != nil {
		t.Fatalf("idempotent re-call: %v", err)
	}
}

// TestNewInitCmd_RunEClosureWiresStdDeps covers the RunE closure body
// itself — without this, Cobra-driven invocation goes through code no
// test exercises directly, and the closure could regress (e.g. forget
// to thread args) without any other test failing. Drives a dry-run so
// the call is mutation-free.
func TestNewInitCmd_RunEClosureWiresStdDeps(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	defer resetInitSeams()
	initDryRun = true
	initYes = true

	cmd := newInitCmdForTest()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE closure: %v", err)
	}
}

func TestNewInitCmd_Metadata(t *testing.T) {
	cmd := newInitCmdForTest()
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

	defer resetInitSeams()
	initDryRun = true
	initYes = true

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
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

	defer resetInitSeams()
	initYes = true

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
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

	defer resetInitSeams()
	initYes = true

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
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
	if loaded.Version != 2 {
		t.Errorf("expected config version 2, got %d", loaded.Version)
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

	defer resetInitSeams()
	initYes = true
	initForce = true

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
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
	if loaded.Version != 2 {
		t.Errorf("expected config version 2 after force, got %d", loaded.Version)
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

	// Make claude "installed" via the claude CLI on PATH.
	seedClaudeInstalledSignalLifecycle(t, tmp)
	// The ~/.claude dir here is the managed-settings target, not the install
	// signal: it holds the pre-existing unmanaged settings.json below.
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

	defer resetInitSeams()
	initYes = true
	initForce = true

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
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
	if err := scaffoldWorkflowAssets(agentsHome, stdInitDirMaker{}); err != nil {
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
	if err := scaffoldWorkflowAssets(agentsHome, stdInitDirMaker{}); err != nil {
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
	tmp := seedAllPlatformInstallSignalsLifecycle(t)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	initYes = true

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
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

	// claude installed via the CLI on PATH; init writes the global
	// ~/.claude/settings.json symlink into the dir below.
	seedClaudeInstalledSignalLifecycle(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	initYes = true

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
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
	seedClaudeInstalledSignalLifecycle(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(tmp, ".claude", "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	initYes = true
	initForce = true

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
		t.Fatalf("runInit --force (claude seeded): %v", err)
	}
}

// TestRunInit_SeededClaudeAndExistingSettingsSkipsWithoutForce covers the
// init.go:151-153 else-branch (settings exists, no --force, skip with bullet).
func TestRunInit_SeededClaudeAndExistingSettingsSkipsWithoutForce(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignalLifecycle(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(tmp, ".claude", "settings.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	initYes = true

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
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
	seedClaudeInstalledSignalLifecycle(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	initYes = true

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
		t.Fatalf("runInit first pass: %v", err)
	}

	hooksSrc := filepath.Join(agentsHome, "hooks", "global", "claude-code.json")
	if err := os.MkdirAll(filepath.Dir(hooksSrc), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksSrc, []byte(`{"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	initForce = true

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
		t.Fatalf("runInit --force after hooks seed: %v", err)
	}
}

// TestRunInit_MkdirOnClaudeBranchSucceedsOnEmptyDir is a thin coverage test
// to ensure the seeded-claude init flow does not regress when ~/.claude is
// initially empty (no settings.json present yet — IsNotExist branch).
func TestRunInit_MkdirOnClaudeBranchSucceedsOnEmptyDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	seedClaudeInstalledSignalLifecycle(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	initYes = true

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
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

// ---------- bridge + seam coverage (t05 fixup) ----------

// TestRunInitForTest_BridgeForwardsToRunInit covers the
// RunInitForTest exported-bridge function the parent package's
// seams_test.go consumes until t11 relocates those tests. Driving a
// dry-run keeps the call mutation-free.
func TestRunInitForTest_BridgeForwardsToRunInit(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	defer resetInitSeams()
	initDryRun = true
	initYes = true

	if err := RunInitForTest(newInitCmdForTest(), nil, fakeInitDirMaker{}); err != nil {
		t.Fatalf("RunInitForTest: %v", err)
	}
}

// TestScaffoldWorkflowAssetsForTest_BridgeForwards covers the
// ScaffoldWorkflowAssetsForTest exported-bridge for the same reason as
// the runInit bridge — the parent seams_test.go reaches into it until
// t11. We exercise the auto-create branch (no hooks dir yet) to keep
// the assertion meaningful.
func TestScaffoldWorkflowAssetsForTest_BridgeForwards(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := ScaffoldWorkflowAssetsForTest(agentsHome, fakeInitDirMaker{}); err != nil {
		t.Fatalf("ScaffoldWorkflowAssetsForTest: %v", err)
	}
}

// TestSetInitFlags_OverridesAndPreservesPerArg covers SetInitFlags'
// three-arg conditional rebinding contract: non-nil getters replace
// the package-var Fns; nil leaves the existing Fn in place. Without
// this, the shim could silently fail to repoint one seam (e.g. forget
// to thread Force) and only blow up in production-only paths no test
// exercises.
func TestSetInitFlags_OverridesAndPreservesPerArg(t *testing.T) {
	defer resetInitSeams()
	// Snapshot + restore the package-var Fns so we don't leak across tests.
	origForce, origDryRun, origYes := InitForceFn, InitDryRunFn, InitYesFn
	defer func() {
		InitForceFn = origForce
		InitDryRunFn = origDryRun
		InitYesFn = origYes
	}()

	// Sentinel getters distinct from the defaults.
	const forceSent, dryRunSent, yesSent = true, true, true
	SetInitFlags(
		func() bool { return forceSent },
		func() bool { return dryRunSent },
		func() bool { return yesSent },
	)
	if !InitForceFn() || !InitDryRunFn() || !InitYesFn() {
		t.Fatal("SetInitFlags non-nil branch did not repoint all three getters")
	}

	// Now pass nil for each arg — existing Fns must be preserved.
	SetInitFlags(nil, nil, nil)
	if !InitForceFn() || !InitDryRunFn() || !InitYesFn() {
		t.Error("SetInitFlags nil branch should preserve previously-set getters")
	}
}

// TestInitUsageErrorFn_DefaultHintFormatting covers the lifecycle-local
// default UsageErrorFn (used when the commands shim has not repointed
// it — e.g. lifecycle-only unit tests). The two-branch contract: zero
// hints renders a bare message; >=1 hint renders the msg + indented
// hint lines. Without this, the default could regress to dropping hints
// and only production code (going through commands.UsageError) would
// notice.
func TestInitUsageErrorFn_DefaultHintFormatting(t *testing.T) {
	// IMPORTANT: call InitUsageErrorFn directly without reassigning it.
	// Other tests in this file overwrite it via SetInitFlags-style
	// patterns, but here we want to exercise the package default's two
	// branches (zero hints + >=1 hints) for coverage. We rely on test
	// ordering being incidental: the only way a previous test could
	// leak a non-default Fn here is via init's runInit calling into
	// commands shim — which lifecycle tests never do (lifecycle is
	// the parent here, not the child). To be safe we snapshot and
	// require that the current value still matches the package default
	// signature (zero hints returns the bare msg).
	if err := InitUsageErrorFn("bad"); err == nil || err.Error() != "bad" {
		t.Errorf("zero-hint default: want 'bad', got %v", err)
	}
	got := InitUsageErrorFn("bad", "do x", "or y")
	if got == nil {
		t.Fatal("multi-hint default returned nil")
	}
	want := "bad\n  do x\n  or y"
	if got.Error() != want {
		t.Errorf("multi-hint default: want %q, got %q", want, got.Error())
	}
}

// TestInitNoArgs_DefaultUsageErrorFormatting drives the
// initNoArgs cobra positional validator's reject branch through the
// lifecycle-default InitUsageErrorFn. The hints slice gets the
// usage/help suggestions injected by initNoArgs itself, exercising the
// >=1 hint path of the default formatter. Without this the default
// reject branch is unreachable from a lifecycle-only test (parent
// shim repoints UsageErrorFn before invoking init in production).
func TestInitNoArgs_DefaultUsageErrorFormatting(t *testing.T) {
	cmd := newInitCmdForTest()
	err := InitNoArgs("custom hint")(cmd, []string{"extra"})
	if err == nil {
		t.Fatal("expected error rejecting positional arg")
	}
	if !strings.Contains(err.Error(), "does not accept positional arguments") {
		t.Errorf("missing core message: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "custom hint") {
		t.Errorf("missing custom hint: %q", err.Error())
	}
}

// TestRunInit_ConfirmDeclinedIsNoop drives the runInit Yes=false
// branch where ui.Confirm returns false (EOF on closed stdin). Without
// this the cancellation branch (and its early return) is unreachable
// from a Go test — runInit's other paths all set initYes=true.
func TestRunInit_ConfirmDeclinedIsNoop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	// initYes left false on purpose so ui.Confirm is invoked.

	// Replace stdin with /dev/null so ReadString returns EOF and
	// Confirm returns false (the cancellation path).
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	origStdin := os.Stdin
	os.Stdin = devNull
	defer func() { os.Stdin = origStdin }()

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
		t.Fatalf("runInit (confirm declined): %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "config.json")); err == nil {
		t.Error("confirm-declined should not have created config.json")
	}
}

// TestCreateInitialAgentsDirs_MkdirErrorIsWrapped covers the
// createInitialAgentsDirs error-return branch via fault-injection on
// the first MkdirAll call. The wrapped "creating %s" format is part
// of the user-visible UX, so we assert on it.
func TestCreateInitialAgentsDirs_MkdirErrorIsWrapped(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	boom := fmt.Errorf("synthetic mkdir failure")
	fake := fakeInitDirMaker{mkdirAll: func(string, os.FileMode) error { return boom }}

	err := createInitialAgentsDirs(agentsHome, fake)
	if err == nil {
		t.Fatal("expected MkdirAll error to surface")
	}
	if !strings.Contains(err.Error(), "creating ") {
		t.Errorf("expected 'creating' prefix: %v", err)
	}
}

// TestScaffoldWorkflowAssets_MkdirErrorPropagates covers the
// scaffoldWorkflowAssets first-branch error return when AgentsContextDir
// cannot be created. Without this, a regression that swallows the
// MkdirAll error here would only surface when CopyMissingGlobalBundles
// runs against a partially scaffolded tree.
func TestScaffoldWorkflowAssets_MkdirErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	boom := fmt.Errorf("synthetic ctx mkdir failure")
	fake := fakeInitDirMaker{mkdirAll: func(string, os.FileMode) error { return boom }}

	err := scaffoldWorkflowAssets(agentsHome, fake)
	if err == nil || !strings.Contains(err.Error(), "synthetic ctx") {
		t.Errorf("expected ctx mkdir error to propagate, got %v", err)
	}
}

// TestRunInit_CreateInitialAgentsDirsErrorPropagates exercises the
// runInit-level wrapping of a createInitialAgentsDirs failure. Each
// of the four downstream error returns (scaffoldStarterHomeAssets,
// scaffoldWorkflowAssets, InitEnsureGlobalKGMCPConfigsFn,
// linkClaudeGlobalSettings, linkCursorGlobalHooks) is exercised
// individually via the dedicated tests below, so the failure modes
// are bracketed rather than co-mingled.
func TestRunInit_CreateInitialAgentsDirsErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	initYes = true

	boom := fmt.Errorf("boom")
	fake := fakeInitDirMaker{mkdirAll: func(string, os.FileMode) error { return boom }}

	if err := runInit(newInitCmdForTest(), nil, fake); err == nil {
		t.Fatal("expected createInitialAgentsDirs failure to bubble through runInit")
	}
}

// TestRunInit_ScaffoldWorkflowAssetsErrorPropagates exercises the
// runInit "scaffolding starter hook bundles: %w" wrap. Uses a fake
// initDirMaker that only fails on the AgentsContextDir path so all the
// earlier createInitialAgentsDirs calls succeed and the failure is
// bracketed to scaffoldWorkflowAssets's first MkdirAll.
func TestRunInit_ScaffoldWorkflowAssetsErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	initYes = true

	saved := InitEnsureGlobalKGMCPConfigsFn
	defer func() { InitEnsureGlobalKGMCPConfigsFn = saved }()
	InitEnsureGlobalKGMCPConfigsFn = func(string) error { return nil }

	ctxDir := config.AgentsContextDir()
	// First pass: real mkdir for createInitialAgentsDirs paths. After
	// the agentsHome tree exists we will see a second MkdirAll for
	// ctxDir (scaffoldWorkflowAssets) — fail that one only.
	seenCtx := false
	fake := fakeInitDirMaker{mkdirAll: func(path string, perm os.FileMode) error {
		if path == ctxDir {
			if seenCtx {
				return fmt.Errorf("synthetic ctx mkdir failure")
			}
			seenCtx = true
			// First sighting is during createInitialAgentsDirs — let it
			// succeed (otherwise we fail too early and exercise
			// createInitialAgentsDirs's wrap, not scaffoldWorkflowAssets's).
			return os.MkdirAll(path, perm)
		}
		return os.MkdirAll(path, perm)
	}}

	err := runInit(newInitCmdForTest(), nil, fake)
	if err == nil || !strings.Contains(err.Error(), "scaffolding starter hook bundles") {
		t.Errorf("expected scaffoldWorkflowAssets wrap, got %v", err)
	}
}

// Note: runInit's "scaffolding starter home assets: %w" wrap (init.go
// lines 214-216) is intentionally not tested here. The downstream
// scaffoldhome.CopyMissingStarterAssets skips any pre-existing dst
// path (file OR directory), so a directory-collision fault is silently
// absorbed. Fault-injecting the embed FS read would require either
// changing production code to take an FS interface or chmod tricks
// that only work on POSIX as root. Coverage cost: 2 statements of 134.

// TestRunInit_KGMCPScaffolderErrorPropagates exercises runInit's
// "scaffolding starter KG MCP configs: %w" wrap. We point the
// in-package seam at a synthetic failure so the assertion is hermetic
// (no reliance on the production KG MCP scaffolder shape).
func TestRunInit_KGMCPScaffolderErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := InitEnsureGlobalKGMCPConfigsFn
	defer func() { InitEnsureGlobalKGMCPConfigsFn = saved }()
	InitEnsureGlobalKGMCPConfigsFn = func(string) error {
		return fmt.Errorf("synthetic kgmcp failure")
	}

	defer resetInitSeams()
	initYes = true

	err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{})
	if err == nil || !strings.Contains(err.Error(), "scaffolding starter KG MCP configs") {
		t.Errorf("expected KG MCP wrap, got %v", err)
	}
}

// TestSeedInitialConfig_ExistingConfigNoForceIsNoop covers the early
// return at the head of seedInitialConfig (existing config.json AND
// not Force). Without this branch a regression that re-writes config
// even when --force was not passed would clobber projects on every run.
func TestSeedInitialConfig_ExistingConfigNoForceIsNoop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	preserved := []byte(`{"version":1,"sentinel":"keep"}`)
	cfgPath := filepath.Join(agentsHome, "config.json")
	if err := os.WriteFile(cfgPath, preserved, 0o644); err != nil {
		t.Fatal(err)
	}

	defer resetInitSeams()
	// initForce = false (default), so seedInitialConfig must skip.

	if err := seedInitialConfig(agentsHome); err != nil {
		t.Fatalf("seedInitialConfig: %v", err)
	}
	got, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(preserved) {
		t.Errorf("seedInitialConfig should not have rewritten config.json; got %q", string(got))
	}
}

// A real Stat error on config.json (permission denied on the parent
// directory) is not the same as "config.json doesn't exist yet" — without
// --force, seedInitialConfig must surface it instead of silently returning
// nil (which would leave `da init` reporting success without ever writing
// the foundational config.json).
func TestSeedInitialConfig_StatPermissionError_NoForce_ReturnsError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission-denial semantics")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, agentsHome)

	defer resetInitSeams()
	// initForce = false (default).

	err := seedInitialConfig(agentsHome)
	if err == nil {
		t.Fatal("expected a non-nil error for a real Stat failure on config.json, got nil (silent false success)")
	}
	if !strings.Contains(err.Error(), "checking for existing config.json") {
		t.Errorf("expected the Stat-error wrap, got: %v", err)
	}
}

// --force must still bypass the early-return guard and attempt to write
// config.json even when the pre-flight Stat failed with a real error — the
// fix must not change this case-6 behavior (see seedInitialConfig's switch).
// The write itself then fails for its OWN reason (unwritable directory),
// proving the new Stat-error branch did not short-circuit the force path.
func TestSeedInitialConfig_StatPermissionError_WithForce_StillAttemptsSave(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission-denial semantics")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.MakeDirUnreadable(t, agentsHome)

	defer resetInitSeams()
	initForce = true

	err := seedInitialConfig(agentsHome)
	if err == nil {
		t.Fatal("expected an error (unwritable config dir), got nil")
	}
	if strings.Contains(err.Error(), "checking for existing config.json") {
		t.Errorf("--force must skip the pre-flight Stat-error short-circuit, got: %v", err)
	}
}

// A state-dir MkdirAll failure during runInit must be warned about, not
// silently reported as "ok" — and must not fail runInit overall (the state
// dir is best-effort).
func TestRunInit_StateDirMkdirAllError_WarnsButSucceeds(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	initYes = true

	stateDir := config.AgentsStateDir()
	fake := fakeInitDirMaker{mkdirAll: func(path string, perm os.FileMode) error {
		if path == stateDir {
			return fmt.Errorf("mkdir denied")
		}
		return os.MkdirAll(path, perm)
	}}

	out := captureDoctorOutput(t, func() {
		if err := runInit(newInitCmdForTest(), nil, fake); err != nil {
			t.Fatalf("runInit should not fail on a best-effort state-dir MkdirAll error, got: %v", err)
		}
	})
	if !strings.Contains(out, "could not create state directory") {
		t.Errorf("expected a warning about the failed state directory creation, got: %q", out)
	}
	if strings.Contains(out, "Created state directory") {
		t.Errorf("must not print a false 'ok' bullet when state-dir creation failed, got: %q", out)
	}
}

// TestRecordPlatformState_NotInstalledBranch exercises the
// not-installed leg of recordPlatformState (the bullet-none render
// path). The all-platforms-seeded test only exercises the installed
// leg; together they cover both.
func TestRecordPlatformState_NotInstalledBranch(t *testing.T) {
	// Use a temp HOME with no platform signals so IsInstalled returns
	// false for the platforms init detects. ByID("claude") is the
	// cheapest probe. PATH must point at an actually-empty dir, not
	// just a non-existent one, so exec.LookPath conclusively fails.
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	emptyBin := filepath.Join(tmp, "no-bins")
	if err := os.MkdirAll(emptyBin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", emptyBin)

	p := platform.ByID("claude")
	if p == nil {
		t.Skip("claude platform not registered in this build")
	}
	if p.IsInstalled() {
		t.Skip("claude unexpectedly installed in this sandbox")
	}

	cfg := &config.Config{
		Version:  1,
		Projects: make(map[string]config.Project),
		Agents:   make(map[string]config.Agent),
	}
	recordPlatformState(cfg, p)

	if cfg.IsPlatformEnabled(p.ID()) {
		t.Errorf("expected %s to be recorded as not-enabled", p.ID())
	}
}

// TestLinkClaudeGlobalSettings_NotInstalledIsNoop exercises the early
// return when claude is not installed (no ~/.claude probe directory
// AND no claude binary on PATH). Without this, the early-return guard
// is only exercised transitively through runInit happy-path tests
// where claude IS installed.
func TestLinkClaudeGlobalSettings_NotInstalledIsNoop(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp) // no ~/.claude created
	emptyBin := filepath.Join(tmp, "no-bins")
	if err := os.MkdirAll(emptyBin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", emptyBin) // scrub real claude binary from probe
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	if err := linkClaudeGlobalSettings(agentsHome, stdInitDirMaker{}); err != nil {
		t.Errorf("not-installed branch should be a no-op, got %v", err)
	}
}

// TestLinkCursorGlobalHooks_NotInstalledIsNoop is the cursor analogue:
// without ~/.cursor (and without seeding any cursor signal) the helper
// returns nil early. PATH is scrubbed so a real cursor binary on the
// developer's machine cannot satisfy the probe.
//
// Skipped on darwin because the cursor platform's IsInstalled() probes
// /Applications/Cursor.app first — a developer-installed Cursor on
// macOS makes this branch unreachable locally. Linux + Windows runners
// in CI cover it, and the merged multi-OS coverage profile reflects
// that.
func TestLinkCursorGlobalHooks_NotInstalledIsNoop(t *testing.T) {
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat("/Applications/Cursor.app"); err == nil {
			t.Skip("Cursor.app installed locally — IsInstalled probe unconditional on darwin")
		}
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	emptyBin := filepath.Join(tmp, "no-bins")
	if err := os.MkdirAll(emptyBin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", emptyBin)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	if err := linkCursorGlobalHooks(agentsHome, stdInitDirMaker{}); err != nil {
		t.Errorf("not-installed branch should be a no-op, got %v", err)
	}
}

// TestLinkClaudeGlobalSettings_SymlinkReplacingErrorPropagates
// exercises the "linking %s" wrap when links.SymlinkReplacing returns
// an error. Triggered by making the ~/.claude target directory
// read-only AFTER the best-effort MkdirAll so SymlinkReplacing's own
// write fails. Skipped on Windows (chmod 0500 does not stop symlink
// creation there) and as root (bypasses perm bits).
func TestLinkClaudeGlobalSettings_SymlinkReplacingErrorPropagates(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod-based fault injection not available")
	}
	tmp := seedAllPlatformInstallSignalsLifecycle(t)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "settings", "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	settingsSrc := filepath.Join(agentsHome, "settings", "global", "claude-code.json")
	if err := os.WriteFile(settingsSrc, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	claudeDir := filepath.Join(tmp, ".claude")
	// claudeDir already exists from the seed helper. Make it read-only
	// so SymlinkReplacing's create-temp-and-rename fails.
	t.Cleanup(func() { _ = os.Chmod(claudeDir, 0o755) })
	if err := os.Chmod(claudeDir, 0o500); err != nil {
		t.Fatal(err)
	}

	defer resetInitSeams()
	err := linkClaudeGlobalSettings(agentsHome, stdInitDirMaker{})
	if err == nil {
		t.Fatal("expected SymlinkReplacing failure to surface")
	}
	if !strings.Contains(err.Error(), "linking ") {
		t.Errorf("expected 'linking' wrap: %v", err)
	}
}

// TestLinkCursorGlobalHooks_HardlinkReplacingErrorPropagates is the
// cursor analogue of the SymlinkReplacing error test. Same Windows /
// root skip rules.
func TestLinkCursorGlobalHooks_HardlinkReplacingErrorPropagates(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod-based fault injection not available")
	}
	tmp := seedAllPlatformInstallSignalsLifecycle(t)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "hooks", "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	cursorSrc := filepath.Join(agentsHome, "hooks", "global", "cursor.json")
	if err := os.WriteFile(cursorSrc, []byte(`{"hooks":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	cursorDir := filepath.Join(tmp, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(cursorDir, 0o755) })
	if err := os.Chmod(cursorDir, 0o500); err != nil {
		t.Fatal(err)
	}

	defer resetInitSeams()
	err := linkCursorGlobalHooks(agentsHome, stdInitDirMaker{})
	if err == nil {
		t.Fatal("expected HardlinkReplacing failure to surface")
	}
	if !strings.Contains(err.Error(), "linking ") {
		t.Errorf("expected 'linking' wrap: %v", err)
	}
}

// TestRunInit_LinkClaudeErrorPropagates exercises the runInit-level
// return when linkClaudeGlobalSettings fails. Drives the same chmod
// fault as the direct helper test, but through the runInit pipeline so
// the early-success steps (createInitialAgentsDirs / seedInitialConfig
// / scaffoldStarterHomeAssets / scaffoldWorkflowAssets / KGMCP) all
// run normally first and the failure is bracketed to the claude link.
func TestRunInit_LinkClaudeErrorPropagates(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod-based fault injection not available")
	}
	tmp := seedAllPlatformInstallSignalsLifecycle(t)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	claudeDir := filepath.Join(tmp, ".claude")
	t.Cleanup(func() { _ = os.Chmod(claudeDir, 0o755) })

	saved := InitEnsureGlobalKGMCPConfigsFn
	defer func() { InitEnsureGlobalKGMCPConfigsFn = saved }()
	// Bypass KGMCP scaffold to keep the test bracketed to claude link.
	InitEnsureGlobalKGMCPConfigsFn = func(string) error { return nil }

	defer resetInitSeams()
	initYes = true

	// Lock claude dir AFTER the seed creates it but BEFORE init runs.
	if err := os.Chmod(claudeDir, 0o500); err != nil {
		t.Fatal(err)
	}

	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err == nil {
		t.Fatal("expected linkClaudeGlobalSettings error to bubble out")
	}
}

// TestRunInit_LinkCursorErrorPropagates is the cursor analogue. We
// allow claude to succeed and lock the cursor dir to bracket the
// failure to linkCursorGlobalHooks.
func TestRunInit_LinkCursorErrorPropagates(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("chmod-based fault injection not available")
	}
	tmp := seedAllPlatformInstallSignalsLifecycle(t)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := InitEnsureGlobalKGMCPConfigsFn
	defer func() { InitEnsureGlobalKGMCPConfigsFn = saved }()
	InitEnsureGlobalKGMCPConfigsFn = func(string) error { return nil }

	defer resetInitSeams()
	initYes = true

	// Run init once to let createInitialAgentsDirs scaffold the cursor
	// src; the first run also creates the cursor dst hardlink. Then
	// remove the dst, lock the cursor dir, and re-init with --force so
	// linkCursorGlobalHooks attempts a fresh HardlinkReplacing under
	// the read-only dir.
	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err != nil {
		t.Fatalf("first init: %v", err)
	}
	// Ensure cursor src exists (init may not seed cursor.json by default
	// — write one if absent so the helper proceeds past the missing-src
	// early return).
	cursorSrc := filepath.Join(agentsHome, "hooks", "global", "cursor.json")
	if _, err := os.Stat(cursorSrc); os.IsNotExist(err) {
		if err := os.WriteFile(cursorSrc, []byte(`{"hooks":{}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cursorDir := filepath.Join(tmp, ".cursor")
	if err := os.MkdirAll(cursorDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Remove any existing dst from the first run so the link attempt is
	// fresh, then lock the dir.
	_ = os.Remove(filepath.Join(cursorDir, "hooks.json"))
	t.Cleanup(func() { _ = os.Chmod(cursorDir, 0o755) })
	if err := os.Chmod(cursorDir, 0o500); err != nil {
		t.Fatal(err)
	}

	initForce = true
	if err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{}); err == nil {
		t.Fatal("expected linkCursorGlobalHooks error to bubble out")
	}
}

// TestSeedInitialConfig_SaveErrorPropagates exercises the cfg.Save
// failure wrap by pre-creating a directory at the config.json path so
// the atomic file write fails. Works on every OS — the chmod approach
// would skip Windows where dir-readonly does not block child writes.
func TestSeedInitialConfig_SaveErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(agentsHome, "config.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	defer resetInitSeams()
	initForce = true // force the write past the os.Stat early return

	err := seedInitialConfig(agentsHome)
	if err == nil || !strings.Contains(err.Error(), "saving config") {
		t.Errorf("expected 'saving config' wrap, got %v", err)
	}
}

// TestRunInit_SeedConfigErrorPropagates exercises the runInit-level
// "seedInitialConfig returned err" branch (line 210-212). Pre-creates
// a DIRECTORY at the config.json path so cfg.Save's WriteFile fails
// trying to write a file over a directory — works on all OSes and
// avoids the brittle "lock after N mkdirs" pattern.
func TestRunInit_SeedConfigErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-create config.json as a directory.
	if err := os.MkdirAll(filepath.Join(agentsHome, "config.json"), 0o755); err != nil {
		t.Fatal(err)
	}

	defer resetInitSeams()
	initYes = true
	initForce = true // bypass the "existing home, no force" early return

	err := runInit(newInitCmdForTest(), nil, stdInitDirMaker{})
	if err == nil {
		t.Fatal("expected seedInitialConfig save failure to bubble out")
	}
}

// TestLinkCursorGlobalHooks_MissingSrcIsNoop covers the
// "Cursor IS installed but cursor.json src doesn't exist" early-return
// (the os.Stat err != nil branch). The seeded-install happy path
// always creates the src file, leaving this branch uncovered.
func TestLinkCursorGlobalHooks_MissingSrcIsNoop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("seed helper skips on Windows")
	}
	tmp := seedAllPlatformInstallSignalsLifecycle(t)
	agentsHome := filepath.Join(tmp, ".agents")
	// Do NOT create hooks/global/cursor.json — exercise the Stat err branch.
	t.Setenv("AGENTS_HOME", agentsHome)

	defer resetInitSeams()
	if err := linkCursorGlobalHooks(agentsHome, stdInitDirMaker{}); err != nil {
		t.Errorf("missing cursor.json src should be a no-op, got %v", err)
	}
	cursorDst := filepath.Join(tmp, ".cursor", "hooks.json")
	if _, err := os.Lstat(cursorDst); err == nil {
		t.Error("no hardlink should have been created without a cursor.json src")
	}
}

// ---------- NewInitCmd (t13a) ----------
//
// NewInitCmd is the t13a-introduced constructor that absorbs what the
// parent commands/init.go shim used to do — building the cobra literal
// and wiring SetInitFlags + InitUsageErrorFn from Deps. The tests below
// pin its contract so t13b's root.go rewire can rely on the documented
// shape.

// testInitDeps returns a lifecycle.Deps suitable for NewInitCmd tests:
// Force/DryRun/Yes flow through deps.Flags so wireInitSeamsFromDeps
// closures route them to InitForceFn / InitDryRunFn / InitYesFn. A
// sentinel UsageError lets the rejection test confirm the deps-supplied
// formatter (not the in-package default) actually fired.
func testInitDeps(flags GlobalFlags) Deps {
	return Deps{
		Flags:        flags,
		UsageError:   func(msg string, hints ...string) error { return fmt.Errorf("usage-err: %s", msg) },
		ExampleBlock: func(lines ...string) string { return strings.Join(lines, "\n") },
	}
}

// TestNewInitCmd_BuildsCobraWithCorrectMetadata pins the constructor's
// cobra surface so t13b's root.go rewire can rely on Use/Short/Long/
// Example all coming straight from the InitCmd* constants. Without this,
// a constructor regression that drops a field would fail only in CLI
// integration tests.
func TestNewInitCmd_BuildsCobraWithCorrectMetadata(t *testing.T) {
	defer resetInitSeams()
	cmd := NewInitCmd(testInitDeps(GlobalFlags{}))
	if cmd == nil {
		t.Fatal("NewInitCmd returned nil")
	}
	if cmd.Use != InitCmdUse {
		t.Errorf("Use = %q, want %q", cmd.Use, InitCmdUse)
	}
	if cmd.Short != InitCmdShort {
		t.Errorf("Short = %q, want %q", cmd.Short, InitCmdShort)
	}
	if cmd.Long != InitCmdLong {
		t.Errorf("Long mismatch")
	}
	if cmd.Example != InitCmdExample {
		t.Errorf("Example mismatch")
	}
	if cmd.RunE == nil {
		t.Error("RunE should be wired")
	}
	if cmd.Args == nil {
		t.Error("Args validator should be wired")
	}
}

// TestNewInitCmd_WiresSeamsFromDepsFlags pins the construction-time
// wireInitSeamsFromDeps call: every InitForceFn / InitDryRunFn /
// InitYesFn must observe the deps.Flags value at call time. Without
// this, a regression that drops one of the SetInitFlags closures would
// silently revert that flag to the package default (false), and only
// production runs with that specific flag set would notice.
func TestNewInitCmd_WiresSeamsFromDepsFlags(t *testing.T) {
	defer resetInitSeams()
	// Snapshot the package-var Fns so the wireInitSeamsFromDeps overwrite
	// doesn't leak across tests.
	origForce, origDryRun, origYes := InitForceFn, InitDryRunFn, InitYesFn
	origUsage := InitUsageErrorFn
	defer func() {
		InitForceFn = origForce
		InitDryRunFn = origDryRun
		InitYesFn = origYes
		InitUsageErrorFn = origUsage
	}()

	deps := testInitDeps(GlobalFlags{Force: true, DryRun: true, Yes: true})
	_ = NewInitCmd(deps)

	if !InitForceFn() {
		t.Error("NewInitCmd did not wire InitForceFn from deps.Flags.Force")
	}
	if !InitDryRunFn() {
		t.Error("NewInitCmd did not wire InitDryRunFn from deps.Flags.DryRun")
	}
	if !InitYesFn() {
		t.Error("NewInitCmd did not wire InitYesFn from deps.Flags.Yes")
	}
	// Sentinel UsageError must have replaced the default.
	err := InitUsageErrorFn("rejected", "hint A")
	if err == nil || !strings.Contains(err.Error(), "usage-err: rejected") {
		t.Errorf("InitUsageErrorFn not routed through deps.UsageError; got %v", err)
	}
}

// TestNewInitCmd_FlagsFnTakesPrecedenceOverDepsFlags pins the FlagsFn
// contract: when both deps.FlagsFn and deps.Flags are set, the closure
// wins. This matters because cobra mutates upstream flag state AFTER the
// constructor returns; a static Deps.Flags snapshot taken at construction
// time would be stale. T13b's worker passes a closure over commands.Flags
// to get live reads on each invocation.
func TestNewInitCmd_FlagsFnTakesPrecedenceOverDepsFlags(t *testing.T) {
	defer resetInitSeams()
	origForce, origDryRun, origYes := InitForceFn, InitDryRunFn, InitYesFn
	defer func() {
		InitForceFn = origForce
		InitDryRunFn = origDryRun
		InitYesFn = origYes
	}()

	live := GlobalFlags{Force: true}
	deps := Deps{
		Flags:        GlobalFlags{Force: false, DryRun: true, Yes: true}, // snapshot — must be ignored
		FlagsFn:      func() GlobalFlags { return live },
		ExampleBlock: func(lines ...string) string { return strings.Join(lines, "\n") },
	}
	_ = NewInitCmd(deps)

	if !InitForceFn() {
		t.Error("FlagsFn().Force should win over deps.Flags.Force")
	}
	if InitDryRunFn() {
		t.Error("FlagsFn().DryRun (false) should win over deps.Flags.DryRun (true)")
	}
	if InitYesFn() {
		t.Error("FlagsFn().Yes (false) should win over deps.Flags.Yes (true)")
	}

	// Mutate the live snapshot and re-read through the wired closure —
	// the change must be observed (otherwise FlagsFn was captured-by-value
	// somewhere and t13b's live-read contract breaks).
	live = GlobalFlags{Force: false, DryRun: true, Yes: true}
	if InitForceFn() {
		t.Error("post-mutation Force should now be false via live FlagsFn")
	}
	if !InitDryRunFn() {
		t.Error("post-mutation DryRun should now be true via live FlagsFn")
	}
	if !InitYesFn() {
		t.Error("post-mutation Yes should now be true via live FlagsFn")
	}
}

// TestNewInitCmd_PreservesDefaultUsageErrorWhenDepsOmits covers the
// nil-UsageError branch in wireInitSeamsFromDeps. Lifecycle-only unit
// tests construct Deps without a hint formatter (testStatusDeps /
// testDoctorDeps both supply one; testInitDeps could be called with the
// field zeroed for a regression). The default InitUsageErrorFn must
// survive so the in-package formatter still produces a readable error.
func TestNewInitCmd_PreservesDefaultUsageErrorWhenDepsOmits(t *testing.T) {
	defer resetInitSeams()
	origUsage := InitUsageErrorFn
	defer func() { InitUsageErrorFn = origUsage }()

	// Force the package var to a sentinel so we can detect whether
	// NewInitCmd overwrote it.
	sentinel := func(msg string, hints ...string) error {
		return fmt.Errorf("sentinel: %s", msg)
	}
	InitUsageErrorFn = sentinel

	deps := Deps{
		Flags:        GlobalFlags{},
		UsageError:   nil, // explicit: caller omits the formatter
		ExampleBlock: func(lines ...string) string { return strings.Join(lines, "\n") },
	}
	_ = NewInitCmd(deps)

	err := InitUsageErrorFn("x")
	if err == nil || !strings.Contains(err.Error(), "sentinel: x") {
		t.Errorf("nil deps.UsageError should preserve existing InitUsageErrorFn; got %v", err)
	}
}

// TestNewInitCmd_RunEAppliesGlobalsAndRunsRunInit drives the RunE
// closure end to end: applyDepsToGlobals must run (pin Version copy),
// the runtime seams must point at deps, and the moved runInit body must
// fire successfully under --dry-run. Without this, a regression that
// drops the wrapper's applyDepsToGlobals call would leave deps.Version
// unobserved in production (init's WriteRefreshToAgentsRC is install-
// only so init wouldn't notice; we pin it here to guard the contract
// the install/doctor constructors also depend on).
func TestNewInitCmd_RunEAppliesGlobalsAndRunsRunInit(t *testing.T) {
	defer resetInitSeams()

	// Snapshot package vars so the RunE write doesn't leak.
	origVersion, origCommit, origDescribe := Version, Commit, Describe
	defer func() {
		Version = origVersion
		Commit = origCommit
		Describe = origDescribe
	}()
	origForce, origDryRun, origYes := InitForceFn, InitDryRunFn, InitYesFn
	origUsage := InitUsageErrorFn
	defer func() {
		InitForceFn = origForce
		InitDryRunFn = origDryRun
		InitYesFn = origYes
		InitUsageErrorFn = origUsage
	}()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", filepath.Join(tmp, ".agents"))

	deps := Deps{
		Flags:        GlobalFlags{DryRun: true, Yes: true},
		UsageError:   func(msg string, hints ...string) error { return fmt.Errorf("u:%s", msg) },
		ExampleBlock: func(lines ...string) string { return strings.Join(lines, "\n") },
		Version:      "1.2.3-test",
		Commit:       "deadbeef",
		Describe:     "v1.2.3-test-0-gdeadbeef",
	}

	cmd := NewInitCmd(deps)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if Version != "1.2.3-test" {
		t.Errorf("applyDepsToGlobals did not copy Version; got %q", Version)
	}
	if Commit != "deadbeef" {
		t.Errorf("applyDepsToGlobals did not copy Commit; got %q", Commit)
	}
	if Describe != "v1.2.3-test-0-gdeadbeef" {
		t.Errorf("applyDepsToGlobals did not copy Describe; got %q", Describe)
	}
	// --dry-run + --yes path takes the early "DRY RUN - no changes made"
	// return without creating ~/.agents — confirms the dry-run flag
	// observed by InitDryRunFn() (which reads through wireInitSeamsFromDeps).
	if _, err := os.Stat(filepath.Join(tmp, ".agents")); !os.IsNotExist(err) {
		t.Errorf("dry-run init should not have created ~/.agents")
	}
}

// TestNewInitCmd_RejectsPositionalArgs is the negative-path check that
// the cobra Args validator (sourced from InitNoArgs(InitCmdNoArgsHint))
// rejects extra positional input. Mirrors the parent shim's
// TestNewInitCmd_ShimRejectsPositionalArgs but at the lifecycle-package
// constructor level so a t13b regression that drops the Args wiring is
// caught here.
func TestNewInitCmd_RejectsPositionalArgs(t *testing.T) {
	defer resetInitSeams()
	cmd := NewInitCmd(testInitDeps(GlobalFlags{}))
	if err := cmd.Args(cmd, nil); err != nil {
		t.Errorf("zero args should be accepted, got %v", err)
	}
	if err := cmd.Args(cmd, []string{"unexpected"}); err == nil {
		t.Error("positional arg should be rejected")
	}
}

// ---------- applyDepsToGlobals (t13a) ----------
//
// applyDepsToGlobals is the helper NewInstallCmd / NewDoctorCmd /
// NewInitCmd's RunE wrapper calls before delegating to the moved RunE
// body. Its precedence + no-op-safe behavior is the contract t13b's
// worker (and parent shims today) rely on.

// TestApplyDepsToGlobals_FlagsFnPrecedence pins the FlagsFn-wins-over-
// Flags rule. Without this, a regression that flips the precedence
// would silently break t13b's live-read pattern (the static Deps.Flags
// snapshot taken at root composition time would shadow the live closure).
func TestApplyDepsToGlobals_FlagsFnPrecedence(t *testing.T) {
	saved := Flags
	defer func() { Flags = saved }()

	live := GlobalFlags{Force: true, Yes: true}
	deps := Deps{
		Flags:   GlobalFlags{DryRun: true},
		FlagsFn: func() GlobalFlags { return live },
	}
	applyDepsToGlobals(deps)

	if Flags.Force != true || Flags.Yes != true {
		t.Errorf("FlagsFn values should populate Flags; got %+v", Flags)
	}
	if Flags.DryRun != false {
		t.Error("Deps.Flags.DryRun should be ignored when FlagsFn is set")
	}
}

// TestApplyDepsToGlobals_FallsBackToDepsFlagsWhenFlagsFnNil pins the
// other half of the precedence rule: when FlagsFn is nil, Deps.Flags
// is used. This is the path lifecycle's existing tests already exercise
// implicitly via the parent shim's syncLifecycleGlobals; the test makes
// the contract explicit at the helper level so a refactor that drops
// the fallback branch fails here.
func TestApplyDepsToGlobals_FallsBackToDepsFlagsWhenFlagsFnNil(t *testing.T) {
	saved := Flags
	defer func() { Flags = saved }()

	deps := Deps{
		Flags:   GlobalFlags{Force: true, DryRun: true, Verbose: true, Yes: true},
		FlagsFn: nil,
	}
	applyDepsToGlobals(deps)
	if Flags != deps.Flags {
		t.Errorf("expected Flags == deps.Flags, got %+v vs %+v", Flags, deps.Flags)
	}
}

// TestApplyDepsToGlobals_EmptyStringsPreservePackageDefaults pins the
// "" -> skip write contract for Version/Commit/Describe. Without this,
// a regression that always overwrites would clobber the compile-time
// build-info defaults ("dev"/"" per refresh.go) whenever a caller
// passed the zero-value Deps.
func TestApplyDepsToGlobals_EmptyStringsPreservePackageDefaults(t *testing.T) {
	savedV, savedC, savedD := Version, Commit, Describe
	defer func() {
		Version = savedV
		Commit = savedC
		Describe = savedD
	}()

	Version = "1.0.0"
	Commit = "abc123"
	Describe = "v1.0.0"

	applyDepsToGlobals(Deps{}) // every field zero
	if Version != "1.0.0" || Commit != "abc123" || Describe != "v1.0.0" {
		t.Errorf("empty Deps overwrote build-info vars; got V=%q C=%q D=%q",
			Version, Commit, Describe)
	}
}

// TestApplyDepsToGlobals_NonEmptyStringsOverwrite pins the positive
// side: non-empty Version/Commit/Describe propagate. Without this, a
// regression that swaps the empty-check polarity would silently
// ignore caller-supplied build info.
func TestApplyDepsToGlobals_NonEmptyStringsOverwrite(t *testing.T) {
	savedV, savedC, savedD := Version, Commit, Describe
	defer func() {
		Version = savedV
		Commit = savedC
		Describe = savedD
	}()

	deps := Deps{Version: "2.0.0", Commit: "feedface", Describe: "v2.0.0-1-gfeedface"}
	applyDepsToGlobals(deps)
	if Version != "2.0.0" || Commit != "feedface" || Describe != "v2.0.0-1-gfeedface" {
		t.Errorf("non-empty values should overwrite; got V=%q C=%q D=%q",
			Version, Commit, Describe)
	}
}

// TestApplyDepsToGlobals_ErrorWithHintsNilPreserves pins the
// ErrorWithHintsFn nil branch: when deps.ErrorWithHints is nil, the
// existing package-var Fn survives. Lifecycle-only unit tests rely on
// this — they construct Deps without an ErrorWithHints and expect the
// default formatter to remain in place.
func TestApplyDepsToGlobals_ErrorWithHintsNilPreserves(t *testing.T) {
	saved := ErrorWithHintsFn
	defer func() { ErrorWithHintsFn = saved }()

	sentinel := func(msg string, hints ...string) error {
		return fmt.Errorf("sentinel-ewh: %s", msg)
	}
	ErrorWithHintsFn = sentinel

	applyDepsToGlobals(Deps{}) // ErrorWithHints nil

	err := ErrorWithHintsFn("x")
	if err == nil || !strings.Contains(err.Error(), "sentinel-ewh: x") {
		t.Errorf("nil deps.ErrorWithHints should preserve package Fn; got %v", err)
	}
}

// TestApplyDepsToGlobals_ErrorWithHintsNonNilOverwrites pins the
// positive side: a deps-supplied formatter replaces the package var.
func TestApplyDepsToGlobals_ErrorWithHintsNonNilOverwrites(t *testing.T) {
	saved := ErrorWithHintsFn
	defer func() { ErrorWithHintsFn = saved }()

	deps := Deps{
		ErrorWithHints: func(msg string, hints ...string) error {
			return fmt.Errorf("from-deps: %s", msg)
		},
	}
	applyDepsToGlobals(deps)
	err := ErrorWithHintsFn("y")
	if err == nil || !strings.Contains(err.Error(), "from-deps: y") {
		t.Errorf("non-nil deps.ErrorWithHints should overwrite; got %v", err)
	}
}
