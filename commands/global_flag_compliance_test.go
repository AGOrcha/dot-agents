package commands

import (
	"strings"
	"testing"
)

func TestSyncPullCmd_RejectsGlobalDryRun(t *testing.T) {
	prev := Flags.DryRun
	Flags.DryRun = true
	defer func() { Flags.DryRun = prev }()

	cmd := newSyncPullCmd()
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error when Flags.DryRun is set")
	}
	if !strings.Contains(err.Error(), "sync pull") || !strings.Contains(err.Error(), "dry-run") {
		t.Fatalf("unexpected error: %v", err)
	}
}
