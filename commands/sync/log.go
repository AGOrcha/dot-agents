package sync

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/spf13/cobra"
)

func newLogCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "log",
		Short: "Show recent commit history in ~/.agents/",
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsHome := config.AgentsHome()
			out, err := exec.Command("git", "-C", agentsHome, "log", "--oneline", "--decorate", "-n", "10").CombinedOutput()
			fmt.Fprint(os.Stdout, string(out))
			if err != nil {
				return fmt.Errorf("git log: %w", err)
			}
			return nil
		},
	}
}
