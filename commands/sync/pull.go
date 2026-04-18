package sync

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/spf13/cobra"
)

func newPullCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Pull latest changes from remote",
		RunE: func(cmd *cobra.Command, args []string) error {
			if deps.Flags.DryRun {
				return fmt.Errorf("sync pull: --dry-run is not supported (git pull would still run); omit --dry-run for this subcommand")
			}
			agentsHome := config.AgentsHome()
			out, err := exec.Command("git", "-C", agentsHome, "pull").CombinedOutput()
			fmt.Fprint(os.Stdout, string(out))
			if err != nil {
				return fmt.Errorf("git pull: %w", err)
			}
			return postPullRefresh(deps, hasGitManifests())
		},
	}
}
