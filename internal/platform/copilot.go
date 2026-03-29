package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/dot-agents/dot-agents/internal/config"
	"github.com/dot-agents/dot-agents/internal/links"
)

type copilot struct{}

func NewCopilot() Platform { return &copilot{} }

func (c *copilot) ID() string          { return "copilot" }
func (c *copilot) DisplayName() string { return "GitHub Copilot" }

func (c *copilot) IsInstalled() bool {
	home, _ := os.UserHomeDir()
	for _, dir := range []string{
		filepath.Join(home, ".vscode", "extensions"),
		filepath.Join(home, ".vscode-insiders", "extensions"),
		filepath.Join(home, ".vscode-server", "extensions"),
	} {
		if entries, err := os.ReadDir(dir); err == nil {
			for _, e := range entries {
				if e.IsDir() && strings.Contains(e.Name(), "copilot") {
					return true
				}
			}
		}
	}
	_, err := exec.LookPath("copilot")
	return err == nil
}

func (c *copilot) Version() string {
	home, _ := os.UserHomeDir()
	for _, dir := range []string{
		filepath.Join(home, ".vscode", "extensions"),
		filepath.Join(home, ".vscode-insiders", "extensions"),
	} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() && strings.Contains(name, "copilot") {
				// Name format: publisher.extension-version
				parts := strings.Split(name, "-")
				if len(parts) >= 2 {
					return parts[len(parts)-1] + " (Extension)"
				}
			}
		}
	}
	out, err := exec.Command("copilot", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Split(string(out), "\n")[0])
}

func (c *copilot) HasDeprecatedFormat(repoPath string) bool { return false }
func (c *copilot) DeprecatedDetails(repoPath string) string { return "" }

func (c *copilot) CreateLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	// .github/copilot-instructions.md
	if err := c.createInstructionsLink(project, repoPath, agentsHome); err != nil {
		return err
	}

	// .agents/skills/
	if err := c.createSkillsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	// .github/agents/{name}.agent.md
	if err := c.createAgentsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	// .vscode/mcp.json
	if err := c.createMCPLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	// .claude/settings.local.json (hooks compat)
	if err := c.createClaudeCompatLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	// .github/hooks/{name}.json
	if err := c.createProjectHookFiles(project, repoPath, agentsHome); err != nil {
		return err
	}

	return nil
}

func (c *copilot) resolveInstructionsSrc(project, agentsHome string) string {
	// Priority order per bash implementation
	candidates := []string{
		filepath.Join(agentsHome, "rules", project, "copilot-instructions.md"),
		filepath.Join(agentsHome, "rules", "global", "copilot-instructions.md"),
	}
	for _, f := range candidates {
		if _, err := os.Stat(f); err == nil {
			return f
		}
	}
	// Fallback: rules.(md|mdc|txt)
	for _, scope := range []string{project, "global"} {
		for _, ext := range []string{"md", "mdc", "txt"} {
			f := filepath.Join(agentsHome, "rules", scope, "rules."+ext)
			if _, err := os.Stat(f); err == nil {
				return f
			}
		}
	}
	return ""
}

func (c *copilot) createInstructionsLink(project, repoPath, agentsHome string) error {
	src := c.resolveInstructionsSrc(project, agentsHome)
	if src == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Join(repoPath, ".github"), 0755); err != nil {
		return err
	}
	links.Symlink(src, filepath.Join(repoPath, ".github", "copilot-instructions.md"))
	return nil
}

func (c *copilot) createSkillsLinks(project, repoPath, agentsHome string) error {
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
		if !e.IsDir() {
			continue
		}
		skillDir := filepath.Join(projectSkills, e.Name())
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

func (c *copilot) createAgentsLinks(project, repoPath, agentsHome string) error {
	agentsTarget := filepath.Join(repoPath, ".github", "agents")
	if err := os.MkdirAll(agentsTarget, 0755); err != nil {
		return err
	}
	projectAgents := filepath.Join(agentsHome, "agents", project)
	entries, err := os.ReadDir(projectAgents)
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		agentDir := filepath.Join(projectAgents, e.Name())
		agentMD := filepath.Join(agentDir, "AGENT.md")
		if _, err := os.Stat(agentMD); err != nil {
			continue
		}
		target := filepath.Join(agentsTarget, e.Name()+".agent.md")
		if _, err := os.Lstat(target); err == nil {
			continue
		}
		links.Symlink(agentMD, target)
	}
	return nil
}

func (c *copilot) createMCPLinks(project, repoPath, agentsHome string) error {
	// Priority: project/copilot.json, project/mcp.json, global/copilot.json, global/mcp.json
	for _, scope := range []string{project, "global"} {
		for _, name := range []string{"copilot.json", "mcp.json"} {
			src := filepath.Join(agentsHome, "mcp", scope, name)
			if _, err := os.Stat(src); err == nil {
				if err := os.MkdirAll(filepath.Join(repoPath, ".vscode"), 0755); err != nil {
					return err
				}
				links.Symlink(src, filepath.Join(repoPath, ".vscode", "mcp.json"))
				return nil
			}
		}
	}
	return nil
}

func (c *copilot) createClaudeCompatLinks(project, repoPath, agentsHome string) error {
	for _, scope := range []string{project, "global"} {
		// hooks/ takes priority over settings/
		for _, dir := range []string{"hooks", "settings"} {
			src := filepath.Join(agentsHome, dir, scope, "claude-code.json")
			if _, err := os.Stat(src); err == nil {
				if err := os.MkdirAll(filepath.Join(repoPath, ".claude"), 0755); err != nil {
					return err
				}
				links.Symlink(src, filepath.Join(repoPath, ".claude", "settings.local.json"))
				return nil
			}
		}
	}
	return nil
}

func (c *copilot) createProjectHookFiles(project, repoPath, agentsHome string) error {
	hooksDir := filepath.Join(agentsHome, "hooks", project)
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		return nil // no project hooks configured
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".json")
		// Skip reserved platform files
		if name == "cursor" || name == "claude-code" {
			continue
		}
		src := filepath.Join(hooksDir, e.Name())
		dstDir := filepath.Join(repoPath, ".github", "hooks")
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			return err
		}
		links.Symlink(src, filepath.Join(dstDir, name+".json"))
	}
	return nil
}

func (c *copilot) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, ".github", "copilot-instructions.md"), agentsHome)
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, ".vscode", "mcp.json"), agentsHome)
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, ".claude", "settings.local.json"), agentsHome)

	// .agents/skills/
	skillsDir := filepath.Join(repoPath, ".agents", "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			links.RemoveIfSymlinkUnder(filepath.Join(skillsDir, e.Name()), agentsHome)
		}
	}

	// .github/agents/*.agent.md
	agentsDir := filepath.Join(repoPath, ".github", "agents")
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".agent.md") {
				links.RemoveIfSymlinkUnder(filepath.Join(agentsDir, e.Name()), agentsHome)
			}
		}
	}

	// .github/hooks/*.json
	hooksDir := filepath.Join(repoPath, ".github", "hooks")
	if entries, err := os.ReadDir(hooksDir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") {
				links.RemoveIfSymlinkUnder(filepath.Join(hooksDir, e.Name()), agentsHome)
			}
		}
	}

	return nil
}
