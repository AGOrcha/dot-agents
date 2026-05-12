package platform

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
)

type copilot struct{}

const (
	copilotMCPJSON           = "mcp.json"
	copilotClaudeDir         = ".claude"
	copilotSettingsLocalJSON = "settings.local.json"
	copilotInstructionsMD    = "copilot-instructions.md"
	copilotGitHubDir         = ".github"
	copilotVSCodeDir         = ".vscode"
)

func NewCopilot() Platform { return &copilot{} }

func (c *copilot) ID() string          { return "copilot" }
func (c *copilot) DisplayName() string { return "GitHub Copilot" }

// SessionReader — no env var injects session ID during a Copilot CLI run.
func (c *copilot) AIAgentPrefix() string              { return "copilot" }
func (c *copilot) SessionEnvs() []string              { return nil }
func (c *copilot) EntrypointEnvs() []string           { return nil }
func (c *copilot) ResolveModel(_, _, _ string) string { return "" }

// StatsReader — Copilot CLI session stats not exposed as a queryable local file.
func (c *copilot) ReadUsageStats(_ string) *PlatformUsageStats { return nil }

// SessionTokenScanner implementation.
// Scans ~/.copilot/session-state/*/events.jsonl for session.shutdown events,
// which carry per-model aggregate token totals. Per-turn counts are ephemeral
// in the Copilot CLI (kept in memory for /usage only, never written to disk).
// sessionID and projectPath are ignored — Copilot CLI publishes no session ID
// env var; filtering is by events.jsonl mtime > afterTimestamp.
func (c *copilot) ScanSessionTokens(home, _, _, afterTimestamp string) SessionTokenMetrics {
	return copilotScanSessionTokens(home, afterTimestamp)
}

// copilotScanSessionTokens walks ~/.copilot/session-state/ for events.jsonl
// files modified after afterTimestamp and sums the modelMetrics from
// session.shutdown events. Fields are camelCase: inputTokens, outputTokens,
// cacheReadTokens, cacheWriteTokens.
func copilotScanSessionTokens(home, afterTimestamp string) SessionTokenMetrics {
	stateDir := filepath.Join(home, ".copilot", "session-state")
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		return SessionTokenMetrics{}
	}

	var after time.Time
	if afterTimestamp != "" {
		after, _ = time.Parse(time.RFC3339, afterTimestamp)
	}

	var m SessionTokenMetrics
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		eventsPath := filepath.Join(stateDir, e.Name(), "events.jsonl")
		info, err := os.Stat(eventsPath)
		if err != nil {
			continue
		}
		if !after.IsZero() && !info.ModTime().After(after) {
			continue
		}
		copilotAccumulateShutdownTokens(eventsPath, &m)
	}
	return m
}

