package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Git operations on ~/.agents/",
	}
	cmd.AddCommand(newSyncInitCmd())
	cmd.AddCommand(newSyncCommitCmd())
	cmd.AddCommand(newSyncPullCmd())
	cmd.AddCommand(newSyncPushCmd())
	cmd.AddCommand(newSyncStatusCmd())
	cmd.AddCommand(newSyncLogCmd())
	return cmd
}

func newSyncInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize git repo in ~/.agents/",
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsHome := config.AgentsHome()
			gitDir := agentsHome + "/.git"

			if _, err := os.Stat(gitDir); err == nil {
				ui.Info("~/.agents/ is already a git repository.")
				fmt.Fprintln(os.Stdout)

				// Show remote if configured, otherwise show setup steps
				out, _ := exec.Command("git", "-C", agentsHome, "remote", "-v").Output()
				remote := strings.TrimSpace(string(out))
				if remote != "" {
					ui.Info("Remote configured:")
					// Print first two lines (fetch + push)
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

			if Flags.DryRun {
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

			// Create .gitignore if missing
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

func newSyncCommitCmd() *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "commit [message]",
		Short: "Commit all changes in ~/.agents/",
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsHome := config.AgentsHome()

			// Auto-generate message from diff stats if not provided
			if message == "" && len(args) > 0 {
				message = strings.Join(args, " ")
			}
			if message == "" {
				// Build a message from modified/added/deleted counts
				out, _ := exec.Command("git", "-C", agentsHome, "diff", "--cached", "--stat", "HEAD").Output()
				if len(out) == 0 {
					// Nothing staged yet — stage first
					exec.Command("git", "-C", agentsHome, "add", "-A").Run()
					out, _ = exec.Command("git", "-C", agentsHome, "diff", "--cached", "--stat", "HEAD").Output()
				}
				message = "Update ~/.agents/ configuration"
				_ = out
			}

			if Flags.DryRun {
				ui.DryRun("git add -A")
				ui.DryRun(fmt.Sprintf("git commit -m %q", message))
				return nil
			}

			exec.Command("git", "-C", agentsHome, "add", "-A").Run()
			out, err := exec.Command("git", "-C", agentsHome, "commit", "-m", message).CombinedOutput()
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

func newSyncPullCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pull",
		Short: "Pull latest changes from remote",
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsHome := config.AgentsHome()
			out, err := exec.Command("git", "-C", agentsHome, "pull").CombinedOutput()
			fmt.Fprint(os.Stdout, string(out))
			if err != nil {
				return fmt.Errorf("git pull: %w", err)
			}

			// Check if any managed projects have manifests with git sources
			hasManifests := false
			if cfg, err := config.Load(); err == nil {
				for _, name := range cfg.ListProjects() {
					if path := cfg.GetProjectPath(name); path != "" {
						if rc, err := config.LoadAgentsRC(path); err == nil {
							for _, src := range rc.Sources {
								if src.Type == "git" {
									hasManifests = true
									break
								}
							}
							_ = rc
						}
					}
					if hasManifests {
						break
					}
				}
			}

			// Offer to refresh managed projects so pulled MCP/rule changes take effect.
			if Flags.Yes || ui.Confirm("Refresh managed projects with pulled changes?", true) {
				fmt.Fprintln(os.Stdout)
				if err := runRefresh(""); err != nil {
					return err
				}
				if hasManifests {
					fmt.Fprintf(os.Stdout, "\n  %sProjects with git sources: run 'dot-agents install' in each to re-resolve resources.%s\n", ui.Dim, ui.Reset)
				}
				return nil
			}
			fmt.Fprintf(os.Stdout, "\n  %sRun 'dot-agents refresh' to apply changes to managed projects.%s\n", ui.Dim, ui.Reset)
			if hasManifests {
				fmt.Fprintf(os.Stdout, "  %sProjects with git sources: run 'dot-agents install' in each to re-resolve resources.%s\n", ui.Dim, ui.Reset)
			}
			return nil
		},
	}
}

