package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
)

type claude struct{}

const claudeCodeJSON = "claude-code.json"

func NewClaude() Platform { return &claude{} }

func (c *claude) ID() string          { return "claude" }
func (c *claude) DisplayName() string { return "Claude Code" }

func (c *claude) IsInstalled() bool {
	if _, err := exec.LookPath("claude"); err == nil {
		return true
	}
	home, _ := os.UserHomeDir()
	_, err := os.Stat(filepath.Join(home, ".claude"))
	return err == nil
}

func (c *claude) Version() string {
	out, err := exec.Command("claude", "--version").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.Split(string(out), "\n")[0])
}

func (c *claude) HasDeprecatedFormat(repoPath string) bool {
	_, err := os.Stat(filepath.Join(repoPath, ".claude.json"))
	return err == nil
}

func (c *claude) DeprecatedDetails(repoPath string) string {
	if c.HasDeprecatedFormat(repoPath) {
		return ".claude.json → .claude/settings.json"
	}
	return ""
}

func (c *claude) CreateLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	if err := c.prepareLinks(repoPath, agentsHome); err != nil {
		return err
	}

	if err := c.createRulesLinks(project, repoPath, agentsHome); err != nil {
		return err
	}
	c.linkProjectSettings(project, repoPath, agentsHome)
	c.linkProjectMCP(project, repoPath, agentsHome)

	if err := c.createAgentsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}

	return c.createSkillsLinks(project, repoPath, agentsHome)
}

func (c *claude) prepareLinks(repoPath, agentsHome string) error {
	if err := c.ensureUserAgents(agentsHome); err != nil {
		return err
	}
	if err := c.ensureUserRules(agentsHome); err != nil {
		return err
	}
	if err := c.ensureUserSettings(agentsHome); err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(repoPath, ".claude", "rules"), 0755)
}

func (c *claude) linkProjectSettings(project, repoPath, agentsHome string) {
	target := filepath.Join(repoPath, ".claude", "settings.local.json")
	projectBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), project)
	if err == nil && len(projectBundles) > 0 {
		_ = emitRenderedHookFile(projectBundles, target, renderClaudeHookSettings)
		return
	}
	globalBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
	if err == nil && len(globalBundles) > 0 {
		_ = emitRenderedHookFile(globalBundles, target, renderClaudeHookSettings)
		return
	}
	spec := findClaudeSettingsHookSpec(agentsHome, project)
	if spec == nil {
		_ = removeManagedFileIf(target, isLikelyRenderedClaudeHookSettings)
		return
	}
	_ = emitHookSpec(spec, target, HookEmissionMode{
		Shape:     HookShapeDirect,
		Transport: HookTransportSymlink,
	})
}

func (c *claude) linkProjectMCP(project, repoPath, agentsHome string) {
	if src := resolveScopedFile(agentsHome, "mcp", project, "claude.json", "mcp.json"); src != "" {
		links.Symlink(src, filepath.Join(repoPath, ".mcp.json"))
	}
}

func findClaudeSettingsHookSpec(agentsHome, scope string) *HookSpec {
	return resolveHookSpecInScope(agentsHome, []string{"hooks", "settings"}, scope, claudeCodeJSON)
}

func (c *claude) createRulesLinks(project, repoPath, agentsHome string) error {
	rulesDir := filepath.Join(repoPath, ".claude", "rules")
	projectRulesDir := filepath.Join(agentsHome, "rules", project)

	entries, err := os.ReadDir(projectRulesDir)
	if err != nil {
		return nil // no project rules
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := filepath.Ext(name)
		if ext != ".md" && ext != ".mdc" && ext != ".txt" {
			continue
		}
		stem := strings.TrimSuffix(name, ext)
		src := filepath.Join(projectRulesDir, name)
		dst := filepath.Join(rulesDir, project+"--"+stem+".md")
		links.Symlink(src, dst)
	}
	return nil
}

func (c *claude) ensureUserAgents(agentsHome string) error {
	globalAgents := filepath.Join(agentsHome, "agents", "global")
	if _, err := os.Stat(globalAgents); err != nil {
		return nil
	}

	for _, homeRoot := range config.UserHomeRoots() {
		userAgentsDir := filepath.Join(homeRoot, ".claude", "agents")
		if err := os.MkdirAll(userAgentsDir, 0755); err != nil {
			continue
		}
		entries, err := os.ReadDir(globalAgents)
		if err != nil {
			continue
		}
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
				continue // already a symlink
			}
			links.Symlink(agentDir, target)
		}
	}
	return nil
}

