package commands

// Per SHAPE.md OD-2 the doctor command moved to commands/lifecycle/ in t09.
// The functional tests live in commands/lifecycle/doctor_test.go and
// commands/lifecycle/doctor_repair_e2e_test.go (the moved twin of the
// pre-t09 commands/doctor_test.go and commands/doctor_repair_e2e_test.go).
//
// What stays in this file:
//
//  - fakeDoctorConfigLoader: the interface-DI test double that
//    commands/seams_test.go (deferred to t11) constructs to drive the
//    runDoctor shim's load-error branch. Mirrors commands/lifecycle/
//    doctor_test.go's copy byte-for-byte until t11 collapses the
//    duplicate.
//
//  - Shim tests pinning the root-package shim's wiring: NewDoctorCmd's
//    Use/Args/RunE surface, lifecycleDoctorDeps factory wiring, and the
//    RunE closure dispatch. These mirror commands/status_test.go's shim
//    tests and exist to keep the per-file coverage gate satisfied on
//    commands/doctor.go (the moved implementation already has deep
//    coverage in commands/lifecycle/doctor_test.go).
//
// All three groups are deleted together when t11 splits seams_test.go.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// fakeDoctorConfigLoader is the interface-DI test double for
// doctorConfigLoader (per docs/TEST_SEAMS.md). A nil func field delegates
// to the real config.Load implementation.
type fakeDoctorConfigLoader struct {
	loadConfig func() (*config.Config, error)
}

func (f fakeDoctorConfigLoader) LoadConfig() (*config.Config, error) {
	if f.loadConfig != nil {
		return f.loadConfig()
	}
	return config.Load()
}

// TestPrintAuditShim_ForwardsToLifecycle covers the
// commands/status.go printAudit forwarder. The shim's only caller
// (commands/doctor.go's reportOneProjectLinkHealth verbose branches) moved
// into lifecycle in t09, leaving the forwarder with no production caller.
// It survives until t13 alongside the rest of the status.go shims. This
// test gives the dead-but-typed forwarder one call site so the per-file
// coverage gate stays >=95% on commands/status.go through the t09→t13
// window. After t13 deletes the shim this test goes with it.
//
// The test lives in commands/doctor_test.go (not commands/status_test.go)
// because the t09 write scope covers commands/doctor*.go; commands/
// status.go and its test file are outside this task's edit boundary.
func TestPrintAuditShim_ForwardsToLifecycle(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// Empty config + project path so printAudit's per-platform helpers run
	// against an empty managed tree and exit cleanly.
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	printAudit("proj", filepath.Join(tmp, "proj"), agentsHome, "", cfg)
}

// TestNewDoctorCmdShim_BuildsCobraCommand verifies the root shim wires
// lifecycle.NewDoctorCmd and surfaces the expected Use/Args surface.
// Deeper doctor behavior tests live in commands/lifecycle/doctor_test.go.
func TestNewDoctorCmdShim_BuildsCobraCommand(t *testing.T) {
	cmd := NewDoctorCmd()
	if cmd == nil {
		t.Fatal("NewDoctorCmd() returned nil")
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

// TestNewDoctorCmdShim_LifecycleDepsWiringPopulated pins that the
// lifecycle.Deps factory leaves no required hint helper nil — a regression
// that drops one would surface only as a confusing nil-deref inside
// deps.NoArgsWithHints / deps.ExampleBlock at the first --help invocation.
func TestNewDoctorCmdShim_LifecycleDepsWiringPopulated(t *testing.T) {
	deps := lifecycleDoctorDeps()
	if deps.ErrorWithHints == nil {
		t.Error("lifecycleDoctorDeps.ErrorWithHints is nil")
	}
	if deps.UsageError == nil {
		t.Error("lifecycleDoctorDeps.UsageError is nil")
	}
	if deps.NoArgsWithHints == nil {
		t.Error("lifecycleDoctorDeps.NoArgsWithHints is nil")
	}
	if deps.ExampleBlock == nil {
		t.Error("lifecycleDoctorDeps.ExampleBlock is nil")
	}
}

// TestNewDoctorCmdShim_RunEClosureFires exercises the RunE closure the
// shim wraps so syncLifecycleGlobals + inner RunE both register as
// covered. Without this test the closure body shows as 0% local coverage
// and NewDoctorCmd drops below the per-file gate.
func TestNewDoctorCmdShim_RunEClosureFires(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatalf("mkdir agentsHome: %v", err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// Hermetic empty config so the doctor pipeline has nothing to iterate
	// but still exercises the full RunE path.
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save: %v", err)
	}

	saved := Flags
	Flags = GlobalFlags{}
	defer func() { Flags = saved }()

	cmd := NewDoctorCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(os.Stderr)
	cmd.SetErr(os.Stderr)
	if err := cmd.Execute(); err != nil {
		t.Errorf("NewDoctorCmd Execute: %v", err)
	}
}
