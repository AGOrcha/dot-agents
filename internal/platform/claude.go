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

const (
	claudeCodeJSON          = "claude-code.json"
	claudeSettingsJSON      = "settings.json"
	claudeSettingsLocalJSON = "settings.local.json"
	claudeDir               = ".claude"
)

func NewClaude() Platform { return &claude{} }

func (c *claude) ID() string          { return "claude" }
func (c *claude) DisplayName() string { return "Claude Code" }

func (c *claude) IsInstalled() bool {
	if _, err := exec.LookPath("claude"); err == nil {
		return true
	}
	home, _ := os.UserHomeDir()
	_, err := os.Stat(filepath.Join(home, claudeDir))
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
	return os.MkdirAll(filepath.Join(repoPath, claudeDir, "rules"), 0755)
}

func (c *claude) linkProjectSettings(project, repoPath, agentsHome string) {
	target := filepath.Join(repoPath, claudeDir, claudeSettingsLocalJSON)
	projectBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), project)
	if err != nil {
		return
	}
	globalBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
	if err != nil {
		return
	}
	_ = emitPreferredHookFile(
		target,
		renderClaudeHookSettings,
		findClaudeSettingsHookSpec(agentsHome, project),
		directSymlinkHookMode,
		removeRenderedClaudeHookSettings,
		projectBundles,
		globalBundles,
	)
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
	rulesDir := filepath.Join(repoPath, claudeDir, "rules")
	projectRulesDir := filepath.Join(agentsHome, "rules", project)

	entries, err := os.ReadDir(projectRulesDir)
	if err != nil {
		return c.pruneProjectRuleLinks(rulesDir, project)
	}
	wanted := map[string]string{}
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
		wanted[project+"--"+stem+".md"] = src
	}
	if err := c.pruneProjectRuleLinks(rulesDir, project, wanted); err != nil {
		return err
	}
	for name, src := range wanted {
		links.Symlink(src, filepath.Join(rulesDir, name))
	}
	return nil
}

func (c *claude) pruneProjectRuleLinks(rulesDir, project string, wanted ...map[string]string) error {
	keep := map[string]string{}
	if len(wanted) > 0 {
		keep = wanted[0]
	}
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return nil
	}
	prefix := project + "--"
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".md") {
			continue
		}
		if _, ok := keep[name]; ok {
			continue
		}
		if err := os.Remove(filepath.Join(rulesDir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (c *claude) ensureUserAgents(agentsHome string) error {
	globalAgents := filepath.Join(agentsHome, "agents", "global")
	entries, err := os.ReadDir(globalAgents)
	if err != nil {
		return nil
	}

	for _, homeRoot := range config.UserHomeRoots() {
		if err := c.ensureUserAgentsInHome(homeRoot, globalAgents, entries); err != nil {
			continue
		}
	}
	return nil
}

func (c *claude) ensureUserAgentsInHome(homeRoot, globalAgents string, entries []os.DirEntry) error {
	userAgentsDir := filepath.Join(homeRoot, claudeDir, "agents")
	if err := os.MkdirAll(userAgentsDir, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		c.linkUserAgent(globalAgents, userAgentsDir, entry)
	}
	return nil
}

func (c *claude) linkUserAgent(globalAgents, userAgentsDir string, entry os.DirEntry) {
	agentDir := filepath.Join(globalAgents, entry.Name())
	if !isClaudeAgentDir(agentDir) {
		return
	}
	target := filepath.Join(userAgentsDir, entry.Name())
	if isSymlink(target) {
		return
	}
	links.Symlink(agentDir, target)
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
		target := filepath.Join(homeRoot, claudeDir, "CLAUDE.md")
		if info, err := os.Lstat(target); err == nil && info.Mode()&os.ModeSymlink != 0 {
			continue // already a symlink
		}
		os.MkdirAll(filepath.Join(homeRoot, claudeDir), 0755)
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
		return emitRenderedHookFileToUserHomes(globalBundles, filepath.Join(claudeDir, claudeSettingsJSON), renderClaudeHookSettings)
	}

	spec := findClaudeSettingsHookSpec(agentsHome, "global")
	if spec == nil {
		for _, homeRoot := range config.UserHomeRoots() {
			_ = removeManagedFileIf(filepath.Join(homeRoot, claudeDir, claudeSettingsJSON), isLikelyRenderedClaudeHookSettings)
		}
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		target := filepath.Join(homeRoot, claudeDir, claudeSettingsJSON)
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
		userSkillsDir := filepath.Join(homeRoot, claudeDir, "skills")
		if err := syncScopedDirSymlinks(agentsHome, "skills", "global", "SKILL.md", userSkillsDir); err != nil {
			return err
		}
	}
	return nil
}

func (c *claude) createAgentsLinks(project, repoPath, agentsHome string) error {
	// Mirror ~/.agents/agents/<project>/<name>/ into the repo (same model as ensureUserSkills /
	// syncScopedDirSymlinks). Shared-target projection may already create `.claude/agents/*`;
	// this pass also ensures `.agents/agents/*` and heals incorrect symlinks idempotently.
	return syncScopedDirSymlinksTargets(agentsHome, "agents", project, "AGENT.md",
		filepath.Join(repoPath, ".agents", "agents"),
		filepath.Join(repoPath, claudeDir, "agents"),
	)
}

func (c *claude) createSkillsLinks(project, repoPath, agentsHome string) error {
	// Shared repo targets (.claude/skills/*, .agents/skills/*) are now written
	// by CollectAndExecuteSharedTargetPlan at the command layer before
	// CreateLinks is called. This method only handles user-home skill links.
	c.ensureUserSkills(agentsHome)
	return nil
}

func (c *claude) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	c.removeProjectRuleLinks(project, repoPath, agentsHome)
	c.removeProjectSettingsLink(project, repoPath, agentsHome)
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, ".mcp.json"), agentsHome)
	c.removeScopedDirLinks(filepath.Join(repoPath, claudeDir, "agents"), agentsHome)
	c.removeScopedDirLinks(filepath.Join(repoPath, claudeDir, "skills"), agentsHome)
	c.removeScopedDirLinks(filepath.Join(repoPath, ".agents", "skills"), agentsHome)
	return nil
}

