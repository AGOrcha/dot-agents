package platform

import (
	"bufio"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
)

type claude struct {
	io platformIO
}

const (
	claudeCodeJSON          = "claude-code.json"
	claudeSettingsJSON      = "settings.json"
	claudeSettingsLocalJSON = "settings.local.json"
	claudeDir               = ".claude"
	claudeAgentsBucketDir   = ".agents"
	claudeMCPFile           = ".mcp.json"
)

func NewClaude() Platform { return &claude{io: stdPlatformIO{}} }

func (c *claude) ID() string          { return "claude" }
func (c *claude) DisplayName() string { return "Claude Code" }

// SessionReader implementation — confirmed env var contract as of Claude Code 2.x.
func (c *claude) AIAgentPrefix() string    { return "claude-code" }
func (c *claude) SessionEnvs() []string    { return []string{"CLAUDE_CODE_SESSION_ID"} }
func (c *claude) EntrypointEnvs() []string { return []string{"CLAUDE_CODE_ENTRYPOINT"} }
func (c *claude) ResolveModel(home, projectPath, sessionID string) string {
	return resolveClaudeCodeModelFromJSONL(home, projectPath, sessionID)
}

// StatsReader implementation.
func (c *claude) ReadUsageStats(home string) *PlatformUsageStats {
	return claudeReadUsageStats(home)
}

