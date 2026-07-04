package eval

import (
	"testing"
)

func TestNewCmdWiresSubcommands(t *testing.T) {
	cmd := NewCmd(Deps{JSON: func() bool { return false }})
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

func TestDepsJSON(t *testing.T) {
	if (Deps{}).json() {
		t.Error("nil JSON getter should resolve to false")
	}
	if !(Deps{JSON: func() bool { return true }}).json() {
		t.Error("JSON getter returning true should resolve to true")
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
