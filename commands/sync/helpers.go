package sync

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

func hasGitManifests() bool {
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	for _, name := range cfg.ListProjects() {
		path := cfg.GetProjectPath(name)
		if path == "" {
			continue
		}
		rc, err := config.LoadAgentsRC(path)
		if err != nil {
			continue
		}
		for _, src := range rc.Sources {
			if src.Type == "git" {
				return true
			}
		}
	}
	return false
}

func printGitSourcesHint() {
	fmt.Fprintf(os.Stdout, "  %sProjects with git sources: run 'dot-agents install' in each to re-resolve resources.%s\n", ui.Dim, ui.Reset)
}

func postPullRefresh(deps Deps, hasManifests bool) error {
	if deps.Flags.Yes || ui.Confirm("Refresh managed projects with pulled changes?", true) {
		fmt.Fprintln(os.Stdout)
		if err := deps.RunRefresh(""); err != nil {
			return err
		}
		if hasManifests {
			fmt.Fprintln(os.Stdout)
			printGitSourcesHint()
		}
		return nil
	}
	fmt.Fprintf(os.Stdout, "\n  %sRun 'dot-agents refresh' to apply changes to managed projects.%s\n", ui.Dim, ui.Reset)
	if hasManifests {
		printGitSourcesHint()
	}
	return nil
}

func printBranchStatus(agentsHome string) {
	branch, _ := exec.Command("git", "-C", agentsHome, "rev-parse", "--abbrev-ref", "HEAD").Output()
	branchStr := strings.TrimSpace(string(branch))
	if branchStr != "" {
		fmt.Fprintf(os.Stdout, "  Branch:  %s%s%s\n", ui.Bold, branchStr, ui.Reset)
	}
}

func printRemoteStatus(agentsHome string) bool {
	remoteOut, _ := exec.Command("git", "-C", agentsHome, "remote", "get-url", "origin").Output()
	remoteStr := strings.TrimSpace(string(remoteOut))
	hasRemote := remoteStr != ""
	if hasRemote {
		fmt.Fprintf(os.Stdout, "  Remote:  %s%s%s\n", ui.Dim, remoteStr, ui.Reset)
	} else {
		fmt.Fprintf(os.Stdout, "  Remote:  %s(none)%s\n", ui.Dim, ui.Reset)
	}
	return hasRemote
}

func printAheadBehind(agentsHome string, hasRemote bool) {
	if !hasRemote {
		return
	}
	aheadBehind, _ := exec.Command("git", "-C", agentsHome, "rev-list", "--count", "--left-right", "origin/HEAD...HEAD").Output()
	ab := strings.Fields(strings.TrimSpace(string(aheadBehind)))
	if len(ab) != 2 {
		return
	}
	behind, ahead := ab[0], ab[1]
	aheadStr := ahead
	if ahead != "0" {
		aheadStr = ui.Green + ahead + ui.Reset
	}
	fmt.Fprintf(os.Stdout, "  Ahead:   %s  Behind: %s\n", aheadStr, behind)
}

// CountPorcelainLines parses `git status --porcelain` output (including synthetic fixtures in tests).
func CountPorcelainLines(porcelain string) (staged, unstaged, untracked int) {
	for _, line := range strings.Split(porcelain, "\n") {
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
	return staged, unstaged, untracked
}

func countPorcelainStatus(agentsHome string) (int, int, int) {
	porcelain, _ := exec.Command("git", "-C", agentsHome, "status", "--porcelain").Output()
	return CountPorcelainLines(string(porcelain))
}

func printStatusSummary(staged, unstaged, untracked int) {
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

	totalChanges := staged + unstaged + untracked
	if totalChanges == 0 {
		ui.Success("No changes — working tree clean")
	} else {
		ui.Warn(fmt.Sprintf("%d change(s) pending commit", totalChanges))
	}
	fmt.Fprintln(os.Stdout)
}
