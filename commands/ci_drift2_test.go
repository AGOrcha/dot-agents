package commands

// Tests in this file extend ci_drift_test.go to cover platform IsInstalled-true
// branches for cursor, codex, opencode, and copilot in addition to claude.
//
// On a fresh CI runner none of these binaries are in PATH, so the
// `if p.IsInstalled()` branches in runInit, runAdd, runRefresh, and runRemove
// stay dark even when claude is seeded. To exercise them deterministically we
// either:
//   - claude:  create ~/.claude/ (filesystem fallback at claude.go:230-232)
//   - copilot: create ~/.vscode/extensions/<…copilot…>/ (filesystem fallback at
//              copilot.go:131-144)
//   - cursor/codex/opencode: drop minimal executable shims into a temp dir
//              and prepend it to PATH, so exec.LookPath returns success.
//
// The shim approach is safe: the IsInstalled check only calls exec.LookPath
// (no execution). For commands that subsequently call p.Version() (which DOES
// execve the binary), our shim writes the platform name to stdout so the
// caller stays happy.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// seedAllPlatformInstallSignals sets up HOME and PATH so every platform's
// IsInstalled() returns true. The bin directory is automatically cleaned by
// t.TempDir(). Returns the temp HOME path.
//
// Note: cursor IsInstalled() checks /Applications/Cursor.app first, then
// exec.LookPath("agent"), then exec.LookPath("cursor"). We seed an `agent`
// shim, which satisfies the second check on any OS without writing under
// /Applications.
func seedAllPlatformInstallSignals(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PATH/shim seeding semantics differ on Windows; skip there")
	}
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	// claude: ~/.claude/ directory (FS fallback)
	if err := os.MkdirAll(filepath.Join(tmp, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	// copilot: ~/.vscode/extensions/<…copilot…>/ directory
	copilotExt := filepath.Join(tmp, ".vscode", "extensions", "github.copilot-1.0.0")
	if err := os.MkdirAll(copilotExt, 0o755); err != nil {
		t.Fatal(err)
	}

	// cursor/codex/opencode: PATH-based detection. Create executable shims
	// that just print their own name (Version may invoke them).
	binDir := filepath.Join(tmp, "fakebin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := "#!/bin/sh\necho \"$(basename \"$0\") 0.0.0\"\n"
	for _, name := range []string{"agent", "codex", "opencode"} {
		p := filepath.Join(binDir, name)
		if err := os.WriteFile(p, []byte(shim), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Prepend bin dir to PATH so the shims win over real binaries.
	oldPath := os.Getenv("PATH")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+oldPath)

	return tmp
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

	if err := runInit(NewInitCmd(), nil); err != nil {
		t.Fatalf("runInit (all platforms seeded): %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Every platform should have been detected and recorded as enabled.
	for _, id := range []string{"claude", "cursor", "codex", "opencode", "copilot"} {
		if !cfg.IsPlatformEnabled(id) {
			t.Errorf("expected %s enabled after init with all signals seeded", id)
		}
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

	if err := runAdd(projectPath, ""); err != nil {
		t.Fatalf("runAdd (all platforms seeded): %v", err)
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

	// Non-dry-run path so refresh.go:171 CreateLinks is invoked for every
	// installed enabled platform.
	if err := runRefresh(""); err != nil {
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

	if err := runRefresh(""); err != nil {
		t.Errorf("runRefresh dry-run (all platforms seeded): %v", err)
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
