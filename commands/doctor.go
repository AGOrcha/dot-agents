package commands

// Transitional shim for the doctor command — t09 of root-command-decomposition.
//
// The implementation moved to commands/lifecycle/doctor.go in this task.
// This file remains as a thin wiring shim because:
//
//  1. commands/seams_test.go (split deferred to t11) reaches the doctor
//     pipeline through the unexported runDoctor symbol in package commands
//     plus the doctorConfigLoader interface. The lower-cased forwarder
//     below keeps that compile-time reference valid until seams_test.go is
//     re-homed.
//
//  2. commands/root.go still imports commands.NewDoctorCmd; the switch to
//     lifecycle.NewDoctorCmd directly happens in t13 when this file is
//     deleted entirely.
//
// Per SHAPE.md OD-2 the root-package shims are deleted in t13. Until then
// this file is the single sync point between the doctor RunE closure and
// the lifecycle package globals (Flags, ErrorWithHintsFn).

import (
	"github.com/NikashPrakash/dot-agents/commands/lifecycle"
	"github.com/spf13/cobra"
)

// doctorConfigLoader aliases lifecycle.DoctorConfigLoader so commands/
// seams_test.go's fakeDoctorConfigLoader (still in root until t11) keeps
// satisfying the parameter type expected by the lower-cased forwarder
// below.
type doctorConfigLoader = lifecycle.DoctorConfigLoader

// stdDoctorConfigLoader aliases lifecycle.StdDoctorConfigLoader so test
// sites that construct stdDoctorConfigLoader{} keep compiling.
type stdDoctorConfigLoader = lifecycle.StdDoctorConfigLoader

// lifecycleDoctorDeps builds the Deps struct passed to lifecycle.NewDoctorCmd.
// Mirrors lifecycleDeps() in commands/install.go and lifecycleStatusDeps()
// in commands/status.go.
func lifecycleDoctorDeps() lifecycle.Deps {
	return lifecycle.Deps{
		Flags: lifecycle.GlobalFlags{
			DryRun:  Flags.DryRun,
			Force:   Flags.Force,
			Verbose: Flags.Verbose,
			Yes:     Flags.Yes,
		},
		ErrorWithHints:        ErrorWithHints,
		UsageError:            UsageError,
		MaximumNArgsWithHints: MaximumNArgsWithHints,
		RangeArgsWithHints:    RangeArgsWithHints,
		ExactArgsWithHints:    ExactArgsWithHints,
		NoArgsWithHints:       NoArgsWithHints,
		ExampleBlock:          ExampleBlock,
	}
}

// NewDoctorCmd wires the doctor subcommand. Thin shim around
// lifecycle.NewDoctorCmd; the RunE closure is rewrapped so we sync the
// commands-package globals into lifecycle's parallel package vars right
// before the moved implementation reads them. See commands/install.go's
// syncLifecycleGlobals for the shared sync helper.
func NewDoctorCmd() *cobra.Command {
	cmd := lifecycle.NewDoctorCmd(lifecycleDoctorDeps())
	innerRunE := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		syncLifecycleGlobals()
		return innerRunE(c, args)
	}
	return cmd
}

// runDoctor is the root-package forwarder for lifecycle.RunDoctor.
// commands/seams_test.go's TestRunDoctor_ConfigLoadError calls it with a
// fake doctorConfigLoader; the cmd/args arguments are ignored by doctor's
// run body but kept on the signature so existing test call sites keep
// compiling. The lifecycle globals are synced first so the moved
// implementation observes live commands-package state.
func runDoctor(cmd *cobra.Command, args []string, deps doctorConfigLoader) error {
	syncLifecycleGlobals()
	return lifecycle.RunDoctor(cmd, args, deps)
}
