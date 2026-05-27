package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/commands/lifecycle"
	"github.com/spf13/cobra"
)

// findSubcommand returns the named subcommand of root or fails the test.
func findSubcommand(t *testing.T, root *cobra.Command, name string) *cobra.Command {
	t.Helper()
	for _, c := range root.Commands() {
		if c.Name() == name {
			return c
		}
	}
	t.Fatalf("subcommand %q not found", name)
	return nil
}

// setupAgentsHome creates a fake AGENTS_HOME and HOME at t.TempDir for tests
// that exercise subcommands which inspect ~/.agents.
func setupAgentsHomeAndHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, "agents")
	fakeHome := filepath.Join(tmp, "home")
	for _, d := range []string{agentsHome, fakeHome} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	t.Setenv("HOME", fakeHome)
	return agentsHome
}

// ── newSyncPullCmd ───────────────────────────────────────────────────────────

func TestNewSyncPullCmd_ReturnsPullSubcommand(t *testing.T) {
	cmd := newSyncPullCmd()
	if cmd == nil {
		t.Fatal("newSyncPullCmd returned nil")
	}
	if cmd.Name() != "pull" {
		t.Errorf("name = %q; want 'pull'", cmd.Name())
	}
}

// ── review.go subcommand RunE coverage ──────────────────────────────────────

func TestNewReviewCmd_RunEDispatches(t *testing.T) {
	setupAgentsHomeAndHome(t)
	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	cmd := NewReviewCmd()
	// Top-level RunE → runReviewList()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("review root RunE: %v", err)
	}
	// Subcommand RunE wrappers exist and error on unknown id.
	for _, name := range []string{"show", "approve", "reject"} {
		sub := findSubcommand(t, cmd, name)
		if err := sub.RunE(sub, []string{"nonexistent-id"}); err == nil {
			t.Errorf("review %s RunE should error on unknown id", name)
		}
	}
}

// ── lifecycle.NewInstallCmd RunE coverage ───────────────────────────────────

func TestLifecycleInstallCmd_RunEGenerate(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0o755)
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	// Switch CWD to a project dir so install --generate works in dry-run.
	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0o755)
	old, _ := os.Getwd()
	os.Chdir(projectPath)
	t.Cleanup(func() {
		os.Chdir(old)
	})

	cmd := lifecycle.NewInstallCmd(buildLifecycleDeps())
	// Flip --generate by setting flag value before RunE.
	if err := cmd.Flags().Set("generate", "true"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("install --generate RunE dry-run: %v", err)
	}
}

func TestLifecycleInstallCmd_RunEDispatch_NoManifest(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0o755)
	t.Setenv("AGENTS_HOME", agentsHome)

	projectPath := filepath.Join(tmp, "p")
	os.MkdirAll(projectPath, 0o755)
	old, _ := os.Getwd()
	os.Chdir(projectPath)
	t.Cleanup(func() { os.Chdir(old) })

	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	cmd := lifecycle.NewInstallCmd(buildLifecycleDeps())
	// generate=false (default) → invokes lifecycle.RunInstall, which errors on
	// missing manifest.
	if err := cmd.RunE(cmd, nil); err == nil {
		t.Error("expected error from install RunE when manifest missing")
	}
}

// ── import.go RunE coverage ─────────────────────────────────────────────────

func TestNewImportCmd_RunEDispatches(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	agentsHome := filepath.Join(tmp, ".agents")
	os.MkdirAll(agentsHome, 0o755)
	t.Setenv("AGENTS_HOME", agentsHome)

	saved := Flags
	Flags = GlobalFlags{Yes: true, DryRun: true}
	defer func() { Flags = saved }()

	cmd := NewImportCmd()
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("import RunE no-args: %v", err)
	}
	// With unknown project name → RunE returns an error (still exercises the closure).
	if err := cmd.RunE(cmd, []string{"some-project"}); err == nil {
		t.Error("expected error for unknown project filter")
	}
}

// ── remove.go RunE coverage ─────────────────────────────────────────────────

func TestNewRemoveCmd_RunEDispatchesError(t *testing.T) {
	setupAgentsHomeAndHome(t)
	saved := Flags
	Flags = GlobalFlags{Yes: true}
	defer func() { Flags = saved }()

	cmd := NewRemoveCmd()
	// Unknown project name → runRemove returns an error.
	if err := cmd.RunE(cmd, []string{"ghost-project-name"}); err == nil {
		t.Error("expected remove RunE to error on unknown project")
	}
}
