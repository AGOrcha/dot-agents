package skills

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

// PromoteSkillIn promotes a repo-local skill (.agents/skills/<name>/) into the
// shared agents store. The canonical location (~/.agents/skills/<project>/<name>/)
// becomes the real directory, and the repo-local path is converted to a managed
// symlink pointing at it. This prevents circular symlinks when platform mirror
// refresh later targets .agents/skills/ inside the repo.
func PromoteSkillIn(name, projectPath string) error {
	sourcePath := filepath.Join(projectPath, ".agents", "skills", name)

	sourceInfo, err := os.Lstat(sourcePath)
	if err != nil {
		return fmt.Errorf("skill %q not found in .agents/skills/: %w", name, err)
	}

	// Load project manifest for project name.
	rc, err := config.LoadAgentsRC(projectPath)
	if err != nil {
		return fmt.Errorf("loading .agentsrc.json: %w", err)
	}
	projectName := rc.Project
	if projectName == "" {
		return fmt.Errorf(".agentsrc.json has no project name set")
	}

	agentsHome := config.AgentsHome()
	destDir := filepath.Join(agentsHome, "skills", projectName)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("creating skills directory: %w", err)
	}
	canonicalPath := filepath.Join(destDir, name)

	if sourceInfo.Mode()&os.ModeSymlink != 0 {
		// Already a managed symlink — verify it points to the canonical path.
		existing, err := os.Readlink(sourcePath)
		if err != nil {
			return fmt.Errorf("reading existing symlink for skill %q: %w", name, err)
		}
		if existing != canonicalPath {
			return fmt.Errorf("skill %q is already a symlink but points to %q, not the canonical path %q", name, existing, canonicalPath)
		}
		// Already converged; fall through to manifest update.
	} else {
		if _, err := os.Stat(filepath.Join(sourcePath, "SKILL.md")); err != nil {
			return fmt.Errorf("skill %q not found in .agents/skills/ (expected SKILL.md at %s/SKILL.md)", name, sourcePath)
		}
		// Repo-local is a real directory. Copy content to canonical, then
		// replace repo-local with a managed symlink.
		if fi, err := os.Lstat(canonicalPath); err == nil {
			if fi.Mode()&os.ModeSymlink != 0 {
				// Old-style promote left a back-symlink; remove it so we can
				// create the real directory.
				if err := os.Remove(canonicalPath); err != nil {
					return fmt.Errorf("removing stale canonical symlink for skill %q: %w", name, err)
				}
			} else {
				return fmt.Errorf("skill %q already exists at canonical path %s as a real directory; cannot promote", name, canonicalPath)
			}
		}
		if err := copySkillDir(sourcePath, canonicalPath); err != nil {
			return fmt.Errorf("copying skill %q to canonical path: %w", name, err)
		}
		if err := os.RemoveAll(sourcePath); err != nil {
			return fmt.Errorf("removing repo-local skill directory for %q: %w", name, err)
		}
		if err := os.Symlink(canonicalPath, sourcePath); err != nil {
			return fmt.Errorf("creating repo-local managed symlink for skill %q: %w", name, err)
		}
	}

	// Register in .agentsrc.json.
	rc.Skills = config.AppendUnique(rc.Skills, name)
	if err := rc.Save(projectPath); err != nil {
		return fmt.Errorf("updating .agentsrc.json: %w", err)
	}

	// Refresh platform-level skill mirrors using the shared executor.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		ui.Bullet("warn", "could not determine home directory; skipping platform mirrors: "+err.Error())
	} else {
		targetRoots := []string{
			filepath.Join(homeDir, ".agents", "skills"),
			filepath.Join(homeDir, ".claude", "skills"),
		}
		if err := platform.ExecuteSharedSkillMirrorPlan(projectName, projectPath, targetRoots...); err != nil {
			ui.Bullet("warn", "platform mirror refresh failed: "+err.Error())
		}
	}

	ui.SuccessBox(
		fmt.Sprintf("Promoted skill '%s' for project '%s'", name, projectName),
		fmt.Sprintf("Registered in .agentsrc.json (%d skill(s) total)", len(rc.Skills)),
		"Run 'dot-agents refresh' to sync across all platforms",
	)
	return nil
}

// copySkillDir recursively copies the directory tree at src to dst, preserving
// file modes. Symlinks in the source tree are skipped.
func copySkillDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // skip symlinks
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
