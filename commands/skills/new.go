package skills

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/AGOrcha/dot-agents/internal/config"
	scaffoldtemplates "github.com/AGOrcha/dot-agents/internal/scaffold/templates"
	"github.com/AGOrcha/dot-agents/internal/ui"
)

// AppendSkillToAgentsRC adds name to the .agentsrc.json Skills list for the
// project registered under scope. Returns a status message on success, "" if
// the scope is not registered, the manifest is legitimately absent, or the
// registration step failed — the failure case is surfaced to the caller by
// CreateSkill via the lower-level appendSkillToAgentsRC instead. Production
// wrapper over appendSkillToAgentsRC; passes stdSkillsIO{}.
func AppendSkillToAgentsRC(name, scope string) string {
	msg, _ := appendSkillToAgentsRC(stdSkillsIO{}, name, scope)
	return msg
}

// appendSkillToAgentsRC is the IO-injected unit under test. Tests pass a
// fakeSkillsIO to drive the ConfigLoad error branch (see seams_test.go). A
// non-nil error means the project IS registered but ConfigLoad/LoadAgentsRC/
// rc.Save hit a real failure (corrupt JSON, permission denied, disk full) —
// createSkill has already written SKILL.md, so it downgrades its success
// message to a warning instead of rolling back. A missing .agentsrc.json
// (os.IsNotExist) is legitimate absence, not an error.
func appendSkillToAgentsRC(io skillsIO, name, scope string) (string, error) {
	cfg, err := io.ConfigLoad()
	if err != nil {
		return "", fmt.Errorf("loading config.json: %w", err)
	}
	projPath := cfg.GetProjectPath(scope)
	if projPath == "" {
		return "", nil
	}
	rc, err := config.LoadAgentsRC(projPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("loading .agentsrc.json: %w", err)
	}
	rc.Skills = config.AppendUnique(rc.Skills, name)
	if err := rc.Save(projPath); err != nil {
		return "", fmt.Errorf("saving .agentsrc.json: %w", err)
	}
	return "Updated .agentsrc.json with skill '" + name + "'", nil
}

// EnsureSkillMarkdown writes a templated SKILL.md when one does not already
// exist at skillMD. A pre-existing file is preserved verbatim. Production
// wrapper over ensureSkillMarkdown; passes stdSkillsIO{}.
func EnsureSkillMarkdown(skillMD, name string) error {
	return ensureSkillMarkdown(stdSkillsIO{}, skillMD, name)
}

// ensureSkillMarkdown is the IO-injected unit under test. Tests pass a
// fakeSkillsIO to drive the WriteFile error branch.
func ensureSkillMarkdown(io skillsIO, skillMD, name string) error {
	if _, err := os.Stat(skillMD); os.IsNotExist(err) {
		content, err := scaffoldtemplates.RenderSkillManifest(name)
		if err != nil {
			return fmt.Errorf("rendering SKILL.md: %w", err)
		}
		if err := io.WriteFile(skillMD, []byte(content), 0644); err != nil {
			return fmt.Errorf("creating SKILL.md: %w", err)
		}
	}
	return nil
}

// SkillCreationNextSteps composes the next-steps list shown after a successful
// skill creation. For non-global scopes the function also attempts to update
// the project .agentsrc.json and appends a confirmation line when that
// succeeded. Production wrapper over skillCreationNextSteps.
func SkillCreationNextSteps(name, scope, skillMD string) []string {
	steps, _ := skillCreationNextSteps(stdSkillsIO{}, name, scope, skillMD)
	return steps
}

// skillCreationNextSteps is the IO-injected unit; only routes through io for
// the appendSkillToAgentsRC delegation. A non-nil error propagates a real
// registration failure from appendSkillToAgentsRC up to createSkill.
func skillCreationNextSteps(io skillsIO, name, scope, skillMD string) ([]string, error) {
	nextSteps := []string{"Edit the skill: " + config.DisplayPath(skillMD)}
	if scope == "global" {
		return nextSteps, nil
	}
	msg, err := appendSkillToAgentsRC(io, name, scope)
	if err != nil {
		return nextSteps, err
	}
	if msg != "" {
		nextSteps = append(nextSteps, msg)
	}
	return nextSteps, nil
}

