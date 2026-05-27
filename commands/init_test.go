package commands

// Per SHAPE.md OD-2 the init command moved to commands/lifecycle/ in t05,
// and t13b deleted the root-package shim entirely. The functional tests
// live in commands/lifecycle/init_test.go.
//
// What stays in this file:
//
//  - fakeInitDirMaker: the interface-DI test double for
//    lifecycle.RunInitForTest / lifecycle.ScaffoldWorkflowAssetsForTest
//    (consumed by commands/seams_test.go's TestScaffoldWorkflowAssets_MkdirError
//    and TestRunInit_MkdirError).
//
//  - A thin smoke test on lifecycle.NewInitCmd(buildLifecycleDeps()) to
//    pin the root.go production wiring (regression guard on the deps
//    factory + constructor call).

import (
	"os"
	"testing"

	"github.com/NikashPrakash/dot-agents/commands/internal/lifecycle"
)

// fakeInitDirMaker mirrors the lifecycle-package test double for
// fault-injecting MkdirAll. Satisfies lifecycle's internal initDirMaker
// interface via duck typing (single MkdirAll method).
type fakeInitDirMaker struct {
	mkdirAll func(string, os.FileMode) error
}

func (f fakeInitDirMaker) MkdirAll(path string, perm os.FileMode) error {
	if f.mkdirAll != nil {
		return f.mkdirAll(path, perm)
	}
	return os.MkdirAll(path, perm)
}

// TestLifecycleInitCmd_BuildsAndRejectsPositionalArgs is a thin smoke
// test confirming the production wiring path root.go uses returns a
// working command that rejects positional args. Deeper init behavior
// tests live in commands/lifecycle/init_test.go.
func TestLifecycleInitCmd_BuildsAndRejectsPositionalArgs(t *testing.T) {
	cmd := lifecycle.NewInitCmd(buildLifecycleDeps())
	if cmd == nil {
		t.Fatal("lifecycle.NewInitCmd returned nil")
	}
	if cmd.Use != "init" {
		t.Errorf("expected Use=init, got %q", cmd.Use)
	}
	if err := cmd.Args(cmd, []string{"unexpected"}); err == nil {
		t.Error("init should reject positional args")
	}
	if err := cmd.Args(cmd, nil); err != nil {
		t.Errorf("init with no args should succeed: %v", err)
	}
}
