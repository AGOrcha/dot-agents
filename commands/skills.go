package commands

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
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

func createSkill(name, scope string) error {
	agentsHome := config.AgentsHome()
	skillDir := filepath.Join(agentsHome, "skills", scope, name)

	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("creating skill directory: %w", err)
	}

	skillMD := filepath.Join(skillDir, "SKILL.md")
	if _, err := os.Stat(skillMD); os.IsNotExist(err) {
		content := fmt.Sprintf("---\nname: %s\ndescription: \"\"\n---\n\n# %s\n\n## When to Use\n\n- \n\n## Steps\n\n1. \n", name, name)
		if err := os.WriteFile(skillMD, []byte(content), 0644); err != nil {
			return fmt.Errorf("creating SKILL.md: %w", err)
		}
	}

	// Create user-level symlinks immediately so the skill is live without needing a refresh.
	// Only global-scope skills get user-level links.
	if scope == "global" {
		ensureUserSkillLinks(agentsHome, name, skillDir)
	}

	nextSteps := []string{"Edit the skill: " + config.DisplayPath(skillMD)}

	// Auto-update .agentsrc.json if at project scope and manifest exists in CWD.
	if scope != "global" {
		if cwd, err := os.Getwd(); err == nil {
			if rc, err := config.LoadAgentsRC(cwd); err == nil {
				rc.Skills = config.AppendUnique(rc.Skills, name)
				if saveErr := rc.Save(cwd); saveErr == nil {
					nextSteps = append(nextSteps, "Updated .agentsrc.json with skill '"+name+"'")
				}
			}
		}
	}

	ui.SuccessBox(fmt.Sprintf("Created skill '%s' in ~/.agents/skills/%s/%s/", name, scope, name), nextSteps...)
	return nil
}
