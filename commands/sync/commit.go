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

func newCommitCmd(deps Deps) *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "commit [message]",
		Short: "Commit all changes in ~/.agents/",
		Example: exampleBlock(
			"  da sync commit",
			"  da sync commit \"Update Codex rules\"",
			"  da sync commit -m \"Refresh shared hooks\"",
		),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsHome := config.AgentsHome()
			resolved := resolveCommitMessage(agentsHome, message, args)
			return runSyncCommit(deps, agentsHome, resolved)
		},
	}
	cmd.Flags().StringVarP(&message, "message", "m", "", "Commit message")
	return cmd
}

// resolveCommitMessage returns the commit message to use for `da sync
// commit`: the explicit flag/positional-args message if provided, otherwise
// a default message. When no message was given, it also makes sure changes
// are staged (so a `git diff --cached` prior to commit sees them) before
// falling back to the default.
func resolveCommitMessage(agentsHome, message string, args []string) string {
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
	return message
}

// runSyncCommit stages and commits all changes under agentsHome. It surfaces
// git add / git commit failures as errors; the sole non-fatal outcome is a
// git commit failure whose output reports "nothing to commit".
func runSyncCommit(deps Deps, agentsHome, message string) error {
	if deps.Flags.DryRun {
		ui.DryRun("git add -A")
		ui.DryRun(fmt.Sprintf("git commit -m %q", message))
		return nil
	}

	if addOut, err := execabs.Command("git", "-C", agentsHome, "add", "-A").CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, strings.TrimSpace(string(addOut)))
	}
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
}
