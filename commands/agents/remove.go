package agents

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

func agentUserError(deps Deps, message string, hints ...string) error {
	if deps.ErrorWithHints != nil {
		return deps.ErrorWithHints(message, hints...)
	}
	if len(hints) == 0 {
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("%s: %s", message, hints[0])
}

func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// RemoveAgentIn removes managed repo symlinks for an agent, drops the name from
// .agentsrc.json when listed, and optionally deletes the canonical directory.
func RemoveAgentIn(deps Deps, name, projectPath string, purge bool) error {
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		return agentUserError(deps, "loading .agentsrc.json", "Run `dot-agents install --generate` or `dot-agents add .` to create the project manifest.")
	}
	projectName := rc.Project
	if projectName == "" {
		return agentUserError(deps, ".agentsrc.json has no project name set", "Run `dot-agents install --generate` or `dot-agents add .` again to repair the manifest.")
	}

	agentsHome := config.AgentsHome()
	canonicalPath := filepath.Join(agentsHome, "agents", projectName, name)
	repoAgents := filepath.Join(projectPath, ".agents", "agents", name)
	repoClaude := filepath.Join(projectPath, ".claude", "agents", name)

	inList := false
	for _, a := range rc.Agents {
		if a == name {
			inList = true
			break
		}
	}

	if !inList && !pathExists(repoAgents) && !pathExists(repoClaude) {
		return agentUserError(deps, fmt.Sprintf("agent %q is not linked in this project", name), "Run `dot-agents agents list` to inspect the managed agents in this repository.")
	}

	if err := cleanupManagedAgentRepoPath(deps, repoAgents, agentsHome, name); err != nil {
		return err
	}
	if err := cleanupManagedAgentRepoPath(deps, repoClaude, agentsHome, name); err != nil {
		return err
	}

	if inList {
		rc.Agents = removeAgentNameFromSlice(rc.Agents, name)
		if err := rc.Save(projectPath); err != nil {
			return fmt.Errorf("updating .agentsrc.json: %w", err)
		}
	}

	canonicalPurged := false
	if purge {
		var err error
		canonicalPurged, err = purgeCanonicalAgent(deps, canonicalPath, name)
		if err != nil {
			return err
		}
	}

	lines := []string{
		"Repo symlinks under .agents/agents/ and .claude/agents/ were removed when present",
	}
	if inList {
		lines = append(lines, fmt.Sprintf("Updated .agentsrc.json (%d agent(s) listed)", len(rc.Agents)))
	} else {
		lines = append(lines, ".agentsrc.json unchanged (agent was not listed)")
	}
	if purge {
		if canonicalPurged {
			lines = append(lines, fmt.Sprintf("Canonical directory removed (%s)", config.DisplayPath(canonicalPath)))
		}
	} else {
		lines = append(lines, fmt.Sprintf("Canonical left at %s (use --purge to delete)", config.DisplayPath(canonicalPath)))
	}
	ui.SuccessBox(fmt.Sprintf("Removed agent '%s' from project '%s'", name, projectName), lines...)
	return nil
}

func removeAgentNameFromSlice(list []string, name string) []string {
	out := make([]string, 0, len(list))
	for _, s := range list {
		if s != name {
			out = append(out, s)
		}
	}
	return out
}

func cleanupManagedAgentRepoPath(deps Deps, path, agentsHome, name string) error {
	_ = links.RemoveIfSymlinkUnder(path, agentsHome)
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		dest, rerr := os.Readlink(path)
		if rerr != nil {
			return rerr
		}
		return agentUserError(deps, fmt.Sprintf("refusing to remove unmanaged symlink for agent %q at %s (points to %s)", name, path, dest), "Remove the symlink manually or re-run `dot-agents agents remove --purge` after linking the agent through dot-agents.")
	}
	if fi.IsDir() {
		return agentUserError(deps, fmt.Sprintf("agent %q: %s is a real directory", name, path), "Remove or relocate the directory before running `dot-agents agents remove` again.")
	}
	return agentUserError(deps, fmt.Sprintf("agent %q: unexpected file at %s", name, path), "Remove the file or replace it with a managed symlink before retrying.")
}

// purgeCanonicalAgent deletes the shared canonical agent directory after confirmation.
// Returns true when the directory was removed by this call.
func purgeCanonicalAgent(deps Deps, canonicalPath, name string) (bool, error) {
	fi, err := os.Lstat(canonicalPath)
	if err != nil {
		if os.IsNotExist(err) {
			ui.Bullet("info", "Canonical path already absent; nothing to purge")
			return false, nil
		}
		return false, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return false, agentUserError(deps, fmt.Sprintf("cannot purge %q: canonical path %s is a symlink", name, config.DisplayPath(canonicalPath)), "Remove the symlink or restore the canonical directory before retrying.")
	}
	if !fi.IsDir() {
		return false, agentUserError(deps, fmt.Sprintf("cannot purge %q: expected a directory at %s", name, config.DisplayPath(canonicalPath)), "Restore the canonical agent directory before retrying.")
	}
	prompt := fmt.Sprintf("Permanently delete canonical agent at %s?", config.DisplayPath(canonicalPath))
	if !ui.Confirm(prompt, deps.Flags.Yes) {
		ui.Info("Purge cancelled.")
		return false, nil
	}
	if err := os.RemoveAll(canonicalPath); err != nil {
		return false, fmt.Errorf("purging canonical agent %q: %w", name, err)
	}
	return true, nil
}
