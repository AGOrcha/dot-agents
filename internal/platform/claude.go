package platform

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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
	claudeAgentsBucketDir   = ".agents"
)

func NewClaude() Platform { return &claude{} }

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
	if _, err := exec.LookPath("claude"); err == nil {
		return true
	}
	home, _ := config.UserHomeDir()
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
	return osMkdirAll(filepath.Join(repoPath, claudeDir, "rules"), 0755)
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
		if err := osRemove(filepath.Join(rulesDir, name)); err != nil && !os.IsNotExist(err) {
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
	if err := osMkdirAll(userAgentsDir, 0755); err != nil {
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
	if isPreExistingManagedLink(target, agentDir) {
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
		if isPreExistingManagedLink(target, src) {
			continue // already a managed link, leave it
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
		if isPreExistingManagedLink(target, spec.SourcePath) {
			continue // already a managed link, leave it
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

	c.removeProjectRuleLinks(project, repoPath, agentsHome)
	c.removeProjectSettingsLink(project, repoPath, agentsHome)
	mcpPath := filepath.Join(repoPath, ".mcp.json")
	links.RemoveIfSymlinkUnder(mcpPath, agentsHome)
	links.RemoveIfHardlinkedToAny(mcpPath, claudeMCPSources(agentsHome, project))
	c.removeScopedDirLinks(filepath.Join(repoPath, claudeDir, "agents"), agentsHome)
	c.removeScopedDirLinks(filepath.Join(repoPath, claudeDir, "skills"), agentsHome)
	c.removeScopedDirLinks(filepath.Join(repoPath, claudeAgentsBucketDir, "skills"), agentsHome)
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
				stem := strings.TrimSuffix(strings.TrimPrefix(e.Name(), prefix), ".md")
				links.RemoveIfHardlinkedToAny(linkPath, claudeRuleSources(agentsHome, project, stem))
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

// isPreExistingManagedLink reports whether path is a managed link we should
// not clobber. It ports the historical "skip if already a symlink" guard to
// the cross-platform link model: a resolvable POSIX symlink / Windows
// junction (any target) is preserved, as is a Windows hard link whose inode
// matches the canonical source we would otherwise (re)create.
func isPreExistingManagedLink(path, source string) bool {
	if _, ok := links.ManagedLinkTarget(path); ok {
		return true
	}
	return links.IsManagedLink(path, source)
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
