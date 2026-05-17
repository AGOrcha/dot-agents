package sync

import (
	"github.com/spf13/cobra"
)

// NewSyncCmd builds the `da sync` command tree from injected dependencies.
func NewSyncCmd(deps Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Git operations on ~/.agents/",
		Long: `Wraps the most common git workflows for the shared ~/.agents store so you can
version, inspect, and distribute configuration changes without manually changing
directories first.`,
		Example: exampleBlock(
			"  da sync init",
			"  da sync status",
			"  da sync push",
		),
	}
	cmd.AddCommand(newInitCmd(deps))
	cmd.AddCommand(newCommitCmd(deps))
	cmd.AddCommand(newPullCmd(deps))
	cmd.AddCommand(newPushCmd(deps))
	cmd.AddCommand(newStatusCmd())
	cmd.AddCommand(newLogCmd())
	return cmd
}
