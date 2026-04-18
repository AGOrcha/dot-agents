package sync

import (
	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show git status of ~/.agents/",
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsHome := config.AgentsHome()

			ui.Header("dot-agents sync status")
			printBranchStatus(agentsHome)
			hasRemote := printRemoteStatus(agentsHome)
			printAheadBehind(agentsHome, hasRemote)
			staged, unstaged, untracked := countPorcelainStatus(agentsHome)
			printStatusSummary(staged, unstaged, untracked)
			return nil
		},
	}
}
