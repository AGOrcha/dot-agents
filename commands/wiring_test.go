package commands

import (
	"errors"
	"testing"
)

// TestKGDeps_PopulatesFromGlobalFlagsAndUXHelpers asserts that kgDeps() snapshots
// the package-level GlobalFlags (JSON/DryRun) and forwards ExampleBlock.
func TestKGDeps_PopulatesFromGlobalFlagsAndUXHelpers(t *testing.T) {
	origJSON, origDry := Flags.JSON, Flags.DryRun
	t.Cleanup(func() { Flags.JSON, Flags.DryRun = origJSON, origDry })

	Flags.JSON = true
	Flags.DryRun = true
	d := kgDeps()
	if !d.Flags.JSON || !d.Flags.DryRun {
		t.Fatalf("kgDeps did not snapshot GlobalFlags: %+v", d.Flags)
	}
	if d.ExampleBlock == nil {
		t.Fatal("kgDeps did not forward ExampleBlock")
	}
	// Sanity-check the forwarded helper returns a non-empty rendered example.
	if got := d.ExampleBlock("line-a", "line-b"); got == "" {
		t.Error("ExampleBlock should produce a non-empty rendering")
	}

	Flags.JSON = false
	Flags.DryRun = false
	d2 := kgDeps()
	if d2.Flags.JSON || d2.Flags.DryRun {
		t.Errorf("kgDeps did not re-snapshot cleared GlobalFlags: %+v", d2.Flags)
	}
}

// TestNewKGCmd_WiresUnderlyingTree verifies the kg cobra tree is built and
// reachable from the commands wiring.
func TestNewKGCmd_WiresUnderlyingTree(t *testing.T) {
	cmd := NewKGCmd()
	if cmd == nil {
		t.Fatal("NewKGCmd returned nil")
	}
	if cmd.Name() != "kg" {
		t.Errorf("expected kg root, got %q", cmd.Name())
	}
	if len(cmd.Commands()) == 0 {
		t.Error("expected kg subcommands to be registered")
	}
}

// TestWorkflowBridgeDeps_GetterFlagsTrackPackageFlags exercises the three
// JSON/Yes/DryRun closures on Deps.Flags so they actually run during a test
// (the lambda bodies are otherwise uncovered).
func TestWorkflowBridgeDeps_GetterFlagsTrackPackageFlags(t *testing.T) {
	origJSON, origYes, origDry := Flags.JSON, Flags.Yes, Flags.DryRun
	t.Cleanup(func() {
		Flags.JSON, Flags.Yes, Flags.DryRun = origJSON, origYes, origDry
	})

	// All flags off: getters must return false.
	Flags.JSON, Flags.Yes, Flags.DryRun = false, false, false
	d := WorkflowBridgeDeps()
	if d.Flags.JSON() || d.Flags.Yes() || d.Flags.DryRun() {
		t.Fatalf("expected all-false getters, got JSON=%v Yes=%v DryRun=%v",
			d.Flags.JSON(), d.Flags.Yes(), d.Flags.DryRun())
	}

	// Flip each flag and confirm the captured closure sees the new value.
	Flags.JSON = true
	Flags.Yes = true
	Flags.DryRun = true
	if !d.Flags.JSON() || !d.Flags.Yes() || !d.Flags.DryRun() {
		t.Errorf("getters did not reflect updated globals: JSON=%v Yes=%v DryRun=%v",
			d.Flags.JSON(), d.Flags.Yes(), d.Flags.DryRun())
	}

	if d.ErrNoProject == nil {
		t.Fatal("WorkflowBridgeDeps did not forward ErrNoProject")
	}
	if !errors.Is(d.ErrNoProject, errNoWorkflowProject) {
		t.Errorf("ErrNoProject did not propagate errNoWorkflowProject")
	}
	if d.ExampleBlock == nil || d.ErrorWithHints == nil || d.UsageError == nil {
		t.Error("WorkflowBridgeDeps did not forward UX helpers")
	}
	if d.NoArgsWithHints == nil || d.ExactArgsWithHints == nil || d.MaximumNArgsWithHints == nil {
		t.Error("WorkflowBridgeDeps did not forward arg-validators")
	}
}

// TestNewWorkflowCmd_WiresUnderlyingTree confirms the workflow tree is
// constructed via the package-level wiring.
func TestNewWorkflowCmd_WiresUnderlyingTree(t *testing.T) {
	cmd := NewWorkflowCmd()
	if cmd == nil {
		t.Fatal("NewWorkflowCmd returned nil")
	}
	if cmd.Name() != "workflow" {
		t.Errorf("expected workflow root, got %q", cmd.Name())
	}
	if len(cmd.Commands()) == 0 {
		t.Error("expected workflow subcommands to be registered")
	}
}
