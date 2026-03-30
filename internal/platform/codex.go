package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
)

type codex struct{}

func NewCodex() Platform { return &codex{} }

func (c *codex) ID() string          { return "codex" }
func (c *codex) DisplayName() string { return "Codex CLI" }

func (c *codex) IsInstalled() bool {
	_, err := exec.LookPath("codex")
	return err == nil
}

func (c *codex) Version() string {
	out, err := exec.Command("codex", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Split(string(out), "\n")[0])
}

func (c *codex) HasDeprecatedFormat(repoPath string) bool { return false }
func (c *codex) DeprecatedDetails(repoPath string) string { return "" }

func (c *codex) CreateLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	if err := c.ensureUserAgents(agentsHome); err != nil {
		return err
	}
	if err := c.ensureUserSkills(agentsHome); err != nil {
		return err
	}

	// AGENTS.md: global then project override
	globalCandidates := []string{
		filepath.Join(agentsHome, "rules", "global", "agents.md"),
		filepath.Join(agentsHome, "rules", "global", "agents.mdc"),
		filepath.Join(agentsHome, "rules", "global", "rules.md"),
		filepath.Join(agentsHome, "rules", "global", "rules.mdc"),
	}
	for _, src := range globalCandidates {
		if _, err := os.Stat(src); err == nil {
			links.Symlink(src, filepath.Join(repoPath, "AGENTS.md"))
			break
		}
	}
	// Project override
	for _, name := range []string{"agents.md", "agents.mdc"} {
		src := filepath.Join(agentsHome, "rules", project, name)
		if _, err := os.Stat(src); err == nil {
			links.Symlink(src, filepath.Join(repoPath, "AGENTS.md"))
			break
		}
	}

	// .codex/config.toml
	if err := os.MkdirAll(filepath.Join(repoPath, ".codex"), 0755); err != nil {
		return err
	}
	for _, scope := range []string{project, "global"} {
		src := filepath.Join(agentsHome, "settings", scope, "codex.toml")
		if _, err := os.Stat(src); err == nil {
			links.Symlink(src, filepath.Join(repoPath, ".codex", "config.toml"))
			break
		}
	}

	// Project agents → .claude/agents/ (GCD compat)
	if err := c.createAgentsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	// Project skills → .agents/skills/
	if err := c.createSkillsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	return nil
}

func (c *codex) ensureUserAgents(agentsHome string) error {
	globalAgents := filepath.Join(agentsHome, "agents", "global")
	if _, err := os.Stat(globalAgents); err != nil {
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		userAgentsDir := filepath.Join(homeRoot, ".codex", "agents")
		if err := os.MkdirAll(userAgentsDir, 0755); err != nil {
			continue
		}
		entries, _ := os.ReadDir(globalAgents)
		for _, e := range entries {
			agentDir := filepath.Join(globalAgents, e.Name())
			if !links.IsDirEntry(agentDir) {
				continue
			}
			if _, err := os.Stat(filepath.Join(agentDir, "AGENT.md")); err != nil {
				continue
			}
			target := filepath.Join(userAgentsDir, e.Name())
			if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			links.Symlink(agentDir, target)
		}
	}
	return nil
}

func (c *codex) ensureUserSkills(agentsHome string) error {
	globalSkills := filepath.Join(agentsHome, "skills", "global")
	if _, err := os.Stat(globalSkills); err != nil {
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		userSkillsDir := filepath.Join(homeRoot, ".agents", "skills")
		if err := os.MkdirAll(userSkillsDir, 0755); err != nil {
			continue
		}
		entries, _ := os.ReadDir(globalSkills)
		for _, e := range entries {
			skillDir := filepath.Join(globalSkills, e.Name())
			if !links.IsDirEntry(skillDir) {
				continue
			}
			if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
				continue
			}
			target := filepath.Join(userSkillsDir, e.Name())
			if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			links.Symlink(skillDir, target)
		}
	}
	return nil
}

func (c *codex) createAgentsLinks(project, repoPath, agentsHome string) error {
	agentsTarget := filepath.Join(repoPath, ".claude", "agents")
	if err := os.MkdirAll(agentsTarget, 0755); err != nil {
		return err
	}
	projectAgents := filepath.Join(agentsHome, "agents", project)
	entries, err := os.ReadDir(projectAgents)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		agentDir := filepath.Join(projectAgents, e.Name())
		if !links.IsDirEntry(agentDir) {
			continue
		}
		if _, err := os.Stat(filepath.Join(agentDir, "AGENT.md")); err != nil {
			continue
		}
		target := filepath.Join(agentsTarget, e.Name())
		if _, err := os.Lstat(target); err == nil {
			continue
		}
		links.Symlink(agentDir, target)
	}
	return nil
}

func (c *codex) createSkillsLinks(project, repoPath, agentsHome string) error {
	skillsTarget := filepath.Join(repoPath, ".agents", "skills")
	if err := os.MkdirAll(skillsTarget, 0755); err != nil {
		return err
	}
	projectSkills := filepath.Join(agentsHome, "skills", project)
	entries, err := os.ReadDir(projectSkills)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		skillDir := filepath.Join(projectSkills, e.Name())
		if !links.IsDirEntry(skillDir) {
			continue
		}
		if _, err := os.Stat(filepath.Join(skillDir, "SKILL.md")); err != nil {
			continue
		}
		target := filepath.Join(skillsTarget, e.Name())
		if _, err := os.Lstat(target); err == nil {
			continue
		}
		links.Symlink(skillDir, target)
	}
	return nil
}

func (c *codex) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, "AGENTS.md"), agentsHome)
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, ".codex", "config.toml"), agentsHome)

	agentsDir := filepath.Join(repoPath, ".claude", "agents")
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			links.RemoveIfSymlinkUnder(filepath.Join(agentsDir, e.Name()), agentsHome)
		}
	}

	skillsDir := filepath.Join(repoPath, ".agents", "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			links.RemoveIfSymlinkUnder(filepath.Join(skillsDir, e.Name()), agentsHome)
		}
	}

	return nil
}
