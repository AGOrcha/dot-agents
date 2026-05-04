package sync

import (
	"fmt"
	"os"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sys/execabs"
)

func newCommitCmd(deps Deps) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "commit [message]",
		Short: "Commit all changes in ~/.agents/",
		Example: exampleBlock(
			"  dot-agents sync commit",
			"  dot-agents sync commit \"Update Codex rules\"",
			"  dot-agents sync commit -m \"Refresh shared hooks\"",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsHome := config.AgentsHome()

			if message == "" && len(args) > 0 {
				message = strings.Join(args, " ")
			}
			if message == "" {
				out, _ := execabs.Command("git", "-C", agentsHome, "diff", "--cached", "--stat", "HEAD").Output()
				if len(out) == 0 {
					execabs.Command("git", "-C", agentsHome, "add", "-A").Run()
					out, _ = execabs.Command("git", "-C", agentsHome, "diff", "--cached", "--stat", "HEAD").Output()
				}
				message = "Update ~/.agents/ configuration"
				_ = out
			}

			if deps.Flags.DryRun {
				ui.DryRun("git add -A")
				ui.DryRun(fmt.Sprintf("git commit -m %q", message))
				return nil
			}

			execabs.Command("git", "-C", agentsHome, "add", "-A").Run()
			out, err := execabs.Command("git", "-C", agentsHome, "commit", "-m", message).CombinedOutput()
			output := strings.TrimSpace(string(out))
			if err != nil {
				if strings.Contains(output, "nothing to commit") {
					ui.Info("Nothing to commit, working tree clean.")
					return nil
				}
				return fmt.Errorf("git commit: %w\n%s", err, output)
			}
			fmt.Fprintln(os.Stdout, output)
			return nil
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "Commit message")
	return cmd
}
