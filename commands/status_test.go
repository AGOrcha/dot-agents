package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// fakeStatusConfigLoader is the interface-DI test double for the lifecycle
// StatusConfigLoader. Preserved in root (commands) package during the
// t08→t11 window because commands/seams_test.go's TestRunStatus_ConfigLoadError
// reaches for it cross-file. Moves into commands/lifecycle/seams_test.go in
// t11 alongside the rest of the lifecycle seam tests.
type fakeStatusConfigLoader struct {
	loadConfig func() (*config.Config, error)
}

func (f fakeStatusConfigLoader) LoadConfig() (*config.Config, error) {
	if f.loadConfig != nil {
		return f.loadConfig()
	}
	return config.Load()
}

// TestNewStatusCmdShim_BuildsCobraCommand verifies the root shim wires the
// lifecycle.NewStatusCmd constructor and surfaces the same Use/flags surface
// callers expect. Deeper status behavior tests live in
// commands/lifecycle/status_test.go alongside the moved implementation
// (per the t08 move; see SHAPE.md §3a).
func TestNewStatusCmdShim_BuildsCobraCommand(t *testing.T) {
	cmd := NewStatusCmd()
	if cmd == nil {
		t.Fatal("NewStatusCmd() returned nil")
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

// TestNewStatusCmdShim_LifecycleDepsWiringPopulated pins that the
// lifecycle.Deps factory leaves no required hint helper nil — a regression
// that drops one would surface only as a confusing nil-deref inside
// statusNoArgsHint at the first --help invocation otherwise.
func TestNewStatusCmdShim_LifecycleDepsWiringPopulated(t *testing.T) {
	deps := lifecycleStatusDeps()
	if deps.ErrorWithHints == nil {
		t.Error("lifecycleStatusDeps.ErrorWithHints is nil")
	}
	if deps.UsageError == nil {
		t.Error("lifecycleStatusDeps.UsageError is nil")
	}
	if deps.ExactArgsWithHints == nil {
		t.Error("lifecycleStatusDeps.ExactArgsWithHints is nil")
	}
}

// TestNewStatusCmdShim_RunEClosureFires exercises the RunE closure the shim
// rebinds (the lifecycle.NewStatusCmd default closure is replaced so the
// globalflagcov static analyzer can resolve Flags.JSON on ./commands).
// Without this test the closure body (audit/agent flag parse +
// lifecycle.RunStatusDefault dispatch) shows as 0% local coverage and
// NewStatusCmd drops below the per-file gate.
func TestNewStatusCmdShim_RunEClosureFires(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatalf("mkdir agentsHome: %v", err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// Hermetic empty config so lifecycle.RunStatusDefault has nothing to
	// iterate but still exercises the JSON-off code path.
	cfg := &config.Config{Version: 1, Projects: map[string]config.Project{}, Agents: map[string]config.Agent{}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("cfg.Save: %v", err)
	}

	cmd := NewStatusCmd()
	cmd.SetArgs([]string{})
	cmd.SetOut(os.Stderr)
	cmd.SetErr(os.Stderr)
	if err := cmd.Execute(); err != nil {
		t.Errorf("NewStatusCmd Execute: %v", err)
	}
}
