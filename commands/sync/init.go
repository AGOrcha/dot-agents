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

func newInitCmd(deps Deps) *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize git repo in ~/.agents/",
		Example: exampleBlock(
			"  da sync init",
			"  da sync init --dry-run",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSyncInit(deps)
		},
	}
}

// runSyncInit dispatches to the existing-repo or fresh-init branch based on
// the presence of ~/.agents/.git.
func runSyncInit(deps Deps) error {
	agentsHome := config.AgentsHome()
	if _, err := os.Stat(agentsHome + "/.git"); err == nil {
		return reportExistingSyncRepo(agentsHome)
	}
	if deps.Flags.DryRun {
		ui.DryRun("git init " + agentsHome)
		ui.DryRun("create .gitignore")
		ui.DryRun("git add .")
		ui.DryRun("git commit -m 'Initial commit'")
		return nil
	}
	return initSyncRepo(agentsHome)
}

// reportExistingSyncRepo prints either the configured remote (when present)
// or the next-steps recipe for adding a remote and pushing.
func reportExistingSyncRepo(agentsHome string) error {
	ui.Info("~/.agents/ is already a git repository.")
	fmt.Fprintln(os.Stdout)

	out, _ := execabs.Command("git", "-C", agentsHome, "remote", "-v").Output()
	remote := strings.TrimSpace(string(out))
	if remote == "" {
		printSyncNextSteps(agentsHome)
		return nil
	}
	ui.Info("Remote configured:")
	lines := strings.Split(remote, "\n")
	for i, l := range lines {
		if i >= 2 {
			break
		}
		fmt.Fprintf(os.Stdout, "  %s\n", l)
	}
	return nil
}

// initSyncRepo runs `git init`, drops a default .gitignore, performs an
// initial add+commit, and prints the next-steps recipe.
func initSyncRepo(agentsHome string) error {
	out, err := execabs.Command("git", "-C", agentsHome, "init").CombinedOutput()
	if err != nil {
		return fmt.Errorf("git init: %w\n%s", err, out)
	}

	gitignorePath := agentsHome + "/.gitignore"
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		_ = os.WriteFile(gitignorePath, []byte("local/\n*.dot-agents-backup\n"), 0644)
	}

	execabs.Command("git", "-C", agentsHome, "add", ".").Run()
	execabs.Command("git", "-C", agentsHome, "commit", "-m", "Initial commit").Run()

	ui.Success("Initialized git repository in ~/.agents/")
	fmt.Fprintln(os.Stdout)
	printSyncNextSteps(agentsHome)
	return nil
}

// printSyncNextSteps writes the canonical "create remote and push" recipe.
func printSyncNextSteps(agentsHome string) {
	fmt.Fprintln(os.Stdout, "Next steps:")
	fmt.Fprintln(os.Stdout, "  1. Create a private repository on GitHub/GitLab")
	fmt.Fprintln(os.Stdout, "  2. Add the remote:")
	fmt.Fprintf(os.Stdout, "       cd %s\n", agentsHome)
	fmt.Fprintln(os.Stdout, "       git remote add origin git@github.com:YOU/agents-config.git")
	fmt.Fprintln(os.Stdout, "  3. Push your config:")
	fmt.Fprintln(os.Stdout, "       da sync push")
}
