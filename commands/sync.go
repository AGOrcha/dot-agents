package commands

import (
	"github.com/NikashPrakash/dot-agents/commands/sync"
	"github.com/spf13/cobra"
)

func syncDeps() sync.Deps {
	return sync.Deps{
		Flags: sync.GlobalFlags{
			DryRun: Flags.DryRun,
			Yes:    Flags.Yes,
			Force:  Flags.Force,
		},
		RunRefresh: runRefresh,
	}
}

func NewSyncCmd() *cobra.Command {
	return sync.NewSyncCmd(syncDeps())
}

// newSyncPullCmd returns only the pull subcommand (used by global flag compliance tests).
func newSyncPullCmd() *cobra.Command {
	root := sync.NewSyncCmd(syncDeps())
	for _, c := range root.Commands() {
		if c.Name() == "pull" {
			return c
		}
	}
	panic("sync: pull subcommand missing")
}
