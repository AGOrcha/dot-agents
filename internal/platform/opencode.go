package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
)

type opencode struct{}

func NewOpenCode() Platform { return &opencode{} }

func (o *opencode) ID() string          { return "opencode" }
func (o *opencode) DisplayName() string { return "OpenCode" }

func (o *opencode) IsInstalled() bool {
	_, err := exec.LookPath("opencode")
	return err == nil
}

func (o *opencode) Version() string {
	out, err := exec.Command("opencode", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Split(string(out), "\n")[0])
}

func (o *opencode) HasDeprecatedFormat(repoPath string) bool { return false }
func (o *opencode) DeprecatedDetails(repoPath string) string { return "" }

func (o *opencode) CreateLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	if err := o.ensureUserAgents(agentsHome); err != nil {
		return err
	}

	// opencode.json config
	if src := resolveScopedFile(agentsHome, "settings", project, "opencode.json"); src != "" {
		links.Symlink(src, filepath.Join(repoPath, "opencode.json"))
	}

	// .opencode/agent/ definitions from canonical agents/{scope}/{name}/AGENT.md
	agentDir := filepath.Join(repoPath, ".opencode", "agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return err
	}

	if err := syncScopedFileSymlinks(agentsHome, "agents", project, "AGENT.md", agentDir, ".md"); err != nil {
		return err
	}

	// Project skills → .agents/skills/
	if err := o.createSkillsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	return nil
}

func (o *opencode) ensureUserAgents(agentsHome string) error {
	for _, homeRoot := range config.UserHomeRoots() {
		userAgentsDir := filepath.Join(homeRoot, ".opencode", "agent")
		if err := syncScopedFileSymlinks(agentsHome, "agents", "global", "AGENT.md", userAgentsDir, ".md"); err != nil {
			return err
		}
	}
	return nil
}

func (o *opencode) createSkillsLinks(project, repoPath, agentsHome string) error {
	skillsTarget := filepath.Join(repoPath, ".agents", "skills")
	if err := os.MkdirAll(skillsTarget, 0755); err != nil {
		return err
	}
	entries, err := listScopedResourceDirs(agentsHome, "skills", project, "SKILL.md")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		target := filepath.Join(skillsTarget, e.Name)
		if _, err := os.Lstat(target); err == nil {
			continue
		}
		links.Symlink(e.Dir, target)
	}
	return nil
}

func (o *opencode) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, "opencode.json"), agentsHome)

	agentDir := filepath.Join(repoPath, ".opencode", "agent")
	if entries, err := os.ReadDir(agentDir); err == nil {
		for _, e := range entries {
			links.RemoveIfSymlinkUnder(filepath.Join(agentDir, e.Name()), agentsHome)
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
