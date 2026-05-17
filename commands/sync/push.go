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

func newPushCmd(deps Deps) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Commit and push changes to remote",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSyncPush(deps, message)
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "Commit message")
	return cmd
}

// runSyncPush is the body of the sync push command, factored out so each
// phase (pending log, dry-run, commit, confirm, push) reads as a guard
// clause rather than nested branches.
func runSyncPush(deps Deps, message string) error {
	agentsHome := config.AgentsHome()
	if message == "" {
		message = "Update ~/.agents/ configuration"
	}

	printPendingPushCommits(agentsHome)

	if deps.Flags.DryRun {
		ui.DryRun("git add -A")
		ui.DryRun(fmt.Sprintf("git commit -m %q", message))
		ui.DryRun("git push")
		return nil
	}

	stageAndCommit(agentsHome, message)

	if !deps.Flags.Yes && !deps.Flags.Force {
		if !ui.Confirm("Push to remote?", false) {
			ui.Info("Push cancelled.")
			return nil
		}
	}

	out, err := execabs.Command("git", "-C", agentsHome, "push").CombinedOutput()
	fmt.Fprint(os.Stdout, string(out))
	if err != nil {
		return fmt.Errorf("git push: %w", err)
	}
	return nil
}

// printPendingPushCommits prints the "Commits to push" section listing
// commits ahead of origin/HEAD, when any.
func printPendingPushCommits(agentsHome string) {
	pendingOut, _ := execabs.Command("git", "-C", agentsHome, "log", "--oneline", "origin/HEAD..HEAD").Output()
	pending := strings.TrimSpace(string(pendingOut))
	if pending == "" {
		return
	}
	ui.Section("Commits to push")
	for _, line := range strings.Split(pending, "\n") {
		fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Dim, line, ui.Reset)
	}
}

// stageAndCommit runs `git add -A` and `git commit -m <message>`, printing
// commit output unless it is the noisy "nothing to commit" status.
func stageAndCommit(agentsHome, message string) {
	execabs.Command("git", "-C", agentsHome, "add", "-A").Run()
	commitOut, _ := execabs.Command("git", "-C", agentsHome, "commit", "-m", message).CombinedOutput()
	commitStr := strings.TrimSpace(string(commitOut))
	if commitStr != "" && !strings.Contains(commitStr, "nothing to commit") {
		fmt.Fprintln(os.Stdout, commitStr)
	}
}
