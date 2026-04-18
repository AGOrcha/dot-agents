package sync

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func newPushCmd(deps Deps) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Commit and push changes to remote",
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsHome := config.AgentsHome()
			if message == "" {
				message = "Update ~/.agents/ configuration"
			}

			pendingOut, _ := exec.Command("git", "-C", agentsHome, "log", "--oneline", "origin/HEAD..HEAD").Output()
			pending := strings.TrimSpace(string(pendingOut))

			if pending != "" {
				ui.Section("Commits to push")
				for _, line := range strings.Split(pending, "\n") {
					fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Dim, line, ui.Reset)
				}
			}

			if deps.Flags.DryRun {
				ui.DryRun("git add -A")
				ui.DryRun(fmt.Sprintf("git commit -m %q", message))
				ui.DryRun("git push")
				return nil
			}

			exec.Command("git", "-C", agentsHome, "add", "-A").Run()
			commitOut, _ := exec.Command("git", "-C", agentsHome, "commit", "-m", message).CombinedOutput()
			commitStr := strings.TrimSpace(string(commitOut))
			if commitStr != "" && !strings.Contains(commitStr, "nothing to commit") {
				fmt.Fprintln(os.Stdout, commitStr)
			}

			if !deps.Flags.Yes && !deps.Flags.Force {
				if !ui.Confirm("Push to remote?", false) {
					ui.Info("Push cancelled.")
					return nil
				}
			}

			out, err := exec.Command("git", "-C", agentsHome, "push").CombinedOutput()
			fmt.Fprint(os.Stdout, string(out))
			if err != nil {
				return fmt.Errorf("git push: %w", err)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "Commit message")
	return cmd
}
