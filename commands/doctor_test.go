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
//  - The stp3 cross-cutting regression sweep for doctor's SharedTargetPlan
//    wiring (repairManagedProject → RunSharedTargetProjection): asserts
//    that doctor's repair pass materializes the projected codex toml when a
//    project has at least one broken managed link, that the dry-run
//    variant does NOT, and that a second doctor pass on a repaired project
//    is byte-identical (idempotent). The deeper unit-level e2e tests live
//    in commands/internal/lifecycle/doctor_repair_e2e_test.go; the tests
//    here pin the cross-cutting projection contract from the root commands
//    package per the stp3-regression-parity bundle write scope.

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/NikashPrakash/dot-agents/commands/internal/lifecycle"
	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/linktest"
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

// ─── stp3 regression: doctor SharedTargetProjection wiring ────────────────
//
// These tests pin the stp-doctor-repair wiring (doctor.go:479
// repairManagedProject → RunSharedTargetProjection). doctor only fires
// repairManagedProject when the link-health audit observes a broken
// managed link, so we seed a managed claude rules symlink, break it, then
// run doctor through the root-package wiring and assert the projected
// codex toml materializes — that artifact is the projection's exclusive
// output, not CreateLinks's.

// seedManagedDoctorRepairProject seeds a project with (a) a working
// managed claude rules symlink, (b) a canonical codex agent under
// agentsHome so the projection has something to materialize. Returns
// (tmp, agentsHome, projectPath, targetPath) where targetPath is the rules
// file that callers should remove to trip the audit into broken state.
func seedManagedDoctorRepairProject(t *testing.T, projectName, agentName string) (string, string, string, string) {
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
	body := "---\nname: " + agentName + "\ndescription: doctor stp3 fixture\n---\n\n# Body\nShip it.\n"
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

// TestDoctorRepair_RunsSharedTargetProjection asserts that when doctor's
// audit flags at least one broken managed link, the repair pass runs the
// shared-target projection and materializes the projected codex toml.
// The projection is the ONLY producer of .codex/agents/<n>.toml; the per-
// platform CreateLinks loop cannot create it. A regression that removes
// the projection call from repairManagedProject (or swallows its error
// path) will leave the file absent and fail this test.
func TestDoctorRepair_RunsSharedTargetProjection(t *testing.T) {
	_, _, projectPath, target := seedManagedDoctorRepairProject(t, "docrepairproj", "implementer")

	if err := os.Remove(target); err != nil {
		t.Fatalf("break managed rule target: %v", err)
	}

	saved := lifecycle.Flags
	lifecycle.Flags = lifecycle.GlobalFlags{Yes: true}
	defer func() { lifecycle.Flags = saved }()

	cmd := lifecycle.NewDoctorCmd(buildLifecycleDeps())
	if err := lifecycle.RunDoctor(cmd, nil, lifecycle.StdDoctorConfigLoader{}); err != nil {
		t.Fatalf("RunDoctor: %v", err)
	}

	tomlPath := filepath.Join(projectPath, ".codex", "agents", "implementer.toml")
	if _, err := os.Stat(tomlPath); err != nil {
		t.Fatalf("doctor repair must materialize %s via SharedTargetProjection: %v", tomlPath, err)
	}
}

// TestDoctorRepair_DryRunNoProjectionMutation asserts that doctor in
// dry-run mode does NOT materialize the projected codex toml even when
// the audit observes a broken managed link. repairManagedProject's
// Flags.DryRun branch only prints "would" lines and must skip the
// RunSharedTargetProjection apply call.
func TestDoctorRepair_DryRunNoProjectionMutation(t *testing.T) {
	_, _, projectPath, target := seedManagedDoctorRepairProject(t, "docdrproj", "implementer")

	if err := os.Remove(target); err != nil {
		t.Fatalf("break managed rule target: %v", err)
	}

	saved := lifecycle.Flags
	lifecycle.Flags = lifecycle.GlobalFlags{Yes: true, DryRun: true}
	defer func() { lifecycle.Flags = saved }()

	cmd := lifecycle.NewDoctorCmd(buildLifecycleDeps())
	if err := lifecycle.RunDoctor(cmd, nil, lifecycle.StdDoctorConfigLoader{}); err != nil {
		t.Fatalf("RunDoctor dry-run: %v", err)
	}

	tomlPath := filepath.Join(projectPath, ".codex", "agents", "implementer.toml")
	if _, err := os.Stat(tomlPath); err == nil {
		t.Fatalf("dry-run doctor must NOT materialize %s; repair is ignoring Flags.DryRun", tomlPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected stat error for %s: %v", tomlPath, err)
	}
}

// TestDoctorRepair_IdempotentAfterRepair asserts that once doctor has
// repaired a project (audit fires, projection runs, links re-created), a
// second doctor pass on the now-healthy state leaves the projected .codex/
// tree byte-identical. The projection's Execute must be a no-op on already
// correct state and the per-platform CreateLinks pass must not churn the
// rendered file's content/mtime.
func TestDoctorRepair_IdempotentAfterRepair(t *testing.T) {
	_, _, projectPath, target := seedManagedDoctorRepairProject(t, "docidemproj", "implementer")

	if err := os.Remove(target); err != nil {
		t.Fatalf("break managed rule target: %v", err)
	}

	saved := lifecycle.Flags
	lifecycle.Flags = lifecycle.GlobalFlags{Yes: true}
	defer func() { lifecycle.Flags = saved }()

	cmd := lifecycle.NewDoctorCmd(buildLifecycleDeps())
	if err := lifecycle.RunDoctor(cmd, nil, lifecycle.StdDoctorConfigLoader{}); err != nil {
		t.Fatalf("first RunDoctor: %v", err)
	}

	codexDir := filepath.Join(projectPath, ".codex")
	first := snapshotTree(t, codexDir)
	if len(first) == 0 {
		t.Fatalf("first doctor pass produced no .codex/ artifacts; repair projection did not run")
	}

	if err := lifecycle.RunDoctor(cmd, nil, lifecycle.StdDoctorConfigLoader{}); err != nil {
		t.Fatalf("second RunDoctor: %v", err)
	}
	second := snapshotTree(t, codexDir)

	if msg, ok := snapshotsEqual(first, second); !ok {
		t.Fatalf("doctor repair not idempotent under .codex/: %s\nfirst=%d second=%d",
			msg, len(first), len(second))
	}
}
