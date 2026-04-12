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

const opencodeJSON = "opencode.json"

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
	if src := resolveScopedFile(agentsHome, "settings", project, opencodeJSON); src != "" {
		links.Symlink(src, filepath.Join(repoPath, opencodeJSON))
	}

	// .opencode/agent/*.md and .agents/skills/ — emitted by CollectAndExecuteSharedTargetPlan
	// via SharedTargetIntents; no direct action needed here.

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


func (o *opencode) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, opencodeJSON), agentsHome)

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

func (o *opencode) SharedTargetIntents(project string) ([]ResourceIntent, error) {
	skills, err := BuildSharedSkillMirrorIntents(project, filepath.Join(".agents", "skills"))
	if err != nil {
		return nil, err
	}
	plugins, err := BuildSharedPluginBundleIntents(project, filepath.Join(".opencode", "plugins"))
	if err != nil {
		return nil, err
	}
	agents, err := BuildSharedAgentFileSymlinkIntents(project, filepath.Join(".opencode", "agent"), ".md")
	if err != nil {
		return nil, err
	}
	return append(append(skills, plugins...), agents...), nil
}
