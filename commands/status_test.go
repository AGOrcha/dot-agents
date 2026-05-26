package commands

import (
	"testing"

	"github.com/NikashPrakash/dot-agents/commands/lifecycle"
	"github.com/NikashPrakash/dot-agents/internal/config"
)

// fakeStatusConfigLoader is the interface-DI test double for the lifecycle
// StatusConfigLoader. Kept in package commands after t13b deleted the
// status shim because commands/seams_test.go's TestRunStatus_ConfigLoadError
// stays in this package and calls lifecycle.RunStatus directly with this
// fake (which satisfies lifecycle.StatusConfigLoader via duck typing).
type fakeStatusConfigLoader struct {
	loadConfig func() (*config.Config, error)
}

func (f fakeStatusConfigLoader) LoadConfig() (*config.Config, error) {
	if f.loadConfig != nil {
		return f.loadConfig()
	}
	return config.Load()
}

// TestLifecycleStatusCmd_BuildsCobraCommand verifies root.go's production
// wiring (lifecycle.NewStatusCmd(buildLifecycleDeps(), jsonFn)) surfaces
// the same Use/flags surface callers expect. Deeper status behavior tests
// live in commands/lifecycle/status_test.go alongside the moved
// implementation.
func TestLifecycleStatusCmd_BuildsCobraCommand(t *testing.T) {
	cmd := lifecycle.NewStatusCmd(buildLifecycleDeps(), func() bool { return Flags.JSON })
	if cmd == nil {
		t.Fatal("lifecycle.NewStatusCmd returned nil")
	}
	if cmd.Use != "status" {
		t.Errorf("expected Use=status, got %q", cmd.Use)
	}
	if cmd.Flags().Lookup("audit") == nil {
		t.Error("missing --audit flag")
	}
	if cmd.Flags().Lookup("agent") == nil {
		t.Error("missing --agent flag")
	}
}
