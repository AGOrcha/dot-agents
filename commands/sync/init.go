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
		// Upgrade an already-initialized home's machine-local sync boundary so
		// the binding table + caches are excluded even on a home that ran
		// `sync init` before this fix landed (defects 2 & 5, R7).
		if !deps.Flags.DryRun {
			if err := ensureSyncGitignore(agentsHome + "/.gitignore"); err != nil {
				return fmt.Errorf("writing .gitignore: %w", err)
			}
			// A .gitignore only stops NEW tracking — a home that already
			// committed local/ or cache/ would keep pushing them. Untrack them
			// (without deleting the working-tree files) so machine-local state
			// stops syncing on an already-initialized home.
			untrackMachineLocalState(agentsHome)
		}
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
	if err := ensureSyncGitignore(gitignorePath); err != nil {
		return fmt.Errorf("writing .gitignore: %w", err)
	}

	if addOut, err := execabs.Command("git", "-C", agentsHome, "add", ".").CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, strings.TrimSpace(string(addOut)))
	}
	if commitOut, err := execabs.Command("git", "-C", agentsHome, "commit", "-m", "Initial commit").CombinedOutput(); err != nil {
		msg := string(commitOut)
		if strings.Contains(msg, "user.email") || strings.Contains(msg, "user.name") {
			return fmt.Errorf("git commit failed (likely missing git user config). Run `git config --global user.email \"you@example.com\"` and `git config --global user.name \"Your Name\"` then re-run `da sync init`: %w", err)
		}
		return fmt.Errorf("git commit: %w (output: %s)", err, strings.TrimSpace(msg))
	}

	ui.Success("Initialized git repository in ~/.agents/")
	fmt.Fprintln(os.Stdout)
	printSyncNextSteps(agentsHome)
	return nil
}

// syncGitignoreEntries are the machine-local sync-boundary directories that must
// never enter the synced ~/.agents tree (home-config defects 2 & 5, R7):
//   - local/ holds the machine-local binding table (id → absolute path).
//   - cache/ holds the tier-1 config cache and tier-2 packages cache.
//
// *.dot-agents-backup keeps the legacy backup-file exclusion.
var syncGitignoreEntries = []string{"local/", "cache/", "*.dot-agents-backup"}

// ensureSyncGitignore guarantees every machine-local sync-boundary entry is
// present in the home .gitignore. A missing file is created with the full set;
// an existing file gains only the entries it lacks (so an already-initialized
// home is upgraded to also exclude cache/ without clobbering user lines). This
// is the mechanism half of the machine-local classification — without it the
// binding table and caches would travel to machine B.
func ensureSyncGitignore(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(path, []byte(strings.Join(syncGitignoreEntries, "\n")+"\n"), 0644)
		}
		return err
	}
	present := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		present[strings.TrimSpace(line)] = true
	}
	var missing []string
	for _, e := range syncGitignoreEntries {
		if !present[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	content := string(data)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += strings.Join(missing, "\n") + "\n"
	return os.WriteFile(path, []byte(content), 0644)
}

// untrackMachineLocalState removes already-tracked machine-local paths (the
// binding table under local/ and the materialized caches under cache/) from the
// git index WITHOUT deleting the working-tree files. `--ignore-unmatch` keeps it
// a no-op when nothing is tracked, so it is safe to run on every `sync init`.
// This is the in-place repair counterpart to ensureSyncGitignore: the gitignore
// stops new tracking, this stops the already-tracked ones from continuing to
// push (defects 2 & 5, R7).
func untrackMachineLocalState(agentsHome string) {
	_ = execabs.Command("git", "-C", agentsHome, "rm", "--cached", "-r",
		"--ignore-unmatch", "--quiet", "local/", "cache/").Run()
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
