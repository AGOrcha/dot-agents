package observability_test

import (
	"testing"

	"github.com/AGOrcha/dot-agents/commands"
)

func TestRootRegistersObservabilityCommand(t *testing.T) {
	root := commands.NewRootCommand()
	cmd, _, err := root.Find([]string{"observability"})
	if err != nil {
		t.Fatalf("find observability: %v", err)
	}
	if cmd == nil || cmd.Name() != "observability" {
		t.Fatalf("observability command not registered: %v", cmd)
	}
	for _, subcommand := range []string{"login", "sync", "status"} {
		child, _, err := root.Find([]string{"observability", subcommand})
		if err != nil || child == nil || child.Name() != subcommand {
			t.Fatalf("observability %s not wired: child=%v err=%v", subcommand, child, err)
		}
	}
}
