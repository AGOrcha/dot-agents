package sync

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPullRejectsDryRun(t *testing.T) {
	t.Parallel()
	deps := Deps{
		Flags: GlobalFlags{DryRun: true},
		RunRefresh: func(string) error {
			return nil
		},
	}
	root := NewSyncCmd(deps)
	var pull *cobra.Command
	for _, c := range root.Commands() {
		if c.Name() == "pull" {
			pull = c
			break
		}
	}
	if pull == nil {
		t.Fatal("pull subcommand not found")
	}
	err := pull.RunE(pull, nil)
	if err == nil {
		t.Fatal("expected error for dry-run pull")
	}
	if !strings.Contains(err.Error(), "dry-run") {
		t.Fatalf("error should mention dry-run: %v", err)
	}
}
