package platform

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
)

type copilot struct {
	io platformIO
}

const (
	copilotMCPJSON           = "mcp.json"
	copilotClaudeDir         = ".claude"
	copilotSettingsLocalJSON = "settings.local.json"
	copilotInstructionsMD    = "copilot-instructions.md"
	copilotGitHubDir         = ".github"
	copilotVSCodeDir         = ".vscode"
	copilotAgentsDir         = ".agents"
)

func NewCopilot() Platform { return &copilot{io: stdPlatformIO{}} }

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
	home, _ := config.UserHomeDir()
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
	return probeInstalled("copilot")
}

func (c *copilot) Version() string {
	home, _ := config.UserHomeDir()
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
	return probeVersionLine("copilot")
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
	if err := c.io.MkdirAll(filepath.Join(repoPath, copilotGitHubDir), 0755); err != nil {
		return err
	}
	// Managed-replace at a fixed owned path (.github/copilot-instructions.md).
	return links.SymlinkReplacing(src, filepath.Join(repoPath, copilotGitHubDir, copilotInstructionsMD), backupSidecar)
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
		if err := c.io.MkdirAll(filepath.Join(repoPath, copilotVSCodeDir), 0755); err != nil {
			return err
		}
		// Managed-replace at a fixed owned path (.vscode/mcp.json).
		return links.SymlinkReplacing(src, filepath.Join(repoPath, copilotVSCodeDir, copilotMCPJSON), backupSidecar)
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
	if err := c.io.MkdirAll(filepath.Join(repoPath, copilotClaudeDir), 0755); err != nil {
		return err
	}
	return emitPreferredHookFile(
		c.io,
		target,
		renderClaudeHookSettings,
		resolveHookSpec(agentsHome, []string{"hooks", "settings"}, project, "claude-code.json"),
		directSymlinkHookMode,
		func(p string) error { return removeRenderedClaudeHookSettings(c.io, p) },
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
	if err := emitRenderedHookFanout(c.io, specs, hooksDir, renderCopilotHookFile); err != nil {
		return err
	}
	wanted, err := renderedCopilotHookNames(specs)
	if err != nil {
		return err
	}
	return pruneManagedRenderedFanoutExtras(c.io, hooksDir, wanted, isLikelyRenderedCopilotHookFile)
}

func (c *copilot) emitLegacyProjectHookFiles(agentsHome, project, hooksDir string) error {
	specs, err := ListHookSpecs(agentsHome, project)
	if err != nil {
		return pruneManagedRenderedFanoutExtras(c.io, hooksDir, map[string]bool{}, isLikelyRenderedCopilotHookFile)
	}
	if err := emitHookFanout(c.io, specs, hooksDir, HookEmissionMode{
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
	return pruneManagedRenderedFanoutExtras(c.io, hooksDir, wanted, isLikelyRenderedCopilotHookFile)
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

	var errs []error
	errs = append(errs, c.removeTopLevelLinks(project, repoPath, agentsHome))
	errs = append(errs, c.removeClaudeCompatSettings(project, repoPath, agentsHome))
	errs = append(errs, c.removeSkillsLinks(repoPath, agentsHome))
	errs = append(errs, c.removeAgentLinks(project, repoPath, agentsHome))
	errs = append(errs, c.removeHookLinks(project, repoPath, agentsHome))
	return errors.Join(errs...)
}

func (c *copilot) removeTopLevelLinks(project, repoPath, agentsHome string) error {
	instr := filepath.Join(repoPath, copilotGitHubDir, copilotInstructionsMD)
	mcp := filepath.Join(repoPath, copilotVSCodeDir, copilotMCPJSON)
	return errors.Join(
		links.RemoveIfSymlinkUnder(instr, agentsHome),
		removeHardlinkedManaged(instr, copilotInstructionsSources(agentsHome, project)),
		links.RemoveIfSymlinkUnder(mcp, agentsHome),
		removeHardlinkedManaged(mcp, copilotMCPSources(agentsHome, project)),
	)
}

// copilotInstructionsSources mirrors resolveInstructionsSrc: every canonical
// path createInstructionsLink could have linked, so a Windows hard-linked
// managed instructions file is cleaned up alongside the symlink case.
func copilotInstructionsSources(agentsHome, project string) []string {
	srcs := []string{
		filepath.Join(agentsHome, "rules", project, copilotInstructionsMD),
		filepath.Join(agentsHome, "rules", "global", copilotInstructionsMD),
	}
	for _, scope := range []string{project, "global"} {
		for _, ext := range []string{"md", "mdc", "txt"} {
			srcs = append(srcs, filepath.Join(agentsHome, "rules", scope, "rules."+ext))
		}
	}
	return srcs
}

// copilotMCPSources mirrors createMCPLinks' resolveScopedFile call.
func copilotMCPSources(agentsHome, project string) []string {
	var srcs []string
	for _, scope := range scopedNames(project) {
		for _, name := range []string{"copilot.json", copilotMCPJSON} {
			srcs = append(srcs, filepath.Join(agentsHome, "mcp", scope, name))
		}
	}
	return srcs
}

func (c *copilot) removeClaudeCompatSettings(project, repoPath, agentsHome string) error {
	projectBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), project)
	if err == nil && len(projectBundles) > 0 {
		_ = removeManagedRenderedHookFile(c.io, projectBundles, filepath.Join(repoPath, copilotClaudeDir, copilotSettingsLocalJSON), renderClaudeHookSettings)
	} else {
		globalBundles, globalErr := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
		if globalErr == nil && len(globalBundles) > 0 {
			_ = removeManagedRenderedHookFile(c.io, globalBundles, filepath.Join(repoPath, copilotClaudeDir, copilotSettingsLocalJSON), renderClaudeHookSettings)
		}
	}
	return links.RemoveIfSymlinkUnder(filepath.Join(repoPath, copilotClaudeDir, copilotSettingsLocalJSON), agentsHome)
}

func (c *copilot) removeSkillsLinks(repoPath, agentsHome string) error {
	skillsDir := filepath.Join(repoPath, copilotAgentsDir, "skills")
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil
	}
	var errs []error
	for _, e := range entries {
		errs = append(errs, links.RemoveIfSymlinkUnder(filepath.Join(skillsDir, e.Name()), agentsHome))
	}
	return errors.Join(errs...)
}

