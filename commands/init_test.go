package commands

import (
	"os"
	"testing"

	"github.com/NikashPrakash/dot-agents/commands/lifecycle"
	"github.com/spf13/cobra"
)

// Test-only forwarders so seams_test.go (which stays in package commands
// until t11) keeps compiling after init's implementation moved to
// commands/lifecycle/. Each forwarder routes to the lifecycle package's
// exported test-helper surface. Removed by t11 alongside the rest of
// seams_test.go's lifecycle-targeted tests.

// fakeInitDirMaker mirrors the lifecycle-package test double for
// fault-injecting MkdirAll. Implements the lifecycle.InitDirMakerForTest
// contract via duck-typing through scaffoldWorkflowAssets / runInit
// which both accept the lifecycle-internal initDirMaker interface.
type fakeInitDirMaker struct {
	mkdirAll func(string, os.FileMode) error
}

func (f fakeInitDirMaker) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAll != nil {
		return f.mkdirAll(path, perm)
	}
	return os.MkdirAll(path, perm)
}

func scaffoldWorkflowAssets(agentsHome string, deps fakeInitDirMaker) error {
	return lifecycle.ScaffoldWorkflowAssetsForTest(agentsHome, deps)
}

func runInit(cmd *cobra.Command, args []string, deps fakeInitDirMaker) error {
	// seams_test.go's TestRunInit_MkdirError sets commands.Flags before
	// calling, so route through the same shim wiring NewInitCmd uses to
	// keep the seam pointed at commands.Flags.
	lifecycle.SetInitFlags(
		func() bool { return Flags.Force },
		func() bool { return Flags.DryRun },
		func() bool { return Flags.Yes },
	)
	return lifecycle.RunInitForTest(cmd, args, deps)
}

// TestNewInitCmd_ShimWiresLifecycleSeams verifies the shim repoints
// every lifecycle init seam at the parent commands package — without
// this, the shim could silently leave the no-op defaults in place and
// init would lose its UsageError formatting + KG MCP scaffolding without
// any test failing. The behavioral substance of init lives in
// commands/lifecycle/init_test.go; this is a contract test for the shim.
func TestNewInitCmd_ShimWiresLifecycleSeams(t *testing.T) {
	// Reset to capture-the-defaults baseline first.
	lifecycle.InitUsageErrorFn = nil
	lifecycle.InitEnsureGlobalKGMCPConfigsFn = nil

	saved := Flags
	Flags = GlobalFlags{Force: true, DryRun: true, Yes: true}
	defer func() { Flags = saved }()

	cmd := NewInitCmd()
	if cmd == nil {
		t.Fatal("NewInitCmd returned nil")
	}
	if cmd.Use != "init" {
		t.Errorf("expected Use=init, got %q", cmd.Use)
	}

	// After the shim runs, getter Fns must read from commands.Flags.
	if !lifecycle.InitForceFn() {
		t.Error("shim did not wire InitForceFn to commands.Flags.Force")
	}
	if !lifecycle.InitDryRunFn() {
		t.Error("shim did not wire InitDryRunFn to commands.Flags.DryRun")
	}
	if !lifecycle.InitYesFn() {
		t.Error("shim did not wire InitYesFn to commands.Flags.Yes")
	}
	if lifecycle.InitUsageErrorFn == nil {
		t.Error("shim did not wire InitUsageErrorFn")
	}
	if lifecycle.InitEnsureGlobalKGMCPConfigsFn == nil {
		t.Error("shim did not wire InitEnsureGlobalKGMCPConfigsFn")
	}
}

// TestNewInitCmd_ShimRejectsPositionalArgs is a thin negative check
// confirming the moved init command still rejects positional args
// through the shim (root-level integration with cobra's Args hook).
func TestNewInitCmd_ShimRejectsPositionalArgs(t *testing.T) {
	cmd := NewInitCmd()
	if err := cmd.Args(cmd, []string{"unexpected"}); err == nil {
		t.Error("init should reject positional args")
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Errorf("init with no args should succeed: %v", err)
	}
}