// copilotAccumulateShutdownTokens reads a Copilot events.jsonl and adds the
// modelMetrics from any session.shutdown event into m.
func copilotAccumulateShutdownTokens(path string, m *SessionTokenMetrics) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"session.shutdown"`)) {
			continue
		}
		var event struct {
			Type string `json:"type"`
			Data struct {
				ModelMetrics map[string]struct {
					Usage struct {
						InputTokens      int `json:"inputTokens"`
						OutputTokens     int `json:"outputTokens"`
						CacheReadTokens  int `json:"cacheReadTokens"`
						CacheWriteTokens int `json:"cacheWriteTokens"`
						ReasoningTokens  int `json:"reasoningTokens"`
					} `json:"usage"`
				} `json:"modelMetrics"`
			} `json:"data"`
		}
		if err := json.Unmarshal(line, &event); err != nil || event.Type != "session.shutdown" {
			continue
		}
		for _, mm := range event.Data.ModelMetrics {
			m.InputTokens += mm.Usage.InputTokens
			m.OutputTokens += mm.Usage.OutputTokens
			m.CacheReadTokens += mm.Usage.CacheReadTokens
			m.CacheCreationTokens += mm.Usage.CacheWriteTokens
			m.ReasoningTokens += mm.Usage.ReasoningTokens
			m.MessageCount++
		}
	}
}

func (c *copilot) IsInstalled() bool {
	home, _ := os.UserHomeDir()
	for _, dir := range []string{
		filepath.Join(home, copilotVSCodeDir, "extensions"),
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
		filepath.Join(home, copilotVSCodeDir, "extensions"),
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
		filepath.Join(agentsHome, "rules", project, copilotInstructionsMD),
		filepath.Join(agentsHome, "rules", "global", copilotInstructionsMD),
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
	if err := os.MkdirAll(filepath.Join(repoPath, copilotGitHubDir), 0755); err != nil {
		return err
	}
	links.Symlink(src, filepath.Join(repoPath, copilotGitHubDir, copilotInstructionsMD))
	return nil
}

func (c *copilot) createSkillsLinks(project, repoPath, _ string) error {
	return nil
}

func (c *copilot) createAgentsLinks(project, repoPath, agentsHome string) error {
	// `.github/agents/*.agent.md` — symlinked from canonical AGENT.md by CollectAndExecuteSharedTargetPlan
	return nil
}

func (c *copilot) createMCPLinks(project, repoPath, agentsHome string) error {
	if src := resolveScopedFile(agentsHome, "mcp", project, "copilot.json", copilotMCPJSON); src != "" {
		if err := os.MkdirAll(filepath.Join(repoPath, copilotVSCodeDir), 0755); err != nil {
			return err
		}
		links.Symlink(src, filepath.Join(repoPath, copilotVSCodeDir, copilotMCPJSON))
	}
	return nil
}

func (c *copilot) createClaudeCompatLinks(project, repoPath, agentsHome string) error {
	target := filepath.Join(repoPath, copilotClaudeDir, copilotSettingsLocalJSON)
	projectBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), project)
	if err != nil {
		return err
	}
	globalBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(repoPath, copilotClaudeDir), 0755); err != nil {
		return err
	}
	return emitPreferredHookFile(
		target,
		renderClaudeHookSettings,
		resolveHookSpec(agentsHome, []string{"hooks", "settings"}, project, "claude-code.json"),
		directSymlinkHookMode,
		removeRenderedClaudeHookSettings,
		projectBundles,
		globalBundles,
	)
}

func (c *copilot) createProjectHookFiles(project, repoPath, agentsHome string) error {
	hooksDir := filepath.Join(repoPath, copilotGitHubDir, "hooks")
	canonicalSpecs, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global", project)
	if err != nil {
		return err
	}
	if len(canonicalSpecs) > 0 {
		return c.emitCanonicalProjectHookFiles(canonicalSpecs, hooksDir)
	}

	return c.emitLegacyProjectHookFiles(agentsHome, project, hooksDir)
}

func (c *copilot) emitCanonicalProjectHookFiles(specs []HookSpec, hooksDir string) error {
	if err := emitRenderedHookFanout(specs, hooksDir, renderCopilotHookFile); err != nil {
		return err
	}
	wanted, err := renderedCopilotHookNames(specs)
	if err != nil {
		return err
	}
	return pruneManagedRenderedFanoutExtras(hooksDir, wanted, isLikelyRenderedCopilotHookFile)
}

func (c *copilot) emitLegacyProjectHookFiles(agentsHome, project, hooksDir string) error {
	specs, err := ListHookSpecs(agentsHome, project)
	if err != nil {
		return pruneManagedRenderedFanoutExtras(hooksDir, map[string]bool{}, isLikelyRenderedCopilotHookFile)
	}
	if err := emitHookFanout(specs, hooksDir, HookEmissionMode{
		Shape:     HookShapeRenderFanout,
		Transport: HookTransportSymlink,
	}, func(spec HookSpec) (string, bool) {
		if spec.Name == "cursor" || spec.Name == "claude-code" {
			return "", false
		}
		return spec.Name + ".json", true
	}); err != nil {
		return err
	}
	wanted := legacyCopilotHookNames(specs)
	return pruneManagedRenderedFanoutExtras(hooksDir, wanted, isLikelyRenderedCopilotHookFile)
}

func renderedCopilotHookNames(specs []HookSpec) (map[string]bool, error) {
	wanted := map[string]bool{}
	for _, spec := range specs {
		name, _, ok, renderErr := renderCopilotHookFile(spec)
		if renderErr != nil {
			return nil, renderErr
		}
		if ok {
			wanted[name] = true
		}
	}
	return wanted, nil
}

func legacyCopilotHookNames(specs []HookSpec) map[string]bool {
	wanted := map[string]bool{}
	for _, spec := range specs {
		if spec.Name == "cursor" || spec.Name == "claude-code" {
			continue
		}
		wanted[spec.Name+".json"] = true
	}
	return wanted
}

func (c *copilot) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()

	c.removeTopLevelLinks(repoPath, agentsHome)
	c.removeClaudeCompatSettings(project, repoPath, agentsHome)
	c.removeSkillsLinks(repoPath, agentsHome)
	c.removeAgentLinks(repoPath, agentsHome)
	c.removeHookLinks(project, repoPath, agentsHome)
	return nil
}

func (c *copilot) removeTopLevelLinks(repoPath, agentsHome string) {
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, copilotGitHubDir, copilotInstructionsMD), agentsHome)
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, copilotVSCodeDir, copilotMCPJSON), agentsHome)
}

func (c *copilot) removeClaudeCompatSettings(project, repoPath, agentsHome string) {
	projectBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), project)
	if err == nil && len(projectBundles) > 0 {
		_ = removeManagedRenderedHookFile(projectBundles, filepath.Join(repoPath, copilotClaudeDir, copilotSettingsLocalJSON), renderClaudeHookSettings)
	} else {
		globalBundles, globalErr := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
		if globalErr == nil && len(globalBundles) > 0 {
			_ = removeManagedRenderedHookFile(globalBundles, filepath.Join(repoPath, copilotClaudeDir, copilotSettingsLocalJSON), renderClaudeHookSettings)
		}
	}
	links.RemoveIfSymlinkUnder(filepath.Join(repoPath, copilotClaudeDir, copilotSettingsLocalJSON), agentsHome)
}

func (c *copilot) removeSkillsLinks(repoPath, agentsHome string) {
	skillsDir := filepath.Join(repoPath, ".agents", "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			links.RemoveIfSymlinkUnder(filepath.Join(skillsDir, e.Name()), agentsHome)
		}
	}
}

func (c *copilot) removeAgentLinks(repoPath, agentsHome string) {
	agentsDir := filepath.Join(repoPath, copilotGitHubDir, "agents")
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".agent.md") {
				links.RemoveIfSymlinkUnder(filepath.Join(agentsDir, e.Name()), agentsHome)
			}
		}
	}
}

func (c *copilot) removeHookLinks(project, repoPath, agentsHome string) {
	hooksDir := filepath.Join(repoPath, copilotGitHubDir, "hooks")
	canonicalSpecs, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global", project)
	if err == nil && len(canonicalSpecs) > 0 {
		_ = removeManagedRenderedHookFanout(canonicalSpecs, hooksDir, renderCopilotHookFile)
	}
	if entries, err := os.ReadDir(hooksDir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".json") {
				links.RemoveIfSymlinkUnder(filepath.Join(hooksDir, e.Name()), agentsHome)
			}
		}
	}
}

func (c *copilot) SharedTargetIntents(project string) ([]ResourceIntent, error) {
	skills, err := BuildSharedSkillMirrorIntents(project, filepath.Join(".agents", "skills"))
	if err != nil {
		return nil, err
	}
	agents, err := BuildSharedAgentFileSymlinkIntents(project, filepath.Join(copilotGitHubDir, "agents"), ".agent.md")
	if err != nil {
		return nil, err
	}
	return append(skills, agents...), nil
}
