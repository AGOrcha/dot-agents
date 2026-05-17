package commands

import (
	"testing"
)

func TestNewSessionCmd_ConstructsWithoutPanic(t *testing.T) {
	cmd := NewSessionCmd()
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Use != "session" {
		t.Errorf("Use = %q, want %q", cmd.Use, "session")
	}
}

func TestNewSessionCmd_HasStatsSubcommand(t *testing.T) {
	cmd := NewSessionCmd()
	found := false
	for _, sub := range cmd.Commands() {
		if sub.Name() == "stats" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'stats' subcommand under session")
	}
}
