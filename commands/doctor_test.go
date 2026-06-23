package commands

// Per SHAPE.md OD-2 the doctor command moved to commands/lifecycle/ in t09,
// and t13b deleted the root-package shim entirely. The functional tests
// live in commands/lifecycle/doctor_test.go and
// commands/lifecycle/doctor_repair_e2e_test.go.
//
// What stays in this file:
//
//  - fakeDoctorConfigLoader: the interface-DI test double that
//    commands/seams_test.go's TestRunDoctor_ConfigLoadError constructs to
//    drive lifecycle.RunDoctor's load-error branch. It satisfies
//    lifecycle.DoctorConfigLoader via duck typing.
//
//  - A thin smoke test on lifecycle.NewDoctorCmd(buildLifecycleDeps()) to
//    pin the root.go production wiring (regression guard on the deps
//    factory + constructor call).
//
//  - A read-only regression guard for doctor (§7A.6): asserts that, run
//    through the root production wiring with a broken managed link present,
//    doctor reports the breakage but does NOT materialize the projected
//    codex toml and does NOT mutate the project tree. This replaces the
//    pre-§7A.6 stp3 repair sweep (doctor used to re-run the shared-target
//    projection + CreateLinks); repair was removed from doctor, so the
//    contract is now "detect, never fix". The deeper unit-level read-only
//    e2e tests live in commands/internal/lifecycle/doctor_repair_e2e_test.go.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AGOrcha/dot-agents/commands/internal/lifecycle"
	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/linktest"
)

// fakeDoctorConfigLoader is the interface-DI test double for
// lifecycle.DoctorConfigLoader (per docs/TEST_SEAMS.md). A nil func field
// delegates to the real config.Load implementation.
type fakeDoctorConfigLoader struct {
	loadConfig func() (*config.Config, error)
}

func (f fakeDoctorConfigLoader) LoadConfig() (*config.Config, error) {
	if f.loadConfig != nil {
		return f.loadConfig()
	}
	return config.Load()
}

// TestLifecycleDoctorCmd_BuildsCobraCommand verifies root.go's production
// wiring (lifecycle.NewDoctorCmd(buildLifecycleDeps())) surfaces the
// expected Use/Args surface. Deeper doctor behavior tests live in
// commands/lifecycle/doctor_test.go.
func TestLifecycleDoctorCmd_BuildsCobraCommand(t *testing.T) {
	cmd := lifecycle.NewDoctorCmd(buildLifecycleDeps())
	if cmd == nil {
		t.Fatal("lifecycle.NewDoctorCmd returned nil")
	}
	if cmd.Use != "doctor" {
		t.Errorf("expected Use=doctor, got %q", cmd.Use)
	}
	if err := cmd.Args(cmd, []string{"x"}); err == nil {
		t.Error("doctor takes no positional args")
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Errorf("doctor should accept zero args, got %v", err)
	}
}

// ─── §7A.6 regression: doctor is read-only (no repair) ────────────────────
//
// Before §7A.6 doctor would, on observing a broken managed link, fire a
// repair pass (RunSharedTargetProjection + per-platform CreateLinks) that
// materialized the projected codex toml. That repair was removed: doctor now
// only detects. This guard seeds a managed claude rules symlink + a canonical
// codex agent (so the projection WOULD have something to materialize), breaks
// the link, runs doctor through the root production wiring, and asserts the
// projected toml is NOT created and the project tree is left untouched.

// seedDoctorReadOnlyProject seeds a project with (a) a working managed claude
// rules symlink and (b) a canonical codex agent under agentsHome — the same
// fixture the old repair sweep used, so the read-only assertion is meaningful
// (a repair regression would re-materialize the toml here). Returns
// (tmp, agentsHome, projectPath, targetPath) where targetPath is the rules
// file callers remove to trip the audit into broken state.
func seedDoctorReadOnlyProject(t *testing.T, projectName, agentName string) (string, string, string, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("PATH/shim seeding semantics differ on Windows; skip there")
	}
	tmp := seedAllPlatformInstallSignals(t)
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	target := filepath.Join(agentsHome, "rules", projectName, "agents.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("# rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	projectPath := filepath.Join(tmp, projectName)
	claudeRules := filepath.Join(projectPath, ".claude", "rules")
	if err := os.MkdirAll(claudeRules, 0o755); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(claudeRules, projectName+"--agents.md")
	linktest.Link(t, target, linkPath)

	agentDir := filepath.Join(agentsHome, "agents", projectName, agentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + agentName + "\ndescription: doctor read-only fixture\n---\n\n# Body\nShip it.\n"
	if err := os.WriteFile(filepath.Join(agentDir, "AGENT.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	cfg.AddProject(projectName, projectPath)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	return tmp, agentsHome, projectPath, target
}

// TestDoctor_ReadOnly_NoProjectionOnBrokenLink asserts that, with a broken
// managed link present and a platform installed (the exact condition that used
// to trigger doctor's repair pass), doctor through the root production wiring
// does NOT materialize the projected codex toml and does NOT mutate the
// project tree. This is the positive guard for §7A.6's "detect, never fix"
// contract at the root-package level; the old TestDoctorRepair_* sweep that
// asserted the toml WAS materialized was removed with doctor's repair pass.
func TestDoctor_ReadOnly_NoProjectionOnBrokenLink(t *testing.T) {
	_, _, projectPath, target := seedDoctorReadOnlyProject(t, "docreadonlyproj", "implementer")

	if err := os.Remove(target); err != nil {
		t.Fatalf("break managed rule target: %v", err)
	}

	before := snapshotTree(t, projectPath)

	saved := lifecycle.Flags
	lifecycle.Flags = lifecycle.GlobalFlags{Yes: true}
	defer func() { lifecycle.Flags = saved }()

	cmd := lifecycle.NewDoctorCmd(buildLifecycleDeps())
	if err := lifecycle.RunDoctor(cmd, nil, lifecycle.StdDoctorConfigLoader{}); err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}

	tomlPath := filepath.Join(projectPath, ".codex", "agents", "implementer.toml")
	if _, err := os.Stat(tomlPath); err == nil {
		t.Fatalf("read-only doctor must NOT materialize %s; repair was not removed", tomlPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected stat error for %s: %v", tomlPath, err)
	}

	after := snapshotTree(t, projectPath)
	if msg, ok := snapshotsEqual(before, after); !ok {
		t.Fatalf("read-only doctor mutated the project tree: %s", msg)
	}
}