// EnsureUserSkillLinks creates symlinks for a single global skill into all
// user-level skill directories so the skill is immediately available without
// requiring a full refresh. Production wrapper over ensureUserSkillLinks.
//
//   - ~/.agents/skills/<name>  → agentsHome/skills/global/<name>   (Codex)
//   - ~/.claude/skills/<name>  → agentsHome/skills/global/<name>   (Claude Code)
func EnsureUserSkillLinks(agentsHome, name, skillDir string) {
	_ = ensureUserSkillLinks(stdSkillsIO{}, agentsHome, name, skillDir)
}

// ensureUserSkillLinks is the IO-injected unit under test. Tests pass a
// fakeSkillsIO to drive the MkdirAll and Symlink error branches. A non-nil
// error (joining every target's failure) propagates up to createSkill, which
// has already written SKILL.md, so it downgrades its success message to a
// warning instead of rolling back. A target that already has a link is
// skipped, not an error.
func ensureUserSkillLinks(io skillsIO, agentsHome, name, skillDir string) error {
	homeDir, err := config.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolving user home directory: %w", err)
	}
	targets := []string{
		filepath.Join(homeDir, ".agents", "skills", name),
		filepath.Join(homeDir, ".claude", "skills", name),
	}
	var errs []error
	for _, target := range targets {
		if err := io.MkdirAll(filepath.Dir(target), 0755); err != nil {
			errs = append(errs, fmt.Errorf("creating %s: %w", filepath.Dir(target), err))
			continue
		}
		if _, err := os.Lstat(target); err == nil {
			continue // already exists
		}
		if err := io.Symlink(skillDir, target); err != nil {
			errs = append(errs, fmt.Errorf("linking %s: %w", target, err))
		}
	}
	return errors.Join(errs...)
}

// CreateSkill scaffolds a new skill under ~/.agents/skills/<scope>/<name>/,
// drops a templated SKILL.md, and (for global-scope skills) wires user-level
// platform symlinks so the skill is live without requiring `da refresh`.
// Production wrapper over createSkill; passes stdSkillsIO{}.
func CreateSkill(name, scope string) error {
	return createSkill(stdSkillsIO{}, name, scope)
}

// createSkill is the IO-injected unit under test. Tests pass a fakeSkillsIO
// to drive the MkdirAll and downstream ensureSkillMarkdown error branches.
// SKILL.md creation failures still abort with a hard error (nothing was
// written yet). Once SKILL.md exists, a registration-step failure (the
// user-level symlinks and/or the .agentsrc.json update) does NOT roll it
// back or fail the call — it downgrades the terminal success message to a
// warning naming exactly what didn't register.
func createSkill(io skillsIO, name, scope string) error {
	agentsHome := config.AgentsHome()
	skillDir := filepath.Join(agentsHome, "skills", scope, name)

	if err := io.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("creating skill directory: %w", err)
	}

	skillMD := filepath.Join(skillDir, "SKILL.md")
	if err := ensureSkillMarkdown(io, skillMD, name); err != nil {
		return err
	}

	// Create user-level symlinks immediately so the skill is live without
	// needing a refresh. Only global-scope skills get user-level links.
	var regErrs []string
	if scope == "global" {
		if err := ensureUserSkillLinks(io, agentsHome, name, skillDir); err != nil {
			regErrs = append(regErrs, "user-level skill symlinks: "+err.Error())
		}
	}

	nextSteps, err := skillCreationNextSteps(io, name, scope, skillMD)
	if err != nil {
		regErrs = append(regErrs, ".agentsrc.json: "+err.Error())
	}

	if len(regErrs) > 0 {
		lines := append([]string{}, nextSteps...)
		for _, e := range regErrs {
			lines = append(lines, "did not register — "+e)
		}
		ui.WarnBox(
			fmt.Sprintf("Created skill '%s' in ~/.agents/skills/%s/%s/, but registration did not fully complete", name, scope, name),
			lines...,
		)
		return nil
	}

	ui.SuccessBox(
		fmt.Sprintf("Created skill '%s' in ~/.agents/skills/%s/%s/", name, scope, name),
		nextSteps...,
	)
	return nil
}
