package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
)

type cursor struct{}

const (
	cursorHooksFile   = "hooks.json"
	cursorJSON        = "cursor.json"
	cursorDir         = ".cursor"
	globalRulesPrefix = "global--"
)

func NewCursor() Platform { return &cursor{} }

func (c *cursor) ID() string          { return "cursor" }
func (c *cursor) DisplayName() string { return "Cursor" }

func (c *cursor) IsInstalled() bool {
	if _, err := os.Stat("/Applications/Cursor.app"); err == nil {
		return true
	}
	_, err := exec.LookPath("cursor")
	return err == nil
}

func (c *cursor) Version() string {
	// Try app version on macOS
	if _, err := os.Stat("/Applications/Cursor.app"); err == nil {
		out, err := exec.Command("defaults", "read",
			"/Applications/Cursor.app/Contents/Info.plist",
			"CFBundleShortVersionString").Output()
		if err == nil {
			appVer := strings.TrimSpace(string(out))
			if path, err := exec.LookPath("cursor"); err == nil {
				cliOut, err := exec.Command(path, "--version").Output()
				if err == nil {
					cliVer := strings.TrimSpace(strings.Split(string(cliOut), "\n")[0])
					return appVer + " (CLI: " + cliVer + ")"
				}
			}
			return appVer + " (App)"
		}
	}
	if path, err := exec.LookPath("cursor"); err == nil {
		out, err := exec.Command(path, "--version").Output()
		if err == nil {
			return strings.TrimSpace(strings.Split(string(out), "\n")[0])
		}
	}
	return ""
}

func (c *cursor) HasDeprecatedFormat(repoPath string) bool {
	_, err := os.Stat(filepath.Join(repoPath, ".cursorrules"))
	return err == nil
}

func (c *cursor) DeprecatedDetails(repoPath string) string {
	if c.HasDeprecatedFormat(repoPath) {
		return ".cursorrules → .cursor/rules/*.mdc"
	}
	return ""
}

func (c *cursor) CreateLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	if err := c.createRuleLinks(project, repoPath, agentsHome); err != nil {
		return err
	}
	if err := c.createSettingsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}
	if err := c.createMCPLinks(project, repoPath, agentsHome); err != nil {
		return err
	}
	if err := c.createIgnoreLink(project, repoPath, agentsHome); err != nil {
		return err
	}
	if err := c.createAgentsLinks(project, repoPath, agentsHome); err != nil {
		return err
	}
	if err := c.createHooksLinks(project, repoPath, agentsHome); err != nil {
		return err
	}
	return nil
}

func (c *cursor) createRuleLinks(project, repoPath, agentsHome string) error {
	rulesDir := filepath.Join(repoPath, cursorDir, "rules")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		return err
	}
	desired := map[string]string{}
	c.collectRuleLinks(filepath.Join(agentsHome, "rules", "global"), globalRulesPrefix, desired)
	c.collectRuleLinks(filepath.Join(agentsHome, "rules", project), project+"--", desired)
	if err := c.pruneRuleLinks(rulesDir, project, desired); err != nil {
		return err
	}
	for target, src := range desired {
		links.Hardlink(src, filepath.Join(rulesDir, target)) // best-effort
	}
	return nil
}

func (c *cursor) collectRuleLinks(sourceDir, prefix string, desired map[string]string) {
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		c.collectRuleEntry(entry, sourceDir, prefix, desired)
	}
}

func (c *cursor) collectRuleEntry(entry os.DirEntry, sourceDir, prefix string, desired map[string]string) {
	if entry.IsDir() {
		return
	}
	name := entry.Name()
	if !isCursorRuleFile(name) {
		return
	}
	desired[prefix+toMDC(name)] = filepath.Join(sourceDir, name)
}

