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
	for _, scope := range []string{project, "global"} {
		src := filepath.Join(agentsHome, "settings", scope, "opencode.json")
		if _, err := os.Stat(src); err == nil {
			links.Symlink(src, filepath.Join(repoPath, "opencode.json"))
			break
		}
	}

	// .opencode/agent/ definitions from rules/{project}/opencode-*.md
	agentDir := filepath.Join(repoPath, ".opencode", "agent")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return err
	}

	projectRulesDir := filepath.Join(agentsHome, "rules", project)
	if entries, err := os.ReadDir(projectRulesDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasPrefix(name, "opencode-") || !strings.HasSuffix(name, ".md") {
				continue
			}
			targetName := strings.TrimPrefix(name, "opencode-")
			src := filepath.Join(projectRulesDir, name)
			links.Symlink(src, filepath.Join(agentDir, targetName))
		}
	}

	// Project skills → .agents/skills/
	if err := o.createSkillsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	return nil
}

func (o *opencode) ensureUserAgents(agentsHome string) error {
	globalRules := filepath.Join(agentsHome, "rules", "global")
	if _, err := os.Stat(globalRules); err != nil {
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		userAgentsDir := filepath.Join(homeRoot, ".opencode", "agent")
		if err := os.MkdirAll(userAgentsDir, 0755); err != nil {
			continue
		}
		entries, _ := os.ReadDir(globalRules)
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if !strings.HasPrefix(name, "opencode-") || !strings.HasSuffix(name, ".md") {
				continue
			}
			targetName := strings.TrimPrefix(name, "opencode-")
			src := filepath.Join(globalRules, name)
			target := filepath.Join(userAgentsDir, targetName)
			if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			links.Symlink(src, target)
		}
	}
	return nil
}

func (o *opencode) createSkillsLinks(project, repoPath, agentsHome string) error {
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
