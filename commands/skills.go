package commands

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage skills in ~/.agents/skills/",
	}
	cmd.AddCommand(newSkillsListCmd())
	cmd.AddCommand(newSkillsNewCmd())
	cmd.AddCommand(newSkillsPromoteCmd())
	return cmd
}

func newSkillsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [project]",
		Short: "List skills",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return listSkills(scope)
		},
	}
}

func listSkills(scope string) error {
	agentsHome := config.AgentsHome()
	skillsDir := filepath.Join(agentsHome, "skills", scope)

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		ui.Info("No skills found in ~/.agents/skills/" + scope + "/")
		return nil
	}

	ui.Header("Skills (" + scope + ")")
	count := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, e.Name())
		skillMD := filepath.Join(skillPath, "SKILL.md")
		if _, err := os.Stat(skillMD); err == nil {
			desc := readFrontmatterDescription(skillMD)
			if desc != "" {
				ui.Bullet("ok", fmt.Sprintf("%s  %s%s%s", e.Name(), ui.Dim, desc, ui.Reset))
			} else {
				ui.Bullet("ok", e.Name())
			}
		} else {
			ui.Bullet("warn", e.Name()+" (no SKILL.md)")
		}
		count++
	}
	fmt.Fprintf(os.Stdout, "\n  %s%d skill(s) in %s scope%s\n\n", ui.Dim, count, scope, ui.Reset)
	return nil
}

// readFrontmatterDescription parses the YAML frontmatter of a markdown file
// and returns the value of the "description:" field.
// ensureUserSkillLinks creates symlinks for a single global skill into all
// user-level skill directories so the skill is immediately available without
// requiring a full refresh.
//
//   - ~/.agents/skills/<name>  → agentsHome/skills/global/<name>   (Codex)
//   - ~/.claude/skills/<name>  → agentsHome/skills/global/<name>   (Claude Code)
func ensureUserSkillLinks(agentsHome, name, skillDir string) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	targets := []string{
		filepath.Join(homeDir, ".agents", "skills", name),
		filepath.Join(homeDir, ".claude", "skills", name),
	}
	for _, target := range targets {
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			continue
		}
		if _, err := os.Lstat(target); err == nil {
			continue // already exists
		}
		_ = os.Symlink(skillDir, target)
	}
}

func readFrontmatterDescription(mdPath string) string {
	f, err := os.Open(mdPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inFrontmatter := false
	lineNum := 0
	for scanner.Scan() {
		line := scanner.Text()
		lineNum++
		if lineNum == 1 {
			if strings.TrimSpace(line) == "---" {
				inFrontmatter = true
			} else {
				return ""
			}
			continue
		}
		if inFrontmatter {
			if strings.TrimSpace(line) == "---" {
				break
			}
			if strings.HasPrefix(line, "description:") {
				val := strings.TrimPrefix(line, "description:")
				val = strings.TrimSpace(val)
				val = strings.Trim(val, `"'`)
				return val
			}
		}
	}
	return ""
}

func newSkillsNewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new <name> [project]",
		Short: "Create a new skill",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			scope := "global"
			if len(args) > 1 {
				scope = args[1]
			}
			return createSkill(name, scope)
		},
	}
}

// appendSkillToAgentsRC adds name to the .agentsrc.json Skills list for the
// project registered under scope. Returns a status message on success, "" otherwise.
func appendSkillToAgentsRC(name, scope string) string {
	cfg, err := config.Load()
	if err != nil {
		return ""
	}
	projPath := cfg.GetProjectPath(scope)
	if projPath == "" {
		return ""
	}
	rc, err := config.LoadAgentsRC(projPath)
	if err != nil {
		return ""
	}
	rc.Skills = config.AppendUnique(rc.Skills, name)
	if err := rc.Save(projPath); err != nil {
		return ""
	}
	return "Updated .agentsrc.json with skill '" + name + "'"
}

func ensureSkillMarkdown(skillMD, name string) error {
	if _, err := os.Stat(skillMD); os.IsNotExist(err) {
		content := fmt.Sprintf("---\nname: %s\ndescription: \"\"\n---\n\n# %s\n\n## When to Use\n\n- \n\n## Steps\n\n1. \n", name, name)
		if err := os.WriteFile(skillMD, []byte(content), 0644); err != nil {
			return fmt.Errorf("creating SKILL.md: %w", err)
		}
	}
	return nil
}

func skillCreationNextSteps(name, scope, skillMD string) []string {
	nextSteps := []string{"Edit the skill: " + config.DisplayPath(skillMD)}
	if scope != "global" {
		if msg := appendSkillToAgentsRC(name, scope); msg != "" {
			nextSteps = append(nextSteps, msg)
		}
	}
	return nextSteps
}

func createSkill(name, scope string) error {
	agentsHome := config.AgentsHome()
	skillDir := filepath.Join(agentsHome, "skills", scope, name)

	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("creating skill directory: %w", err)
	}

	skillMD := filepath.Join(skillDir, "SKILL.md")
	if err := ensureSkillMarkdown(skillMD, name); err != nil {
		return err
	}

	// Create user-level symlinks immediately so the skill is live without needing a refresh.
	// Only global-scope skills get user-level links.
	if scope == "global" {
		ensureUserSkillLinks(agentsHome, name, skillDir)
	}

	ui.SuccessBox(fmt.Sprintf("Created skill '%s' in ~/.agents/skills/%s/%s/", name, scope, name), skillCreationNextSteps(name, scope, skillMD)...)
	return nil
}

func newSkillsPromoteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "promote <name>",
		Short: "Promote a repo-local skill to shared storage",
		Long: `Promotes a skill from .agents/skills/<name>/ in the current repo to
~/.agents/skills/<project>/<name>/, registers it in .agentsrc.json, and
refreshes shared skill mirrors for all platforms.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolving project path: %w", err)
			}
			return promoteSkillIn(args[0], projectPath)
		},
	}
}

// promoteSkillIn promotes a repo-local skill (.agents/skills/<name>/) into the
// shared agents store. The canonical location (~/.agents/skills/<project>/<name>/)
// becomes the real directory, and the repo-local path is converted to a managed
// symlink pointing at it. This prevents circular symlinks when platform mirror
// refresh later targets .agents/skills/ inside the repo.
func promoteSkillIn(name, projectPath string) error {
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
