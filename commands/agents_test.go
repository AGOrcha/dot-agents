package commands

import "testing"

// Lifecycle tests for import/promote/remove live in commands/agents/agents_test.go.

func TestNewAgentsCmdRegistersSubcommands(t *testing.T) {
	cmd := NewAgentsCmd()
	if cmd == nil {
		t.Fatal("NewAgentsCmd returned nil")
	}
	if cmd.Use != "agents" {
		t.Fatalf("Use = %q", cmd.Use)
	}
	n := len(cmd.Commands())
	if n < 5 {
		t.Fatalf("expected at least 5 subcommands, got %d", n)
	}
}
