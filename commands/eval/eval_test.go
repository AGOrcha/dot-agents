package eval

import (
	"testing"

	"github.com/spf13/cobra"
)

// noopHandler is a stub RunE for command-shape tests that never execute.
func noopHandler(*cobra.Command, []string) error { return nil }

func TestNewCmdWiresSubcommands(t *testing.T) {
	cmd := NewCmd(noopHandler, noopHandler, noopHandler)
	if cmd.Use != "eval" {
		t.Fatalf("Use = %q, want eval", cmd.Use)
	}
	want := map[string]bool{"gen": false, "run": false, "ls": false}
	for _, sub := range cmd.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("subcommand %q not registered", name)
		}
	}
}

func TestResolveRepoDir(t *testing.T) {
	if got := resolveRepoDir("/explicit/root"); got != "/explicit/root" {
		t.Errorf("explicit = %q, want /explicit/root", got)
	}
	// Empty falls back to a non-empty cwd on a live process.
	if got := resolveRepoDir(""); got == "" {
		t.Error("empty repo-dir should fall back to a non-empty cwd")
	}
}

func TestFlagString(t *testing.T) {
	cmd := newGenCmd(noopHandler)
	if err := cmd.Flags().Set(languageFlagName, "go"); err != nil {
		t.Fatalf("set flag: %v", err)
	}
	if got := flagString(cmd, languageFlagName); got != "go" {
		t.Errorf("flagString(language) = %q, want go", got)
	}
	// An undefined flag resolves to empty, not a panic.
	if got := flagString(cmd, "no-such-flag"); got != "" {
		t.Errorf("flagString(undefined) = %q, want empty", got)
	}
}