func claudeReadUsageStats(home string) *PlatformUsageStats {
	data, err := os.ReadFile(filepath.Join(home, claudeDir, "stats-cache.json"))
	if err != nil {
		return nil
	}
	var raw struct {
		TotalSessions int `json:"totalSessions"`
		TotalMessages int `json:"totalMessages"`
		ModelUsage    map[string]struct {
			InputTokens              int `json:"inputTokens"`
			OutputTokens             int `json:"outputTokens"`
			CacheReadInputTokens     int `json:"cacheReadInputTokens"`
			CacheCreationInputTokens int `json:"cacheCreationInputTokens"`
		} `json:"modelUsage"`
		DailyActivity []struct {
			Date          string `json:"date"`
			MessageCount  int    `json:"messageCount"`
			SessionCount  int    `json:"sessionCount"`
			ToolCallCount int    `json:"toolCallCount"`
		} `json:"dailyActivity"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	stats := &PlatformUsageStats{
		PlatformID:    "claude",
		TotalSessions: raw.TotalSessions,
		TotalMessages: raw.TotalMessages,
		TokensByModel: make(map[string]ModelTokenUsage, len(raw.ModelUsage)),
	}
	for model, u := range raw.ModelUsage {
		stats.TokensByModel[model] = ModelTokenUsage{
			InputTokens:              u.InputTokens,
			OutputTokens:             u.OutputTokens,
			CacheReadInputTokens:     u.CacheReadInputTokens,
			CacheCreationInputTokens: u.CacheCreationInputTokens,
		}
	}
	// Last 10 daily activity entries.
	start := 0
	if len(raw.DailyActivity) > 10 {
		start = len(raw.DailyActivity) - 10
	}
	for _, d := range raw.DailyActivity[start:] {
		stats.DailyActivity = append(stats.DailyActivity, DailyUsage{
			Date:          d.Date,
			MessageCount:  d.MessageCount,
			SessionCount:  d.SessionCount,
			ToolCallCount: d.ToolCallCount,
		})
	}
	return stats
}

// BranchSessionFinder implementation.
func (c *claude) FindSessionsOnBranch(home, projectPath, branch string, maxResults int) []BranchSessionInfo {
	return claudeFindSessionsOnBranch(home, projectPath, branch, maxResults)
}

func claudeFindSessionsOnBranch(home, projectPath, branch string, maxResults int) []BranchSessionInfo {
	slug := strings.ReplaceAll(projectPath, "/", "-")
	projectsDir := filepath.Join(home, claudeDir, "projects", slug)
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil
	}

	// Sort by mtime descending — most recent JSONL files first.
	type fileEntry struct {
		name  string
		mtime int64
	}
	var files []fileEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fileEntry{e.Name(), info.ModTime().UnixNano()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mtime > files[j].mtime })

	branchMarker := `"gitBranch":"` + branch + `"`
	var results []BranchSessionInfo
	scanned := 0
	for _, fe := range files {
		if scanned >= 20 || len(results) >= maxResults {
			break
		}
		scanned++
		path := filepath.Join(projectsDir, fe.name)
		info := claudeScanJSONLForBranch(path, branchMarker, branch)
		if info == nil {
			continue
		}
		results = append(results, *info)
	}
	return results
}

// claudeGitBranchEntry is the JSONL entry shape used to identify branch and session.
type claudeGitBranchEntry struct {
	SessionID string `json:"sessionId"`
	UUID      string `json:"uuid"`
	Timestamp string `json:"timestamp"`
	GitBranch string `json:"gitBranch"`
}

// claudeExtractBranchSession parses a JSONL line that matched the branch marker
// and returns the session ID and truncated timestamp, or empty strings on mismatch.
func claudeExtractBranchSession(line, branch string) (sessionID, timestamp string) {
	var entry claudeGitBranchEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return "", ""
	}
	if entry.GitBranch != branch {
		return "", ""
	}
	sid := entry.SessionID
	if sid == "" {
		sid = entry.UUID
	}
	if sid == "" {
		return "", ""
	}
	ts := entry.Timestamp
	if len(ts) > 16 {
		ts = ts[:16] + "Z"
	}
	return sid, ts
}

func claudeScanJSONLForBranch(path, branchMarker, branch string) *BranchSessionInfo {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var info BranchSessionInfo
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 512*1024), 512*1024)
	lineN := 0
	assistantLines := 0
	for sc.Scan() {
		lineN++
		if lineN > 50 {
			break
		}
		line := sc.Text()
		if strings.Contains(line, "assistant") {
			assistantLines++
		}
		if !strings.Contains(line, branchMarker) {
			continue
		}
		// Substring match — confirm the top-level gitBranch field actually equals
		// branch before trusting it. The marker can appear inside quoted message
		// content (e.g. an assistant pasting prior tool output), which would
		// otherwise yield false positives.
		sid, ts := claudeExtractBranchSession(line, branch)
		if sid == "" {
			continue
		}
		info.SessionID = sid
		info.Timestamp = ts
	}
	if info.SessionID == "" {
		return nil
	}
	info.MessageCount = assistantLines
	return &info
}

// SessionTokenScanner implementation.
func (c *claude) ScanSessionTokens(home, projectPath, sessionID, afterTimestamp string) SessionTokenMetrics {
	return claudeScanSessionTokens(home, projectPath, sessionID, afterTimestamp)
}

func (c *claude) IsInstalled() bool {
	// Detect by the actual CLI on PATH, not by the presence of ~/.claude: the
	// config dir persists after uninstall (and `da` itself creates it), so it's a
	// false-positive signal for "the tool is installed".
	return probeInstalled("claude")
}

func (c *claude) Version() string {
	return probeVersionLine("claude")
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
	if err := c.linkProjectMCP(project, repoPath, agentsHome); err != nil {
		return err
	}

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
	return c.io.MkdirAll(filepath.Join(repoPath, claudeDir, "rules"), 0755)
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
		c.io,
		target,
		renderClaudeHookSettings,
		findClaudeSettingsHookSpec(agentsHome, project),
		directSymlinkHookMode,
		func(p string) error { return removeRenderedClaudeHookSettings(c.io, p) },
		projectBundles,
		globalBundles,
	)
}

func (c *claude) linkProjectMCP(project, repoPath, agentsHome string) error {
	if src := resolveScopedFile(agentsHome, "mcp", project, "claude.json", "mcp.json"); src != "" {
		// Managed-replace at a fixed owned repo path (.mcp.json).
		return links.SymlinkReplacing(src, filepath.Join(repoPath, claudeMCPFile), backupSidecar)
	}
	return nil
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
		// Managed-replace: per-project rule symlinks this platform prunes and
		// re-emits every refresh under .claude/rules/. Stale managed symlink →
		// idempotent re-point; a genuine user file → preserved as
		// <name>.dot-agents-backup.
		if err := links.SymlinkReplacing(src, filepath.Join(rulesDir, name), backupSidecar); err != nil {
			return err
		}
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
		if err := c.io.Remove(filepath.Join(rulesDir, name)); err != nil && !os.IsNotExist(err) {
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

	var errs []error
	for _, homeRoot := range config.UserHomeRoots() {
		errs = append(errs, c.ensureUserAgentsInHome(homeRoot, globalAgents, entries))
	}
	return errors.Join(errs...)
}

func (c *claude) ensureUserAgentsInHome(homeRoot, globalAgents string, entries []os.DirEntry) error {
	userAgentsDir := filepath.Join(homeRoot, claudeDir, "agents")
	if err := c.io.MkdirAll(userAgentsDir, 0755); err != nil {
		return err
	}
	for _, entry := range entries {
		if err := c.linkUserAgent(globalAgents, userAgentsDir, entry); err != nil {
			return err
		}
	}
	return nil
}

func (c *claude) linkUserAgent(globalAgents, userAgentsDir string, entry os.DirEntry) error {
	agentDir := filepath.Join(globalAgents, entry.Name())
	if !isClaudeAgentDir(agentDir) {
		return nil
	}
	target := filepath.Join(userAgentsDir, entry.Name())
	if isPreExistingManagedLink(target, agentDir) {
		return nil
	}
	// Managed-replace: dot-agents emits this agent link into the user home
	// every refresh. The managed-link case short-circuited above; a genuine
	// user entry here is preserved as <name>.dot-agents-backup rather than
	// hard-failing refresh.
	return links.SymlinkReplacing(agentDir, target, backupSidecar)
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

	var errs []error
	for _, homeRoot := range config.UserHomeRoots() {
		target := filepath.Join(homeRoot, claudeDir, "CLAUDE.md")
		if isPreExistingManagedLink(target, src) {
			continue // already a managed link, leave it
		}
		if err := os.MkdirAll(filepath.Join(homeRoot, claudeDir), 0755); err != nil {
			errs = append(errs, err)
			continue
		}
		// Managed-replace: ~/.claude/CLAUDE.md is a dot-agents output. The
		// managed-link case short-circuited above; a genuine user CLAUDE.md is
		// preserved as CLAUDE.md.dot-agents-backup, never silently destroyed.
		errs = append(errs, links.SymlinkReplacing(src, target, backupSidecar))
	}
	return errors.Join(errs...)
}

func (c *claude) ensureUserSettings(agentsHome string) error {
	globalBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, "global", c.ID(), "global")
	if err != nil {
		return err
	}
	if len(globalBundles) > 0 {
		return emitRenderedHookFileToUserHomes(c.io, globalBundles, filepath.Join(claudeDir, claudeSettingsJSON), renderClaudeHookSettings)
	}

	spec := findClaudeSettingsHookSpec(agentsHome, "global")
	if spec == nil {
		for _, homeRoot := range config.UserHomeRoots() {
			_ = removeManagedFileIf(c.io, filepath.Join(homeRoot, claudeDir, claudeSettingsJSON), isLikelyRenderedClaudeHookSettings)
		}
		return nil
	}
	for _, homeRoot := range config.UserHomeRoots() {
		target := filepath.Join(homeRoot, claudeDir, claudeSettingsJSON)
		if isPreExistingManagedLink(target, spec.SourcePath) {
			continue // already a managed link, leave it
		}
		_ = emitHookSpec(c.io, spec, target, HookEmissionMode{
			Shape:     HookShapeDirect,
			Transport: HookTransportSymlink,
		})
	}
	return nil
}

func (c *claude) ensureUserSkills(agentsHome string) error {
	for _, homeRoot := range config.UserHomeRoots() {
		userSkillsDir := filepath.Join(homeRoot, claudeDir, "skills")
		if err := syncScopedDirSymlinks(c.io, agentsHome, "skills", "global", "SKILL.md", userSkillsDir); err != nil {
			return err
		}
	}
	return nil
}

func (c *claude) createAgentsLinks(project, repoPath, agentsHome string) error {
	// Mirror ~/.agents/agents/<project>/<name>/ into the repo (same model as ensureUserSkills /
	// syncScopedDirSymlinks). Shared-target projection may already create `.claude/agents/*`;
	// this pass also ensures `.agents/agents/*` and heals incorrect symlinks idempotently.
	return syncScopedDirSymlinksTargets(c.io, agentsHome, "agents", project, "AGENT.md",
		filepath.Join(repoPath, claudeAgentsBucketDir, "agents"),
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

	mcpPath := filepath.Join(repoPath, claudeMCPFile)
	return errors.Join(
		c.removeProjectRuleLinks(project, repoPath, agentsHome),
		c.removeProjectSettingsLink(project, repoPath, agentsHome),
		links.RemoveIfSymlinkUnder(mcpPath, agentsHome),
		removeHardlinkedManaged(mcpPath, claudeMCPSources(agentsHome, project)),
		c.removeScopedDirLinks(filepath.Join(repoPath, claudeDir, "agents"), agentsHome),
		c.removeScopedDirLinks(filepath.Join(repoPath, claudeDir, "skills"), agentsHome),
		c.removeScopedDirLinks(filepath.Join(repoPath, claudeAgentsBucketDir, "skills"), agentsHome),
	)
}

func (c *claude) removeProjectRuleLinks(project, repoPath, agentsHome string) error {
	rulesDir := filepath.Join(repoPath, claudeDir, "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return nil
	}
	prefix := project + "--"
	var errs []error
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		linkPath := filepath.Join(rulesDir, e.Name())
		stem := strings.TrimSuffix(strings.TrimPrefix(e.Name(), prefix), ".md")
		errs = append(errs,
			links.RemoveIfSymlinkUnder(linkPath, agentsHome),
			removeHardlinkedManaged(linkPath, claudeRuleSources(agentsHome, project, stem)),
		)
	}
	return errors.Join(errs...)
}

func (c *claude) removeProjectSettingsLink(project, repoPath, agentsHome string) error {
	projectBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), project)
	if err == nil && len(projectBundles) > 0 {
		_ = removeManagedRenderedHookFile(c.io, projectBundles, filepath.Join(repoPath, claudeDir, claudeSettingsLocalJSON), renderClaudeHookSettings)
	} else {
		globalBundles, globalErr := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
		if globalErr == nil && len(globalBundles) > 0 {
			_ = removeManagedRenderedHookFile(c.io, globalBundles, filepath.Join(repoPath, claudeDir, claudeSettingsLocalJSON), renderClaudeHookSettings)
		}
	}
	return links.RemoveIfSymlinkUnder(filepath.Join(repoPath, claudeDir, claudeSettingsLocalJSON), agentsHome)
}

func (c *claude) removeScopedDirLinks(dir, agentsHome string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var errs []error
	for _, e := range entries {
		errs = append(errs, links.RemoveIfSymlinkUnder(filepath.Join(dir, e.Name()), agentsHome))
	}
	return errors.Join(errs...)
}

// isPreExistingManagedLink reports whether path is a managed link we should
// not clobber. It ports the historical "skip if already a symlink" guard to
// the cross-platform link model: a resolvable POSIX symlink / Windows
// junction (any target) is preserved, as is a Windows hard link whose inode
// matches the canonical source we would otherwise (re)create.
func isPreExistingManagedLink(path, source string) bool {
	if _, ok := links.ManagedLinkTarget(path); ok {
		return true
	}
	if links.IsManagedLink(path, source) {
		return true
	}
	// Windows: a managed file link is a hard link with no reparse point, so
	// ManagedLinkTarget cannot resolve it and IsManagedLink only matches when
	// it points at this exact source. A pre-existing managed link pointing at
	// a *different* canonical file must still be left alone — detect it by
	// its multi-link identity rather than a resolvable/known target.
	return links.IsManagedFileLink(path)
}

// claudeMCPSources enumerates every canonical .mcp.json source path
// linkProjectMCP could have linked, so RemoveLinks can drop a Windows hard
// link (no reparse point) the same way RemoveIfSymlinkUnder drops a symlink.
func claudeMCPSources(agentsHome, project string) []string {
	var srcs []string
	for _, scope := range scopedNames(project) {
		for _, name := range []string{"claude.json", "mcp.json"} {
			srcs = append(srcs, filepath.Join(agentsHome, "mcp", scope, name))
		}
	}
	return srcs
}

// claudeRuleSources enumerates the canonical project-rule source paths
// createRulesLinks could have linked for a given link stem. The repo link is
// always named "<project>--<stem>.md" but the source keeps its original
// .md/.mdc/.txt extension.
func claudeRuleSources(agentsHome, project, stem string) []string {
	base := filepath.Join(agentsHome, "rules", project, stem)
	return []string{base + ".md", base + ".mdc", base + ".txt"}
}

func isClaudeAgentDir(path string) bool {
	if !links.IsDirEntry(path) {
		return false
	}
	_, err := os.Stat(filepath.Join(path, "AGENT.md"))
	return err == nil
}

// BrokenLinks implements BrokenLinkReporter for the claude platform.
//
// Claude's project-scope managed surface is:
//
//  1. .claude/rules/<entry> — managed links (POSIX symlink / Windows
//     junction OR Windows file hard link). A resolvable managed link
//     whose target is missing is broken. A non-resolvable entry (plain
//     file, or a Windows hard-linked file with no reparse point) is
//     unmanaged user content and silently skipped — matching the
//     lifecycle-side collectClaudeBrokenLinks contract.
//
//  2. .mcp.json — single-file managed link at the repo root. Healthy when
//     hard-linked to the canonical source under <agentsHome>/mcp/<project>/mcp.json;
//     reported broken when the link is present but does not resolve to a
//     known canonical (delegated to ScanSingleFileLinks per the
//     diagnostics-helpers contract).
//
// PlatformID is set on every returned BrokenLink so JSON consumers can
// self-describe per-entry.
func (c *claude) BrokenLinks(project, repoPath, agentsHome string) []BrokenLink {
	var broken []BrokenLink
	broken = append(broken, c.brokenRuleLinks(repoPath)...)
	broken = append(broken, c.brokenMCPLink(project, repoPath, agentsHome)...)
	return broken
}

// brokenRuleLinks scans .claude/rules for resolvable-but-broken managed
// links. Extracted so BrokenLinks remains a flat composition.
func (c *claude) brokenRuleLinks(repoPath string) []BrokenLink {
	var broken []BrokenLink
	claudeRulesDir := filepath.Join(repoPath, claudeDir, "rules")
	entries, err := os.ReadDir(claudeRulesDir)
	if err != nil {
		return broken
	}
	for _, e := range entries {
		linkPath := filepath.Join(claudeRulesDir, e.Name())
		state, raw := classifyManagedLink(linkPath)
		if state != linkStateBroken {
			continue
		}
		broken = append(broken, BrokenLink{
			PlatformID:  "claude",
			LinkPath:    linkPath,
			Dest:        raw,
			DisplayDest: config.DisplayPath(absolutizeDest(linkPath, raw)),
		})
	}
	return broken
}

// brokenMCPLink classifies the .mcp.json single-file managed link.
//
// .mcp.json on POSIX is a managed symlink to the canonical mcp source under
// <agentsHome>/mcp/<project>/mcp.json; on Windows the managed link layer
// renders it as a hard link. The diagnostic contract that the legacy
// lifecycle path enforced (collectSingleFileBrokenLinks → managedLinkBroken)
// is "report broken only when the entry is a resolvable managed link AND its
// target is missing" — a hard-linked or absent .mcp.json is silently passed
// over. Preserving that contract exactly is what keeps doctor_test's
// TestCollectBrokenLinks_BrokenClaudeMCP and TestCountProjectLinks_AllHealthyVariants
// green under the new BrokenLinkReporter delegation.
//
// Note: ScanSingleFileLinks is intentionally NOT used here. Its semantics
// flag every resolvable managed link as broken unless it is hard-linked to a
// canonical source — that matches cursor's hard-link-only contract but not
// claude's mixed symlink/hardlink model. project and agentsHome are accepted
// to keep the signature uniform with the rules helper (they are unused
// today, but parameterizing now means P3's CountLinks/Badge migration can
// share the helper without a signature churn).
func (c *claude) brokenMCPLink(_, repoPath, _ string) []BrokenLink {
	linkPath := filepath.Join(repoPath, claudeMCPFile)
	state, raw := classifyManagedLink(linkPath)
	if state != linkStateBroken {
		return nil
	}
	return []BrokenLink{{
		PlatformID:  "claude",
		LinkPath:    linkPath,
		Dest:        raw,
		DisplayDest: config.DisplayPath(absolutizeDest(linkPath, raw)),
	}}
}

func (c *claude) SharedTargetIntents(project string) ([]ResourceIntent, error) {
	skills, err := BuildSharedSkillMirrorIntents(project,
		filepath.Join(claudeDir, "skills"),
		filepath.Join(claudeAgentsBucketDir, "skills"),
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

// CountLinks implements LinkCounter for the claude platform: returns the
// (ok, broken) tally of managed links under the project's repo. Mirrors the
// per-platform inline counter that previously lived in status.go's
// claudeTextBadge / countClaudeRules.
//
// Healthy: a .claude/rules/ entry that is either a resolvable managed link
// with a reachable target OR a Windows hard-linked managed file (link
// count > 1); a resolvable .mcp.json or .claude/settings.local.json; or any
// resolvable entry under .claude/agents/ or .claude/skills/. Broken: a
// resolvable managed link whose target is missing.
func (c *claude) CountLinks(_, repoPath, _ string) (ok, broken int) {
	ok, broken = claudeCountRules(filepath.Join(repoPath, claudeDir, "rules"))
	addManagedFileCounts(&ok, &broken, []string{
		filepath.Join(repoPath, ".mcp.json"),
		filepath.Join(repoPath, claudeDir, claudeSettingsLocalJSON),
	})
	addManagedDirCounts(&ok, &broken, []string{
		filepath.Join(repoPath, claudeDir, "agents"),
		filepath.Join(repoPath, claudeDir, "skills"),
	})
	return ok, broken
}

// Badge implements StatusBadger for the claude platform.
func (c *claude) Badge(project, repoPath, agentsHome string) PlatformBadge {
	ok, broken := c.CountLinks(project, repoPath, agentsHome)
	return PlatformBadge{Name: "Claude", Present: ok > 0, Broken: broken > 0}
}

// claudeOrphanBucket is the single canonical bucket claude owns for orphan
// reporting. Each OrphanCanonicalReporter owns a disjoint bucket so the
// doctor-side iterator (reportOrphanCanonicals) never double-counts a
// canonical entry: claude reports the skills bucket, codex reports agents.
const claudeOrphanBucket = "skills"

// OrphanCanonicals implements OrphanCanonicalReporter for the claude platform.
//
// claude owns the "skills" canonical bucket: it enumerates entries under
// <agentsHome>/skills/<project>/ that have no live back-link at
// <projectPath>/.agents/skills/<name>. A non-matching bucket returns nil so
// the doctor iterator can fan out over every (reporter, bucket) pair without
// any single canonical entry being reported twice (codex owns "agents").
//
// The detection logic is shared with codex via scanOrphanCanonicals — the
// only platform-specific input is which bucket each owns.
func (c *claude) OrphanCanonicals(project, projectPath, agentsHome, bucket string) []OrphanCanonical {
	if bucket != claudeOrphanBucket {
		return nil
	}
	return scanOrphanCanonicals(project, projectPath, agentsHome, bucket)
}

// scanOrphanCanonicals enumerates the entries under
// <agentsHome>/<bucket>/<project>/ that have no live back-link at
// <projectPath>/.agents/<bucket>/<name>. Shared by the claude + codex
// OrphanCanonicalReporter implementations (each owns a disjoint bucket).
//
// An absent canonical bucket dir yields nil (not present, not orphaned). A
// missing back-link is a plain orphan (DisplayNote == ""); a back-link that
// is a resolvable managed link pointing at a different canonical is a
// mis-pointed orphan whose DisplayNote carries the formatted suffix; any
// other present back-link (real dir, or a hard link with no reparse point) is
// a live reference and not reported.
func scanOrphanCanonicals(project, projectPath, agentsHome, bucket string) []OrphanCanonical {
	canonicalDir := filepath.Join(agentsHome, bucket, project)
	entries, err := os.ReadDir(canonicalDir)
	if err != nil {
		return nil
	}
	var orphans []OrphanCanonical
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if oc, ok := classifyOrphanCanonical(projectPath, canonicalDir, bucket, e.Name()); ok {
			orphans = append(orphans, oc)
		}
	}
	return orphans
}

// classifyOrphanCanonical decides whether a single canonical entry is an
// orphan and returns the OrphanCanonical to record plus true when it is.
// Extracted from scanOrphanCanonicals to keep the loop flat for cognitive
// complexity. The branch semantics mirror the legacy lifecycle
// classifyCanonicalOrphan exactly (plain orphan / mis-pointed orphan / live
// reference) — only the return shape differs (typed OrphanCanonical instead
// of an annotated string).
func classifyOrphanCanonical(projectPath, canonicalDir, bucket, name string) (OrphanCanonical, bool) {
	backLink := filepath.Join(projectPath, ".agents", bucket, name)
	if _, err := os.Lstat(backLink); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return OrphanCanonical{Name: name}, true
		}
		return OrphanCanonical{}, false
	}
	raw, ok := links.ManagedLinkTarget(backLink)
	if !ok {
		// A non-resolvable back-link (real dir, or a hard-linked entry with no
		// reparse point) is a live reference, not an orphan.
		return OrphanCanonical{}, false
	}
	target := raw
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(backLink), target)
	}
	expected := filepath.Join(canonicalDir, name)
	if filepath.Clean(target) != filepath.Clean(expected) {
		return OrphanCanonical{Name: name, DisplayNote: "  (mis-pointed: " + target + ")"}, true
	}
	return OrphanCanonical{}, false
}

// claudeUserConfigFiles returns the managed single-file references claude
// maintains under the user's home directory: ~/.claude/CLAUDE.md and
// ~/.claude/settings.json.
func claudeUserConfigFiles(home string) []string {
	claudeHome := filepath.Join(home, claudeDir)
	return []string{
		filepath.Join(claudeHome, "CLAUDE.md"),
		filepath.Join(claudeHome, claudeSettingsJSON),
	}
}

// claudeUserConfigDirs returns the managed directories claude maintains under
// the user's home directory: ~/.claude/agents/ and ~/.claude/skills/.
func claudeUserConfigDirs(home string) []string {
	claudeHome := filepath.Join(home, claudeDir)
	return []string{
		filepath.Join(claudeHome, "agents"),
		filepath.Join(claudeHome, "skills"),
	}
}

// UserBrokenLinks implements UserConfigReporter for the claude platform: it
// reports the broken managed links under the user's home directory. The
// surface mirrors the legacy lifecycle collectBrokenUserLinks claude block
// (CLAUDE.md, settings.json, agents/*, skills/*) and every entry carries
// PlatformID="claude" so doctor's JSON/text consumers self-describe.
func (c *claude) UserBrokenLinks(home string) []BrokenLink {
	return scanUserBrokenLinks("claude", claudeUserConfigFiles(home), claudeUserConfigDirs(home))
}

// UserBadge implements UserConfigReporter for the claude platform: it returns
// the user-config badge summarizing whether claude has any managed user-level
// state and whether any of it is broken. Mirrors the legacy lifecycle
// countPlatformHealth("Claude", ...) badge math.
func (c *claude) UserBadge(home string) PlatformBadge {
	ok, broken := scanUserConfigCounts(claudeUserConfigFiles(home), claudeUserConfigDirs(home))
	return PlatformBadge{Name: "Claude", Present: ok > 0, Broken: broken > 0}
}

// scanUserBrokenLinks classifies each managed file path and each entry under
// every managed dir, returning the resolvable-but-broken links tagged with
// platformID. Shared by the claude/codex/opencode UserConfigReporter
// implementations. DisplayDest is rendered via config.DisplayPath so the
// home-relative display matches the legacy lifecycle collectBrokenUserLinks
// output.
func scanUserBrokenLinks(platformID string, files, dirs []string) []BrokenLink {
	var broken []BrokenLink
	for _, path := range files {
		broken = appendUserBrokenLink(broken, platformID, path)
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			broken = appendUserBrokenLink(broken, platformID, filepath.Join(dir, e.Name()))
		}
	}
	return broken
}

// appendUserBrokenLink appends a BrokenLink for path when it is a resolvable
// managed link whose target is missing. A plain file, absent path, or healthy
// link is silently skipped — matching the legacy managedLinkBroken contract
// the user-config audit enforced.
func appendUserBrokenLink(broken []BrokenLink, platformID, path string) []BrokenLink {
	state, raw := classifyManagedLink(path)
	if state != linkStateBroken {
		return broken
	}
	return append(broken, BrokenLink{
		PlatformID:  platformID,
		LinkPath:    path,
		Dest:        raw,
		DisplayDest: config.DisplayPath(absolutizeDest(path, raw)),
	})
}

// scanUserConfigCounts tallies (ok, broken) for the managed user-config files
// and dirs of one platform, reusing the project-scope managed-count helpers so
// the present/broken semantics stay identical to Badge/CountLinks. Shared by
// the claude/codex/opencode UserBadge implementations.
func scanUserConfigCounts(files, dirs []string) (ok, broken int) {
	addManagedFileCounts(&ok, &broken, files)
	addManagedDirCounts(&ok, &broken, dirs)
	return ok, broken
}

// claudeCountRules walks the .claude/rules directory and reports ok/broken
// counts. Mirrors the historical countClaudeRules in status.go: a resolvable
// managed link is ok or broken by target reachability, and a non-resolvable
// entry with multiple hard links is treated as a Windows managed-file link
// (counted ok). Plain regular files are skipped.
func claudeCountRules(rulesDir string) (ok, broken int) {
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		linkPath := filepath.Join(rulesDir, e.Name())
		switch state, _ := classifyManagedLink(linkPath); state {
		case linkStateHealthy:
			ok++
		case linkStateBroken:
			broken++
		case linkStateNotALink:
			// A Windows managed file link is a hard link with no reparse
			// point; ManagedLinkTarget cannot resolve it. Count it as ok
			// only when nlink > 1, matching status.go's existing behavior.
			if hasMultipleHardLinks(linkPath) {
				ok++
			}
		}
	}
	return ok, broken
}

// addManagedFileCounts evaluates each managed-file path and increments ok/broken
// counters: an absent path is silently skipped (not present), a resolvable
// managed link is ok when its target resolves and broken otherwise, and any
// other present path is ok (regular file or Windows hard link). Mirrors
// status.go's countManagedFileOK so per-platform CountLinks impls don't
// duplicate the branching.
func addManagedFileCounts(ok, broken *int, files []string) {
	for _, path := range files {
		if _, err := os.Lstat(path); err != nil {
			continue
		}
		switch state, _ := classifyManagedLink(path); state {
		case linkStateHealthy:
			*ok++
		case linkStateBroken:
			*broken++
		case linkStateNotALink:
			*ok++
		}
	}
}

// addManagedDirCounts walks each directory and increments ok/broken counters
// per entry using the same classification rules as addManagedFileCounts.
// Absent directories are silently skipped — matching status.go's
// countManagedDirEntries contract.
func addManagedDirCounts(ok, broken *int, dirs []string) {
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if _, err := os.Lstat(path); err != nil {
				continue
			}
			switch state, _ := classifyManagedLink(path); state {
			case linkStateHealthy:
				*ok++
			case linkStateBroken:
				*broken++
			case linkStateNotALink:
				*ok++
			}
		}
	}
}