func (c *claude) ensureUserRules(agentsHome string) error {
	// Priority list for source
	candidates := []string{
		filepath.Join(agentsHome, "rules", "global", "claude-code.mdc"),
		filepath.Join(agentsHome, "rules", "global", "claude-code.md"),
		filepath.Join(agentsHome, "rules", "global", "rules.mdc"),
		filepath.Join(agentsHome, "rules", "global", "rules.md"),
		filepath.Join(agentsHome, "rules", "global", "rules.txt"),
	}

	var src string
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			src = c
			break
		}
	}
	if src == "" {
		return nil
	}

	for _, homeRoot := range config.UserHomeRoots() {
		target := filepath.Join(homeRoot, ".claude", "CLAUDE.md")
		if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
			continue // already a symlink
		}
		os.MkdirAll(filepath.Join(homeRoot, ".claude"), 0755)
		links.Symlink(src, target)
	}
	return nil
}

func (c *claude) ensureUserSettings(agentsHome string) error {
	globalBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, "global", c.ID(), "global")
	if err != nil {
		return err
	}
	if len(globalBundles) > 0 {
		return emitRenderedHookFileToUserHomes(globalBundles, filepath.Join(".claude", "settings.json"), renderClaudeHookSettings)
	}

	spec := findClaudeSettingsHookSpec(agentsHome, "global")
	if spec == nil {
		for _, homeRoot := range config.UserHomeRoots() {
			_ = removeManagedFileIf(filepath.Join(homeRoot, ".claude", "settings.json"), isLikelyRenderedClaudeHookSettings)
		}
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		target := filepath.Join(homeRoot, ".claude", "settings.json")
		if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
			continue // already a symlink, leave it
		}
		_ = emitHookSpec(spec, target, HookEmissionMode{
			Shape:     HookShapeDirect,
			Transport: HookTransportSymlink,
		})
	}
	return nil
}

func (c *claude) ensureUserSkills(agentsHome string) error {
	for _, homeRoot := range config.UserHomeRoots() {
		userSkillsDir := filepath.Join(homeRoot, ".claude", "skills")
		if err := syncScopedDirSymlinks(agentsHome, "skills", "global", "SKILL.md", userSkillsDir); err != nil {
			return err
		}
	}
	return nil
}

func (c *claude) createAgentsLinks(project, repoPath, agentsHome string) error {
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

func (c *claude) createSkillsLinks(project, repoPath, agentsHome string) error {
	c.ensureUserSkills(agentsHome)

	skillsTarget := filepath.Join(repoPath, ".claude", "skills")
	agentsSkillsTarget := filepath.Join(repoPath, ".agents", "skills")
	if err := os.MkdirAll(skillsTarget, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(agentsSkillsTarget, 0755); err != nil {
		return err
	}

	entries, err := listScopedResourceDirs(agentsHome, "skills", project, "SKILL.md")
	if err != nil {
		return nil
	}
	for _, e := range entries {
		name := e.Name
		claudeTarget := filepath.Join(skillsTarget, name)
		if _, err := os.Lstat(claudeTarget); err != nil {
			links.Symlink(e.Dir, claudeTarget)
		}
		agentsTarget := filepath.Join(agentsSkillsTarget, name)
		if _, err := os.Lstat(agentsTarget); err != nil {
			links.Symlink(e.Dir, agentsTarget)
		}
	}
	return nil
}

func (c *claude) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	// Remove .claude/rules/{project}--*.md symlinks
	rulesDir := filepath.Join(repoPath, ".claude", "rules")
	if entries, err := os.ReadDir(rulesDir); err == nil {
		prefix := project + "--"
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), prefix) {
				linkPath := filepath.Join(rulesDir, e.Name())
				links.RemoveIfSymlinkUnder(linkPath, agentsHome)
			}
		}
	}

	// Remove .claude/settings.local.json
	projectBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), project)
	if err == nil && len(projectBundles) > 0 {
		_ = removeManagedRenderedHookFile(projectBundles, filepath.Join(repoPath, ".claude", "settings.local.json"), renderClaudeHookSettings)
	} else {
		globalBundles, globalErr := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
		if globalErr == nil && len(globalBundles) > 0 {
			_ = removeManagedRenderedHookFile(globalBundles, filepath.Join(repoPath, ".claude", "settings.local.json"), renderClaudeHookSettings)
		}
	}
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, ".claude", "settings.local.json"), agentsHome)

	// Remove .mcp.json
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, ".mcp.json"), agentsHome)

	// Remove .claude/agents/ symlinks
	agentsDir := filepath.Join(repoPath, ".claude", "agents")
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			links.RemoveIfSymlinkUnder(filepath.Join(agentsDir, e.Name()), agentsHome)
		}
	}

	// Remove .claude/skills/ symlinks
	skillsDir := filepath.Join(repoPath, ".claude", "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			links.RemoveIfSymlinkUnder(filepath.Join(skillsDir, e.Name()), agentsHome)
		}
	}

	// Remove .agents/skills/ symlinks
	agentsSkillsDir := filepath.Join(repoPath, ".agents", "skills")
	if entries, err := os.ReadDir(agentsSkillsDir); err == nil {
		for _, e := range entries {
			links.RemoveIfSymlinkUnder(filepath.Join(agentsSkillsDir, e.Name()), agentsHome)
		}
	}

	return nil
}
