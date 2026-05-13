package agents

import (
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// noOpHints returns a deps stub whose hint helpers accept any arg shape.
func noOpHints() Deps {
	accept := func(*cobra.Command, []string) error { return nil }
	return Deps{
		ErrorWithHints: func(message string, hints ...string) error { return &hintError{message: message, hints: hints} },
		UsageError:     func(message string, hints ...string) error { return &hintError{message: message, hints: hints} },
		MaximumNArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return accept
		},
		RangeArgsWithHints: func(min, max int, hints ...string) cobra.PositionalArgs {
			return accept
		},
		ExactArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return accept
		},
	}
}

func TestNewAgentsCmd_RegistersAllSubcommands(t *testing.T) {
	cmd := NewAgentsCmd(noOpHints())
	if cmd == nil || cmd.Use != "agents" {
		t.Fatalf("unexpected root: %+v", cmd)
	}

	expected := map[string]bool{
		"list": false, "new": false, "promote": false,
		"import": false, "remove": false,
	}
	for _, c := range cmd.Commands() {
		expected[c.Name()] = true
	}
	for name, found := range expected {
		if !found {
			t.Errorf("missing agents subcommand %q", name)
		}
	}
}

func TestNewAgentsCmd_ExampleBlockMentionsLifecycle(t *testing.T) {
	cmd := NewAgentsCmd(noOpHints())
	for _, needle := range []string{"agents list", "agents new", "agents promote", "agents import", "agents remove"} {
		if !strings.Contains(cmd.Example, needle) {
			t.Errorf("Example missing %q: %q", needle, cmd.Example)
		}
	}
}

func TestNewAgentsCmd_PromoteHasForceFlag(t *testing.T) {
	cmd := NewAgentsCmd(noOpHints())
	var promote *cobra.Command
	for _, c := range cmd.Commands() {
		if c.Name() == "promote" {
			promote = c
		}
	}
	if promote == nil {
		t.Fatal("promote subcommand not found")
	}
	if promote.Flags().Lookup("force") == nil {
		t.Error("promote command missing --force flag")
	}
}

func TestNewAgentsCmd_RemoveHasPurgeFlag(t *testing.T) {
	cmd := NewAgentsCmd(noOpHints())
	var remove *cobra.Command
	for _, c := range cmd.Commands() {
		if c.Name() == "remove" {
			remove = c
		}
	}
	if remove == nil {
		t.Fatal("remove subcommand not found")
	}
	if remove.Flags().Lookup("purge") == nil {
		t.Error("remove command missing --purge flag")
	}
}

func TestScopeFromArgs(t *testing.T) {
	if got := scopeFromArgs(nil); got != "global" {
		t.Errorf("scopeFromArgs(nil) = %q, want global", got)
	}
	if got := scopeFromArgs([]string{"billing"}); got != "billing" {
		t.Errorf("scopeFromArgs([billing]) = %q", got)
	}
}

func TestPathExists(t *testing.T) {
	tmp := t.TempDir()
	if pathExists(tmp + "/missing") {
		t.Error("pathExists should return false for missing path")
	}
	if !pathExists(tmp) {
		t.Error("pathExists should return true for existing path")
	}
}

// chdirToDeletedDir cds into a fresh tempdir, then removes it. On most
// Unix-likes, the process retains a working directory pointer at the now-gone
// inode and subsequent os.Getwd calls return ENOENT. Tests use this to drive
// the os.Getwd error branch inside the promote/remove/import RunE wrappers.
func chdirToDeletedDir(t *testing.T) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(orig)
	})
	if err := os.Remove(dir); err != nil {
		t.Skipf("could not remove cwd to provoke Getwd error: %v", err)
	}
	// Confirm Getwd actually errors now; otherwise skip — some kernels keep cwd valid.
	if _, err := os.Getwd(); err == nil {
		t.Skip("os.Getwd still succeeds after rmdir; cannot exercise error branch")
	}
}

func TestAgentsPromoteCmd_RunE_GetwdError(t *testing.T) {
	chdirToDeletedDir(t)
	root := NewAgentsCmd(noOpHints())
	var promote *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "promote" {
			promote = c
		}
	}
	if promote == nil {
		t.Fatal("promote not found")
	}
	err := promote.RunE(promote, []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "resolv") {
		t.Errorf("expected resolve project path error, got: %v", err)
	}
}

func TestAgentsRemoveCmd_RunE_GetwdError(t *testing.T) {
	chdirToDeletedDir(t)
	root := NewAgentsCmd(noOpHints())
	var remove *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "remove" {
			remove = c
		}
	}
	if remove == nil {
		t.Fatal("remove not found")
	}
	err := remove.RunE(remove, []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "resolv") {
		t.Errorf("expected resolve project path error, got: %v", err)
	}
}

func TestAgentsImportCmd_RunE_GetwdError(t *testing.T) {
	chdirToDeletedDir(t)
	root := NewAgentsCmd(noOpHints())
	var imp *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "import" {
			imp = c
		}
	}
	if imp == nil {
		t.Fatal("import not found")
	}
	err := imp.RunE(imp, []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "resolv") {
		t.Errorf("expected resolve project path error, got: %v", err)
	}
}

func TestAgentManifestConstant(t *testing.T) {
	if agentManifestName != "AGENT.md" {
		t.Errorf("agentManifestName = %q, want AGENT.md", agentManifestName)
	}
}
