package sync

import (
	"testing"
)

func TestNewSyncCmd_ConstructsWithoutPanic(t *testing.T) {
	deps := Deps{
		Flags:      GlobalFlags{},
		RunRefresh: func(projectFilter string) error { return nil },
	}
	cmd := NewSyncCmd(deps)
	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
	if cmd.Use != "sync" {
		t.Errorf("Use = %q, want %q", cmd.Use, "sync")
	}
}

func TestNewSyncCmd_HasExpectedSubcommands(t *testing.T) {
	deps := Deps{
		Flags:      GlobalFlags{},
		RunRefresh: func(projectFilter string) error { return nil },
	}
	cmd := NewSyncCmd(deps)

	expected := map[string]bool{
		"init":   false,
		"commit": false,
		"pull":   false,
		"push":   false,
		"status": false,
		"log":    false,
	}
	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}
	for name, found := range expected {
		if !found {
			t.Errorf("expected subcommand %q not found", name)
		}
	}
}

func TestPrintStatusSummary_NoChanges(t *testing.T) {
	// Smoke test — just verify it doesn't panic with zeros.
	printStatusSummary(0, 0, 0)
}

func TestPrintStatusSummary_WithChanges(t *testing.T) {
	// Smoke test — verify it doesn't panic with non-zero counts.
	printStatusSummary(2, 3, 1)
}
