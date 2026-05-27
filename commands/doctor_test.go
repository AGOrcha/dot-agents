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

import (
	"testing"

	"github.com/NikashPrakash/dot-agents/commands/internal/lifecycle"
	"github.com/NikashPrakash/dot-agents/internal/config"
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