func (c *copilot) removeAgentLinks(project, repoPath, agentsHome string) error {
	const suffix = ".agent.md"
	agentsDir := filepath.Join(repoPath, copilotGitHubDir, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil
	}
	var errs []error
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		dst := filepath.Join(agentsDir, e.Name())
		name := strings.TrimSuffix(e.Name(), suffix)
		errs = append(errs,
			links.RemoveIfSymlinkUnder(dst, agentsHome),
			removeHardlinkedManaged(dst, scopedAgentFileSources(agentsHome, project, name, suffix)),
		)
	}
	return errors.Join(errs...)
}

func (c *copilot) removeHookLinks(project, repoPath, agentsHome string) error {
	hooksDir := filepath.Join(repoPath, copilotGitHubDir, "hooks")
	canonicalSpecs, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global", project)
	if err == nil && len(canonicalSpecs) > 0 {
		_ = removeManagedRenderedHookFanout(c.io, canonicalSpecs, hooksDir, renderCopilotHookFile)
	}
	entries, rdErr := os.ReadDir(hooksDir)
	if rdErr != nil {
		return nil
	}
	var errs []error
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		dst := filepath.Join(hooksDir, e.Name())
		errs = append(errs,
			links.RemoveIfSymlinkUnder(dst, agentsHome),
			removeHardlinkedManaged(dst, scopedBucketFileSources(agentsHome, "hooks", project, e.Name())),
		)
	}
	return errors.Join(errs...)
}

// BrokenLinks implements BrokenLinkReporter for the copilot platform.
//
// Copilot owns two project-scope single-file managed links:
//
//  1. .github/copilot-instructions.md — managed symlink to the highest-
//     priority canonical source under <agentsHome>/rules/ (see
//     createInstructionsLink / resolveInstructionsSrc).
//  2. .vscode/mcp.json — managed symlink to the canonical mcp source
//     resolved by createMCPLinks.
//
// The diagnostic contract preserved from doctor's previous inline
// projectSingleFiles table is "report broken only when the entry is a
// resolvable managed link AND its target is missing" — a hard-linked or
// absent file is silently passed over. classifyManagedLink encodes that
// semantic exactly, matching the claude.brokenMCPLink shape used for the
// equivalent claude .mcp.json single-file entry.
//
// PlatformID is set on every returned BrokenLink so JSON consumers can
// self-describe per-entry.
func (c *copilot) BrokenLinks(_, repoPath, _ string) []BrokenLink {
	var broken []BrokenLink
	broken = append(broken, classifyCopilotSingleFile(filepath.Join(repoPath, copilotGitHubDir, copilotInstructionsMD))...)
	broken = append(broken, classifyCopilotSingleFile(filepath.Join(repoPath, copilotVSCodeDir, copilotMCPJSON))...)
	return broken
}

