package sync

import (
	"fmt"
	"os"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/sys/execabs"
)

func newPushCmd(deps Deps) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Commit and push changes to remote",
		Example: "  da sync push\n" +
			"  da sync push --message \"sync agent rules\"",
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

	if err := stageAndCommit(agentsHome, message); err != nil {
		return err
	}

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
// commit output unless it is the noisy "nothing to commit" status. Only the
// "nothing to commit" sentinel is non-fatal — any other add/commit failure
// aborts before the confirm/push step so `da sync push` never reports
// success while silently failing to commit the pending changes.
func stageAndCommit(agentsHome, message string) error {
	if addOut, err := execabs.Command("git", "-C", agentsHome, "add", "-A").CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, strings.TrimSpace(string(addOut)))
	}
	commitOut, err := execabs.Command("git", "-C", agentsHome, "commit", "-m", message).CombinedOutput()
	commitStr := strings.TrimSpace(string(commitOut))
	if err != nil {
		if strings.Contains(commitStr, "nothing to commit") {
			return nil
		}
		return fmt.Errorf("git commit: %w\n%s", err, commitStr)
	}
	if commitStr != "" {
		fmt.Fprintln(os.Stdout, commitStr)
	}
	return nil
}