func (c *cursor) pruneRuleLinks(rulesDir, project string, desired map[string]string) error {
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasPrefix(name, globalRulesPrefix) && !strings.HasPrefix(name, project+"--") {
			continue
		}
		if _, ok := desired[name]; ok {
			continue
		}
		if err := os.Remove(filepath.Join(rulesDir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (c *cursor) createSettingsLinks(project, repoPath, agentsHome string) error {
	if err := os.MkdirAll(filepath.Join(repoPath, cursorDir), 0755); err != nil {
		return err
	}
	if src := resolveScopedFile(agentsHome, "settings", project, cursorJSON); src != "" {
		dst := filepath.Join(repoPath, cursorDir, "settings.json")
		links.Hardlink(src, dst) // best-effort
	}
	return nil
}

func (c *cursor) createMCPLinks(project, repoPath, agentsHome string) error {
	if err := os.MkdirAll(filepath.Join(repoPath, cursorDir), 0755); err != nil {
		return err
	}
	if src := resolveScopedFile(agentsHome, "mcp", project, cursorJSON, "mcp.json"); src != "" {
		dst := filepath.Join(repoPath, cursorDir, "mcp.json")
		links.Hardlink(src, dst) // best-effort
	}
	return nil
}

func (c *cursor) createIgnoreLink(project, repoPath, agentsHome string) error {
	if src := resolveScopedFile(agentsHome, "settings", project, "cursorignore"); src != "" {
		dst := filepath.Join(repoPath, ".cursorignore")
		links.Hardlink(src, dst) // best-effort
	}
	return nil
}

func (c *cursor) createHooksLinks(project, repoPath, agentsHome string) error {
	if err := c.writeRepoHooks(project, repoPath, agentsHome); err != nil {
		return err
	}
	return c.writeUserHomeHooks(project, agentsHome)
}

func (c *cursor) writeRepoHooks(project, repoPath, agentsHome string) error {
	repoTarget := filepath.Join(repoPath, cursorDir, cursorHooksFile)
	repoBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global", project)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(repoPath, cursorDir), 0755); err != nil {
		return err
	}
	return emitPreferredHookFile(
		repoTarget,
		renderCursorHookConfig,
		resolveHookSpec(agentsHome, []string{"hooks"}, project, cursorJSON),
		directHardlinkHookMode,
		removeRenderedCursorHookConfig,
		repoBundles,
	)
}

func (c *cursor) writeUserHomeHooks(project, agentsHome string) error {
	globalBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
	if err != nil {
		return err
	}
	return emitPreferredHookFileToUserHomes(
		filepath.Join(cursorDir, cursorHooksFile),
		renderCursorHookConfig,
		resolveHookSpecInScope(agentsHome, []string{"hooks"}, "global", cursorJSON),
		directHardlinkHookMode,
		removeRenderedCursorHookConfig,
		globalBundles,
	)
}

func (c *cursor) createAgentsLinks(_ string, _ string, _ string) error {
	// `.claude/agents/*` mirrors match Claude's layout; command layer runs CollectAndExecuteSharedTargetPlan first.
	return nil
}

func (c *cursor) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()
	c.removeRuleLinks(project, repoPath, agentsHome)
	c.removeHooksLink(project, repoPath, agentsHome)
	c.removeAgentLinks(repoPath, agentsHome)

	return nil
}

func (c *cursor) removeRuleLinks(project, repoPath, agentsHome string) {
	rulesDir := filepath.Join(repoPath, cursorDir, "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		c.removeRuleEntry(entry, rulesDir, project, agentsHome)
	}
}

func (c *cursor) removeRuleEntry(entry os.DirEntry, rulesDir, project, agentsHome string) {
	if entry.IsDir() {
		return
	}
	name := entry.Name()
	filePath := filepath.Join(rulesDir, name)

	switch {
	case strings.HasPrefix(name, globalRulesPrefix):
		removeHardlinkIfLinkedToAny(filePath, cursorRuleSources(agentsHome, "global", strings.TrimPrefix(name, globalRulesPrefix)))
	case strings.HasPrefix(name, project+"--"):
		removeHardlinkIfLinkedToAny(filePath, cursorRuleSources(agentsHome, project, strings.TrimPrefix(name, project+"--")))
	}
}

func (c *cursor) removeHooksLink(project, repoPath, agentsHome string) {
	hooksFilePath := filepath.Join(repoPath, cursorDir, cursorHooksFile)
	repoBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global", project)
	if err == nil && len(repoBundles) > 0 {
		_ = removeManagedRenderedHookFile(repoBundles, hooksFilePath, renderCursorHookConfig)
	}
	removeHardlinkIfLinkedToAny(hooksFilePath, []string{
		filepath.Join(agentsHome, "hooks", project, cursorJSON),
		filepath.Join(agentsHome, "hooks", "global", cursorJSON),
	})
}

func (c *cursor) removeAgentLinks(repoPath, agentsHome string) {
	agentsTarget := filepath.Join(repoPath, cursorDir, "agents")
	entries, err := os.ReadDir(agentsTarget)
	if err != nil {
		return
	}
	for _, entry := range entries {
		links.RemoveIfSymlinkUnder(filepath.Join(agentsTarget, entry.Name()), agentsHome)
	}
}

// toMDC converts .md extension to .mdc; leaves .mdc unchanged.
func toMDC(name string) string {
	if strings.HasSuffix(name, ".md") && !strings.HasSuffix(name, ".mdc") {
		return strings.TrimSuffix(name, ".md") + ".mdc"
	}
	return name
}

func isCursorRuleFile(name string) bool {
	return strings.HasSuffix(name, ".mdc") || strings.HasSuffix(name, ".md")
}

func cursorRuleSources(agentsHome, scope, name string) []string {
	return []string{
		filepath.Join(agentsHome, "rules", scope, name),
		filepath.Join(agentsHome, "rules", scope, strings.TrimSuffix(name, ".mdc")+".md"),
	}
}

func removeHardlinkIfLinkedToAny(path string, sources []string) bool {
	for _, src := range sources {
		if linked, _ := links.AreHardlinked(path, src); linked {
			_ = os.Remove(path)
			return true
		}
	}
	return false
}

func (c *cursor) SharedTargetIntents(project string) ([]ResourceIntent, error) {
	// Same repo-relative targets as Claude so duplicate intents merge in the shared plan.
	return BuildSharedAgentMirrorIntents(project, filepath.Join(".claude", "agents"))
}