func newSyncPushCmd() *cobra.Command {
	var message string
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Commit and push changes to remote",
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsHome := config.AgentsHome()
			if message == "" {
				message = "Update ~/.agents/ configuration"
			}

			// Show commits to be pushed
			pendingOut, _ := exec.Command("git", "-C", agentsHome, "log", "--oneline", "origin/HEAD..HEAD").Output()
			pending := strings.TrimSpace(string(pendingOut))

			if pending != "" {
				ui.Section("Commits to push")
				for _, line := range strings.Split(pending, "\n") {
					fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Dim, line, ui.Reset)
				}
			}

			if Flags.DryRun {
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

			if !Flags.Yes && !Flags.Force {
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

func newSyncStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show git status of ~/.agents/",
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsHome := config.AgentsHome()

			ui.Header("dot-agents sync status")

			// Branch
			branch, _ := exec.Command("git", "-C", agentsHome, "rev-parse", "--abbrev-ref", "HEAD").Output()
			branchStr := strings.TrimSpace(string(branch))
			if branchStr != "" {
				fmt.Fprintf(os.Stdout, "  Branch:  %s%s%s\n", ui.Bold, branchStr, ui.Reset)
			}

			// Remote
			remoteOut, _ := exec.Command("git", "-C", agentsHome, "remote", "get-url", "origin").Output()
			remoteStr := strings.TrimSpace(string(remoteOut))
			hasRemote := remoteStr != ""
			if hasRemote {
				fmt.Fprintf(os.Stdout, "  Remote:  %s%s%s\n", ui.Dim, remoteStr, ui.Reset)
			} else {
				fmt.Fprintf(os.Stdout, "  Remote:  %s(none)%s\n", ui.Dim, ui.Reset)
			}

			// Ahead/behind — only when remote exists
			if hasRemote {
				aheadBehind, _ := exec.Command("git", "-C", agentsHome, "rev-list", "--count", "--left-right", "origin/HEAD...HEAD").Output()
				ab := strings.Fields(strings.TrimSpace(string(aheadBehind)))
				if len(ab) == 2 {
					behind, ahead := ab[0], ab[1]
					aheadStr := ahead
					if ahead != "0" {
						aheadStr = ui.Green + ahead + ui.Reset
					}
					fmt.Fprintf(os.Stdout, "  Ahead:   %s  Behind: %s\n", aheadStr, behind)
				}
			}

			// Porcelain counts
			porcelain, _ := exec.Command("git", "-C", agentsHome, "status", "--porcelain").Output()
			staged, unstaged, untracked := 0, 0, 0
			for _, line := range strings.Split(string(porcelain), "\n") {
				if len(line) < 2 {
					continue
				}
				x, y := line[0], line[1]
				if x != ' ' && x != '?' {
					staged++
				}
				if y == 'M' || y == 'D' {
					unstaged++
				}
				if x == '?' && y == '?' {
					untracked++
				}
			}
			fmt.Fprintln(os.Stdout)

			stagedStr := fmt.Sprintf("%d", staged)
			if staged > 0 {
				stagedStr = ui.Yellow + stagedStr + ui.Reset
			}
			untrackedStr := fmt.Sprintf("%s%d%s", ui.Dim, untracked, ui.Reset)

			fmt.Fprintf(os.Stdout, "  Staged:    %s\n", stagedStr)
			fmt.Fprintf(os.Stdout, "  Unstaged:  %d\n", unstaged)
			fmt.Fprintf(os.Stdout, "  Untracked: %s\n", untrackedStr)
			fmt.Fprintln(os.Stdout)

			// Summary line
			totalChanges := staged + unstaged + untracked
			if totalChanges == 0 {
				ui.Success("No changes — working tree clean")
			} else {
				ui.Warn(fmt.Sprintf("%d change(s) pending commit", totalChanges))
			}
			fmt.Fprintln(os.Stdout)

			return nil
		},
	}
}

func newSyncLogCmd() *cobra.Command {
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