func (c *claude) removeProjectRuleLinks(project, repoPath, agentsHome string) {
	rulesDir := filepath.Join(repoPath, claudeDir, "rules")
	if entries, err := os.ReadDir(rulesDir); err == nil {
		prefix := project + "--"
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), prefix) {
				linkPath := filepath.Join(rulesDir, e.Name())
				links.RemoveIfSymlinkUnder(linkPath, agentsHome)
			}
		}
	}
}

func (c *claude) removeProjectSettingsLink(project, repoPath, agentsHome string) {
	projectBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), project)
	if err == nil && len(projectBundles) > 0 {
		_ = removeManagedRenderedHookFile(projectBundles, filepath.Join(repoPath, claudeDir, claudeSettingsLocalJSON), renderClaudeHookSettings)
	} else {
		globalBundles, globalErr := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
		if globalErr == nil && len(globalBundles) > 0 {
			_ = removeManagedRenderedHookFile(globalBundles, filepath.Join(repoPath, claudeDir, claudeSettingsLocalJSON), renderClaudeHookSettings)
		}
	}
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, claudeDir, claudeSettingsLocalJSON), agentsHome)
}

func (c *claude) removeScopedDirLinks(dir, agentsHome string) {
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			links.RemoveIfSymlinkUnder(filepath.Join(dir, e.Name()), agentsHome)
		}
	}
}

func isClaudeAgentDir(path string) bool {
	if !links.IsDirEntry(path) {
		return false
	}
	_, err := os.Stat(filepath.Join(path, "AGENT.md"))
	return err == nil
}

func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func (c *claude) SharedTargetIntents(project string) ([]ResourceIntent, error) {
	skills, err := BuildSharedSkillMirrorIntents(project,
		filepath.Join(claudeDir, "skills"),
		filepath.Join(".agents", "skills"),
	)
	if err != nil {
		return nil, err
	}
	agents, err := BuildSharedAgentMirrorIntents(project, filepath.Join(claudeDir, "agents"))
	if err != nil {
		return nil, err
	}
	out := make([]ResourceIntent, 0, len(skills)+len(agents))
	out = append(out, skills...)
	out = append(out, agents...)
	return out, nil
}