// classifyCopilotSingleFile mirrors classifyCodexSingleFile for the two
// copilot-owned single-file managed paths. Extracted so BrokenLinks remains
// a flat composition and the per-entry semantic stays identical.
func classifyCopilotSingleFile(linkPath string) []BrokenLink {
	state, raw := classifyManagedLink(linkPath)
	if state != linkStateBroken {
		return nil
	}
	return []BrokenLink{{
		PlatformID:  "copilot",
		LinkPath:    linkPath,
		Dest:        raw,
		DisplayDest: config.DisplayPath(absolutizeDest(linkPath, raw)),
	}}
}

func (c *copilot) SharedTargetIntents(project string) ([]ResourceIntent, error) {
	skills, err := BuildSharedSkillMirrorIntents(project, filepath.Join(copilotAgentsDir, "skills"))
	if err != nil {
		return nil, err
	}
	agents, err := BuildSharedAgentFileSymlinkIntents(project, filepath.Join(copilotGitHubDir, "agents"), ".agent.md")
	if err != nil {
		return nil, err
	}
	return append(skills, agents...), nil
}

// CountLinks implements LinkCounter for the copilot platform: returns the
// (ok, broken) tally of managed links under the project's repo. Mirrors the
// per-platform inline counter that previously lived in status.go's
// copilotTextBadge.
//
// Healthy: .github/copilot-instructions.md, .vscode/mcp.json, and (yes —
// shared with claude) .claude/settings.local.json when each is a resolvable
// managed link, plus any resolvable entry under .github/agents/,
// .github/hooks/, or .agents/skills/. Broken: a resolvable managed link
// whose target is missing.
func (c *copilot) CountLinks(_, repoPath, _ string) (ok, broken int) {
	addManagedFileCounts(&ok, &broken, []string{
		filepath.Join(repoPath, copilotGitHubDir, copilotInstructionsMD),
		filepath.Join(repoPath, copilotVSCodeDir, copilotMCPJSON),
		filepath.Join(repoPath, copilotClaudeDir, copilotSettingsLocalJSON),
	})
	addManagedDirCounts(&ok, &broken, []string{
		filepath.Join(repoPath, copilotGitHubDir, "agents"),
		filepath.Join(repoPath, copilotGitHubDir, "hooks"),
		filepath.Join(repoPath, copilotAgentsDir, "skills"),
	})
	return ok, broken
}

// Badge implements StatusBadger for the copilot platform.
func (c *copilot) Badge(project, repoPath, agentsHome string) PlatformBadge {
	ok, broken := c.CountLinks(project, repoPath, agentsHome)
	return PlatformBadge{Name: "Copilot", Present: ok > 0, Broken: broken > 0}
}

// Copilot DOES have a documented user-config layer —
// $HOME/.copilot/copilot-instructions.md, ~/.copilot/skills/,
// ~/.copilot/agents/, ~/.copilot/hooks/ (or $COPILOT_HOME/hooks/), and
// ~/.copilot/mcp-config.json (see PLATFORM_DIRS_DOCS). dot-agents does NOT yet
// wire any copilot user-scope target, so there are no managed user-home links
// to report today (the user-scope ~/.copilot/hooks/ gap is tracked in
// PLATFORM_DIRS_DOCS "User-home wiring"). We still implement UserConfigReporter
// so copilot opts into the user-config diagnostics path cleanly the moment
// user-scope wiring lands — until then both methods report an empty/clean
// surface, which appendPlatformIfPresent filters out so JSON/text output is
// unchanged.
func (c *copilot) UserBrokenLinks(_ string) []BrokenLink {
	return nil
}

// UserBadge implements UserConfigReporter for the copilot platform. See
// UserBrokenLinks: copilot's user-config layer exists but is not yet wired by
// dot-agents, so the badge reports no managed user-home state. Present stays
// false (filtered out of the status badge list) until user-scope wiring lands.
func (c *copilot) UserBadge(_ string) PlatformBadge {
	return PlatformBadge{Name: "Copilot"}
}
