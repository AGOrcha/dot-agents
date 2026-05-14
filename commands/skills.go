package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/commands/skills"
	"github.com/NikashPrakash/dot-agents/internal/config"
	scaffoldtemplates "github.com/NikashPrakash/dot-agents/internal/scaffold/templates"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skills",
		Short: "Manage skills in ~/.agents/skills/",
		Long: `Lists, creates, and promotes reusable skills stored in the canonical
~/.agents/skills tree. Skills created here can be linked into projects and consumed
by supported AI platforms through refresh or install.`,
		Example: ExampleBlock(
			"  da skills list",
			"  da skills new agent-start",
			"  da skills promote session-start",
		),
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
		Example: ExampleBlock(
			"  da skills list",
			"  da skills list billing-api",
		),
		Args: MaximumNArgsWithHints(1, "Optionally pass a project scope to list project-local skills."),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return skills.List(scope)
		},
	}
}

func newSkillsNewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "new <name> [project]",
		Short: "Create a new skill",
		Example: ExampleBlock(
			"  da skills new self-review",
			"  da skills new repo-bootstrap billing-api",
		),
		Args: RangeArgsWithHints(1, 2, "Pass a skill name and optionally a project scope."),
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
		content, err := scaffoldtemplates.RenderSkillManifest(name)
		if err != nil {
			return fmt.Errorf("rendering SKILL.md: %w", err)
		}
		if err := osWriteFile(skillMD, []byte(content), 0644); err != nil {
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

func createSkill(name, scope string) error {
	agentsHome := config.AgentsHome()
	skillDir := filepath.Join(agentsHome, "skills", scope, name)

	if err := osMkdirAll(skillDir, 0755); err != nil {
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
		Example: ExampleBlock(
			"  da skills promote session-start",
			"  da status --audit",
		),
		Args: ExactArgsWithHints(1, "Run this from the project repository that owns `.agents/skills/<name>/`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectPath, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolving project path: %w", err)
			}
			return skills.PromoteSkillIn(args[0], projectPath)
		},
	}
}
