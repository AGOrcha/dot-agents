package agents

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

// PromoteAgentIn promotes a repo-local agent (.agents/agents/<name>/) into the
// shared agents store. The canonical location (~/.agents/agents/<project>/<name>/)
// becomes the real directory, and the repo-local path is converted to a managed
// symlink pointing at it.
func PromoteAgentIn(name, projectPath string, force bool) error {
	sourcePath := filepath.Join(projectPath, ".agents", "agents", name)

	sourceInfo, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("agent %q not found in .agents/agents/: %w", name, err)
	}

	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		return fmt.Errorf("loading .agentsrc.json: %w", err)
	}
	projectName := rc.Project
	if projectName == "" {
		return fmt.Errorf(".agentsrc.json has no project name set")
	}

	agentsHome := config.AgentsHome()
	destDir := filepath.Join(agentsHome, "agents", projectName)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("creating agents directory: %w", err)
	}
	canonicalPath := filepath.Join(destDir, name)

	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		existing, err := os.Readlink(sourcePath)
		if err != nil {
			return fmt.Errorf("reading existing symlink for agent %q: %w", name, err)
		}
		if existing != canonicalPath {
			return fmt.Errorf("agent %q is already a symlink but points to %q, not the canonical path %q", name, existing, canonicalPath)
		}
	} else {
		if _, err := os.Stat(filepath.Join(sourcePath, "AGENT.md")); err != nil {
			return fmt.Errorf("agent %q not found in .agents/agents/ (expected AGENT.md at %s/AGENT.md)", name, sourcePath)
		}
		if fi, err := os.Lstat(canonicalPath); err == nil {
			if fi.Mode()&os.ModeSymlink != 0 {
				if err := os.Remove(canonicalPath); err != nil {
					return fmt.Errorf("removing stale canonical symlink for agent %q: %w", name, err)
				}
			} else if fi.IsDir() {
				if !force {
					return fmt.Errorf("agent %q already exists at canonical path %s as a real directory; use --force to overwrite", name, canonicalPath)
				}
				if err := os.RemoveAll(canonicalPath); err != nil {
					return fmt.Errorf("removing existing canonical directory for agent %q: %w", name, err)
				}
			} else {
				return fmt.Errorf("agent %q already exists at canonical path %s; remove the file and retry", name, canonicalPath)
			}
		}
		if err := copyAgentDir(sourcePath, canonicalPath); err != nil {
			return fmt.Errorf("copying agent %q to canonical path: %w", name, err)
		}
		if err := os.RemoveAll(sourcePath); err != nil {
			return fmt.Errorf("removing repo-local agent directory for %q: %w", name, err)
		}
		if err := os.Symlink(canonicalPath, sourcePath); err != nil {
			return fmt.Errorf("creating repo-local managed symlink for agent %q: %w", name, err)
		}
	}

	rc.Agents = config.AppendUnique(rc.Agents, name)
	if err := rc.Save(projectPath); err != nil {
		return fmt.Errorf("updating .agentsrc.json: %w", err)
	}

	intents, err := platform.BuildSharedAgentMirrorIntents(projectName, filepath.Join(".claude", "agents"))
	if err != nil {
		ui.Bullet("warn", "building agent mirror intents: "+err.Error())
	} else {
		plan, perr := platform.BuildResourcePlan(intents)
		if perr != nil {
			ui.Bullet("warn", "agent mirror plan: "+perr.Error())
		} else if err := plan.Execute(projectPath, config.AgentsHome()); err != nil {
			ui.Bullet("warn", "platform agent symlink refresh failed: "+err.Error())
		}
	}

	ui.SuccessBox(
		fmt.Sprintf("Promoted agent '%s' for project '%s'", name, projectName),
		fmt.Sprintf("Registered in .agentsrc.json (%d agent(s) total)", len(rc.Agents)),
		"Run 'dot-agents refresh' to sync across all platforms",
	)
	return nil
}

// copyAgentDir recursively copies the directory tree at src to dst, preserving
// file modes. Symlinks in the source tree are skipped.
func copyAgentDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		destPath := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(destPath, 0755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		return os.WriteFile(destPath, data, info.Mode())
	})
}
