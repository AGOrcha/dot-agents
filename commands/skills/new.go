package skills

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/internal/config"
	scaffoldtemplates "github.com/NikashPrakash/dot-agents/internal/scaffold/templates"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

// AppendSkillToAgentsRC adds name to the .agentsrc.json Skills list for the
// project registered under scope. Returns a status message on success, "" if
// the scope is not registered or the manifest is missing — both are non-fatal
// "best effort" outcomes used by skillCreationNextSteps.
func AppendSkillToAgentsRC(name, scope string) string {
	cfg, err := configLoad()
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

// EnsureSkillMarkdown writes a templated SKILL.md when one does not already
// exist at skillMD. A pre-existing file is preserved verbatim.
func EnsureSkillMarkdown(skillMD, name string) error {
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

// SkillCreationNextSteps composes the next-steps list shown after a successful
// skill creation. For non-global scopes the function also attempts to update
// the project .agentsrc.json and appends a confirmation line when that
// succeeded.
func SkillCreationNextSteps(name, scope, skillMD string) []string {
	nextSteps := []string{"Edit the skill: " + config.DisplayPath(skillMD)}
	if scope != "global" {
		if msg := AppendSkillToAgentsRC(name, scope); msg != "" {
			nextSteps = append(nextSteps, msg)
		}
	}
	return nextSteps
}

// EnsureUserSkillLinks creates symlinks for a single global skill into all
// user-level skill directories so the skill is immediately available without
// requiring a full refresh.
//
//   - ~/.agents/skills/<name>  → agentsHome/skills/global/<name>   (Codex)
//   - ~/.claude/skills/<name>  → agentsHome/skills/global/<name>   (Claude Code)
func EnsureUserSkillLinks(agentsHome, name, skillDir string) {
	homeDir, err := config.UserHomeDir()
	if err != nil {
		return
	}
	targets := []string{
		filepath.Join(homeDir, ".agents", "skills", name),
		filepath.Join(homeDir, ".claude", "skills", name),
	}
	for _, target := range targets {
		if err := osMkdirAll(filepath.Dir(target), 0755); err != nil {
			continue
		}
		if _, err := os.Lstat(target); err == nil {
			continue // already exists
		}
		_ = osSymlink(skillDir, target)
	}
}

// CreateSkill scaffolds a new skill under ~/.agents/skills/<scope>/<name>/,
// drops a templated SKILL.md, and (for global-scope skills) wires user-level
// platform symlinks so the skill is live without requiring `da refresh`.
func CreateSkill(name, scope string) error {
	agentsHome := config.AgentsHome()
	skillDir := filepath.Join(agentsHome, "skills", scope, name)

	if err := osMkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("creating skill directory: %w", err)
	}

	skillMD := filepath.Join(skillDir, "SKILL.md")
	if err := EnsureSkillMarkdown(skillMD, name); err != nil {
		return err
	}

	// Create user-level symlinks immediately so the skill is live without
	// needing a refresh. Only global-scope skills get user-level links.
	if scope == "global" {
		EnsureUserSkillLinks(agentsHome, name, skillDir)
	}

	ui.SuccessBox(
		fmt.Sprintf("Created skill '%s' in ~/.agents/skills/%s/%s/", name, scope, name),
		SkillCreationNextSteps(name, scope, skillMD)...,
	)
	return nil
}
