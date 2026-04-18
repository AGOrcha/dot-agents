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

func newInitCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize git repo in ~/.agents/",
		Example: exampleBlock(
			"  dot-agents sync init",
			"  dot-agents sync init --dry-run",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsHome := config.AgentsHome()
			gitDir := agentsHome + "/.git"

			if _, err := os.Stat(gitDir); err == nil {
				ui.Info("~/.agents/ is already a git repository.")
				fmt.Fprintln(os.Stdout)

				out, _ := exec.Command("git", "-C", agentsHome, "remote", "-v").Output()
				remote := strings.TrimSpace(string(out))
				if remote != "" {
					ui.Info("Remote configured:")
					lines := strings.Split(remote, "\n")
					for i, l := range lines {
						if i >= 2 {
							break
						}
						fmt.Fprintf(os.Stdout, "  %s\n", l)
					}
				} else {
					fmt.Fprintln(os.Stdout, "Next steps:")
					fmt.Fprintln(os.Stdout, "  1. Create a private repository on GitHub/GitLab")
					fmt.Fprintln(os.Stdout, "  2. Add the remote:")
					fmt.Fprintf(os.Stdout, "       cd %s\n", agentsHome)
					fmt.Fprintln(os.Stdout, "       git remote add origin git@github.com:YOU/agents-config.git")
					fmt.Fprintln(os.Stdout, "  3. Push your config:")
					fmt.Fprintln(os.Stdout, "       dot-agents sync push")
				}
				return nil
			}

			if deps.Flags.DryRun {
				ui.DryRun("git init " + agentsHome)
				ui.DryRun("create .gitignore")
				ui.DryRun("git add .")
				ui.DryRun("git commit -m 'Initial commit'")
				return nil
			}

			out, err := exec.Command("git", "-C", agentsHome, "init").CombinedOutput()
			if err != nil {
				return fmt.Errorf("git init: %w\n%s", err, out)
			}

			gitignorePath := agentsHome + "/.gitignore"
			if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
				_ = os.WriteFile(gitignorePath, []byte("local/\n*.dot-agents-backup\n"), 0644)
			}

			exec.Command("git", "-C", agentsHome, "add", ".").Run()
			exec.Command("git", "-C", agentsHome, "commit", "-m", "Initial commit").Run()

			ui.Success("Initialized git repository in ~/.agents/")
			fmt.Fprintln(os.Stdout)
			fmt.Fprintln(os.Stdout, "Next steps:")
			fmt.Fprintln(os.Stdout, "  1. Create a private repository on GitHub/GitLab")
			fmt.Fprintln(os.Stdout, "  2. Add the remote:")
			fmt.Fprintf(os.Stdout, "       cd %s\n", agentsHome)
			fmt.Fprintln(os.Stdout, "       git remote add origin git@github.com:YOU/agents-config.git")
			fmt.Fprintln(os.Stdout, "  3. Push your config:")
			fmt.Fprintln(os.Stdout, "       dot-agents sync push")
			return nil
		},
	}
}
