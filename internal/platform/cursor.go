package platform

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"golang.org/x/sys/execabs"
	_ "modernc.org/sqlite" // register SQLite driver for database/sql
)

type cursor struct {
	io platformIO
}

const (
	cursorHooksFile   = "hooks.json"
	cursorJSON        = "cursor.json"
	cursorMCPJSON     = "mcp.json"
	cursorDir         = ".cursor"
	globalRulesPrefix = "global--"
)

func NewCursor() Platform { return &cursor{io: stdPlatformIO{}} }

func (c *cursor) ID() string          { return "cursor" }
func (c *cursor) DisplayName() string { return "Cursor" }

// SessionReader — env var contract not yet confirmed.
// Model is readable from ~/.cursor/ai-tracking/ai-code-tracking.db
// (conversation_summaries.model); deferred until P1 adds SQLite access.
func (c *cursor) AIAgentPrefix() string              { return "cursor" }
func (c *cursor) SessionEnvs() []string              { return []string{"CURSOR_SESSION_ID"} }
func (c *cursor) EntrypointEnvs() []string           { return nil }
func (c *cursor) ResolveModel(_, _, _ string) string { return "" }

// StatsReader implementation.
func (c *cursor) ReadUsageStats(home string) *PlatformUsageStats {
	return cursorReadUsageStats(home)
}

func cursorReadUsageStats(home string) *PlatformUsageStats {
	dbPath := filepath.Join(home, cursorDir, "ai-tracking", "ai-code-tracking.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil
	}
	defer db.Close()

	const query = `SELECT commitHash, branchName, scoredAt,
		linesAdded, linesDeleted, composerLinesAdded, composerLinesDeleted,
		humanLinesAdded, v2AiPercentage
		FROM scored_commits ORDER BY scoredAt DESC LIMIT 10`
	rows, err := db.Query(query)
	if err != nil {
		return nil
	}
	defer rows.Close()

	stats := &PlatformUsageStats{PlatformID: "cursor"}
	for rows.Next() {
		var ca CommitAttribution
		var scoredAtMs int64
		if err := rows.Scan(&ca.CommitHash, &ca.BranchName, &scoredAtMs,
			&ca.LinesAdded, &ca.LinesDeleted,
			&ca.ComposerLinesAdded, &ca.ComposerLinesDeleted,
			&ca.HumanLinesAdded, &ca.V2AIPercentage); err != nil {
			continue
		}
		ca.ScoredAt = fmt.Sprintf("%d", scoredAtMs) // Unix ms; caller can format
		stats.CommitAttribution = append(stats.CommitAttribution, ca)
	}
	if len(stats.CommitAttribution) == 0 {
		return nil
	}
	return stats
}

// SessionTokenScanner implementation.
// Scans ~/.cursor/projects/<slug>/agent-tools/*.txt — stream-json result files
// written by the cursor agent binary per completed agent run. Each file has a
// final line with {"type":"result","usage":{...}} in camelCase schema.
func (c *cursor) ScanSessionTokens(home, projectPath, _, afterTimestamp string) SessionTokenMetrics {
	return cursorScanSessionTokens(home, projectPath, afterTimestamp)
}

func cursorScanSessionTokens(home, projectPath, afterTimestamp string) SessionTokenMetrics {
	var after time.Time
	if afterTimestamp != "" {
		after, _ = time.Parse(time.RFC3339, afterTimestamp)
	}

	var m SessionTokenMetrics
	slug := strings.ReplaceAll(strings.TrimPrefix(projectPath, "/"), "/", "-")
	agentToolsDir := filepath.Join(home, cursorDir, "projects", slug, "agent-tools")
	entries, err := os.ReadDir(agentToolsDir)
	if err != nil {
		return m
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		if !after.IsZero() {
			info, err := e.Info()
			if err != nil || !info.ModTime().After(after) {
				continue
			}
		}
		cursorAccumulateResultTokens(filepath.Join(agentToolsDir, e.Name()), &m)
	}
	return m
}

// cursorAccumulateResultTokens scans a stream-json file for {"type":"result"}
// lines and accumulates camelCase token usage into m.
func cursorAccumulateResultTokens(path string, m *SessionTokenMetrics) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"result"`)) {
			continue
		}
		var entry struct {
			Type  string `json:"type"`
			Usage struct {
				InputTokens      int `json:"inputTokens"`
				OutputTokens     int `json:"outputTokens"`
				CacheReadTokens  int `json:"cacheReadTokens"`
				CacheWriteTokens int `json:"cacheWriteTokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(line, &entry); err != nil || entry.Type != "result" {
			continue
		}
		if entry.Usage.InputTokens == 0 && entry.Usage.OutputTokens == 0 {
			continue
		}
		m.InputTokens += entry.Usage.InputTokens
		m.OutputTokens += entry.Usage.OutputTokens
		m.CacheReadTokens += entry.Usage.CacheReadTokens
		m.CacheCreationTokens += entry.Usage.CacheWriteTokens
		m.MessageCount++
	}
}

func (c *cursor) IsInstalled() bool {
	if _, err := os.Stat("/Applications/Cursor.app"); err == nil {
		return true
	}
	return probeInstalled("agent") || probeInstalled("cursor")
}

func (c *cursor) Version() string {
	// macOS app bundle version via defaults; bounded so tests/doctor never hang.
	if _, err := os.Stat("/Applications/Cursor.app"); err == nil {
		appVer, err := macOSCursorAppShortVersion()
		if err == nil && appVer != "" {
			if cli := firstCLIPeekVersion("agent", "cursor"); cli != "" {
				return appVer + " (CLI: " + cli + ")"
			}
			return appVer + " (App)"
		}
	}
	return firstCLIPeekVersion("agent", "cursor")
}

func macOSCursorAppShortVersion() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cliVersionProbeTimeout)
	defer cancel()
	cmd := execabs.CommandContext(ctx, "defaults", "read",
		"/Applications/Cursor.app/Contents/Info.plist",
		"CFBundleShortVersionString")
	cmd.WaitDelay = cliExecPipeWaitDelay
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// firstCLIPeekVersion runs `<name> --version` for the first resolvable binary in order.
// Official Cursor CLI uses `agent` (see install docs); `cursor` remains a fallback.
func firstCLIPeekVersion(binNames ...string) string {
	for _, name := range binNames {
		path, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		v, err := peekCLIVersionLine(path)
		if err == nil && v != "" {
			return v
		}
	}
	return ""
}

// peekCLIVersionLine runs a CLI `--version` probe with a wall-clock bound (via
// the shared probe seam) so doctor and tests cannot hang when a shim blocks
// (e.g. TTY/GUI interaction).
func peekCLIVersionLine(path string) (string, error) {
	out, err := probeVersionAtPath(path, "--version")
	if err != nil {
		return "", err
	}
	return firstLine(out), nil
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
	if err := c.io.MkdirAll(rulesDir, 0755); err != nil {
		return err
	}
	desired := map[string]string{}
	c.collectRuleLinks(filepath.Join(agentsHome, "rules", "global"), globalRulesPrefix, desired)
	c.collectRuleLinks(filepath.Join(agentsHome, "rules", project), project+"--", desired)
	if err := c.pruneRuleLinks(rulesDir, project, desired); err != nil {
		return err
	}
	for target, src := range desired {
		// Managed-replace: a dot-agents .mdc rule hard link this platform owns
		// at a fixed path. On a repeat refresh the prior managed hard link is
		// present and (being a regular file with no reparse point) is not an
		// ownedManagedLink, so plain Hardlink would refuse. Route through the
		// Replacing variant with the sidecar backup so a stale managed link is
		// harmlessly backed up + relinked (idempotent) and a genuine user file
		// is preserved as <dst>.dot-agents-backup, never silently destroyed.
		if err := links.HardlinkReplacing(src, filepath.Join(rulesDir, target), backupSidecar); err != nil {
			return err
		}
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
		if err := c.io.Remove(filepath.Join(rulesDir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (c *cursor) createSettingsLinks(project, repoPath, agentsHome string) error {
	if err := c.io.MkdirAll(filepath.Join(repoPath, cursorDir), 0755); err != nil {
		return err
	}
	if src := resolveScopedFile(agentsHome, "settings", project, cursorJSON); src != "" {
		dst := filepath.Join(repoPath, cursorDir, "settings.json")
		// Managed-replace at a fixed owned path (.cursor/settings.json); see
		// createRuleLinks for the Replacing-variant rationale.
		return links.HardlinkReplacing(src, dst, backupSidecar)
	}
	return nil
}

func (c *cursor) createMCPLinks(project, repoPath, agentsHome string) error {
	if err := c.io.MkdirAll(filepath.Join(repoPath, cursorDir), 0755); err != nil {
		return err
	}
	if src := resolveScopedFile(agentsHome, "mcp", project, cursorJSON, cursorMCPJSON); src != "" {
		dst := filepath.Join(repoPath, cursorDir, cursorMCPJSON)
		// Managed-replace at a fixed owned path (.cursor/mcp.json).
		return links.HardlinkReplacing(src, dst, backupSidecar)
	}
	return nil
}

func (c *cursor) createIgnoreLink(project, repoPath, agentsHome string) error {
	if src := resolveScopedFile(agentsHome, "settings", project, "cursorignore"); src != "" {
		dst := filepath.Join(repoPath, ".cursorignore")
		// Managed-replace at a fixed owned path (.cursorignore).
		return links.HardlinkReplacing(src, dst, backupSidecar)
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
	if err := c.io.MkdirAll(filepath.Join(repoPath, cursorDir), 0755); err != nil {
		return err
	}
	return emitPreferredHookFile(
		c.io,
		repoTarget,
		renderCursorHookConfig,
		resolveHookSpec(agentsHome, []string{"hooks"}, project, cursorJSON),
		directHardlinkHookMode,
		func(p string) error { return removeRenderedCursorHookConfig(c.io, p) },
		repoBundles,
	)
}

func (c *cursor) writeUserHomeHooks(project, agentsHome string) error {
	globalBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global")
	if err != nil {
		return err
	}
	return emitPreferredHookFileToUserHomes(
		c.io,
		filepath.Join(cursorDir, cursorHooksFile),
		renderCursorHookConfig,
		resolveHookSpecInScope(agentsHome, []string{"hooks"}, "global", cursorJSON),
		directHardlinkHookMode,
		func(p string) error { return removeRenderedCursorHookConfig(c.io, p) },
		globalBundles,
	)
}

func (c *cursor) createAgentsLinks(_, _, _ string) error {
	// `.claude/agents/*` mirrors match Claude's layout; command layer runs CollectAndExecuteSharedTargetPlan first.
	return nil
}

func (c *cursor) RemoveLinks(project, repoPath string) error {
	agentsHome := config.AgentsHome()
	return errors.Join(
		c.removeRuleLinks(project, repoPath, agentsHome),
		c.removeHooksLink(project, repoPath, agentsHome),
		c.removeAgentLinks(repoPath, agentsHome),
	)
}

func (c *cursor) removeRuleLinks(project, repoPath, agentsHome string) error {
	rulesDir := filepath.Join(repoPath, cursorDir, "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return nil
	}
	var errs []error
	for _, entry := range entries {
		errs = append(errs, c.removeRuleEntry(entry, rulesDir, project, agentsHome))
	}
	return errors.Join(errs...)
}

func (c *cursor) removeRuleEntry(entry os.DirEntry, rulesDir, project, agentsHome string) error {
	if entry.IsDir() {
		return nil
	}
	name := entry.Name()
	filePath := filepath.Join(rulesDir, name)

	switch {
	case strings.HasPrefix(name, globalRulesPrefix):
		return removeHardlinkedManaged(filePath, cursorRuleSources(agentsHome, "global", strings.TrimPrefix(name, globalRulesPrefix)))
	case strings.HasPrefix(name, project+"--"):
		return removeHardlinkedManaged(filePath, cursorRuleSources(agentsHome, project, strings.TrimPrefix(name, project+"--")))
	}
	return nil
}

func (c *cursor) removeHooksLink(project, repoPath, agentsHome string) error {
	hooksFilePath := filepath.Join(repoPath, cursorDir, cursorHooksFile)
	repoBundles, err := collectCanonicalHookSpecsForPlatform(agentsHome, project, c.ID(), "global", project)
	if err == nil && len(repoBundles) > 0 {
		_ = removeManagedRenderedHookFile(c.io, repoBundles, hooksFilePath, renderCursorHookConfig)
	}
	return removeHardlinkedManaged(hooksFilePath, []string{
		filepath.Join(agentsHome, "hooks", project, cursorJSON),
		filepath.Join(agentsHome, "hooks", "global", cursorJSON),
	})
}

func (c *cursor) removeAgentLinks(repoPath, agentsHome string) error {
	agentsTarget := filepath.Join(repoPath, cursorDir, "agents")
	entries, err := os.ReadDir(agentsTarget)
	if err != nil {
		return nil
	}
	var errs []error
	for _, entry := range entries {
		errs = append(errs, links.RemoveIfSymlinkUnder(filepath.Join(agentsTarget, entry.Name()), agentsHome))
	}
	return errors.Join(errs...)
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

// cursorUserConfigFiles returns the managed single-file references cursor
// maintains under the user's home directory: ~/.cursor/hooks.json. dot-agents
// wires this file from ~/.agents/hooks/{scope}/cursor.json via
// writeUserHomeHooks (see PLATFORM_DIRS_DOCS "User-home wiring" — cursor IS
// wired at user scope, parallel to codex's ~/.codex/hooks.json). Cursor's
// broader documented user-config layer (~/.cursor/agents/, ~/.cursor/mcp.json,
// ~/.cursor/rules, ~/.cursor/plugins/local/) is NOT yet wired by dot-agents, so
// only the hooks file is reported as a managed link today.
func cursorUserConfigFiles(home string) []string {
	return []string{filepath.Join(home, cursorDir, cursorHooksFile)}
}

// UserBrokenLinks implements UserConfigReporter for the cursor platform. The
// managed user-home surface is ~/.cursor/hooks.json (the only user-scope target
// writeUserHomeHooks emits); every reported entry carries PlatformID="cursor".
// A rendered managed file or healthy hard link is silently skipped — only a
// resolvable managed link whose target is missing is reported broken, matching
// the shared scanUserBrokenLinks contract used by claude/codex/opencode.
func (c *cursor) UserBrokenLinks(home string) []BrokenLink {
	return scanUserBrokenLinks("cursor", cursorUserConfigFiles(home), nil)
}

// UserBadge implements UserConfigReporter for the cursor platform: the
// user-config badge over ~/.cursor/hooks.json. Present is true when the managed
// hooks file exists (rendered file or hard link), Broken when it is a dangling
// managed link — mirroring the codex UserBadge badge math over its own
// ~/.codex/hooks.json.
func (c *cursor) UserBadge(home string) PlatformBadge {
	ok, broken := scanUserConfigCounts(cursorUserConfigFiles(home), nil)
	return PlatformBadge{Name: c.DisplayName(), Present: ok > 0, Broken: broken > 0}
}

// PrintAudit implements AuditPrinter for the cursor platform: it renders the
// per-project `.cursor/rules/` rule links and the `.cursor/mcp.json` link.
// Moved verbatim (output preserved byte-for-byte) from the lifecycle-side
// printCursorAudit in Phase 5.
func (c *cursor) PrintAudit(w io.Writer, project, repoPath, agentsHome string) {
	fmt.Fprintf(w, "    %sCursor%s\n", ui.Cyan, ui.Reset)
	rulesDir := filepath.Join(repoPath, cursorDir, "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		fmt.Fprintf(w, "      %s(no .cursor/rules/)%s\n", ui.Dim, ui.Reset)
		return
	}
	if cursorPrintRules(w, project, rulesDir, agentsHome, entries) == 0 {
		fmt.Fprintf(w, "      %s(no rules)%s\n", ui.Dim, ui.Reset)
	}
	cursorPrintMCPLink(w, repoPath)
	fmt.Fprintln(w)
}

// cursorRuleSourceInfo classifies a cursor rule entry into srcType
// ("global"|"project"|"local"), the user-display path it should link to
// (empty for local files), and the canonical scope/filename used to resolve
// the on-disk source under <agentsHome>/rules/. scope and srcName are empty
// for local files.
func cursorRuleSourceInfo(entryName, projectName string) (srcType, linkedTo, scope, srcName string) {
	switch {
	case strings.HasPrefix(entryName, globalRulesPrefix):
		srcName := strings.TrimPrefix(entryName, globalRulesPrefix)
		return "global", "~/.agents/rules/global/" + srcName, "global", srcName
	case strings.HasPrefix(entryName, projectName+"--"):
		srcName := strings.TrimPrefix(entryName, projectName+"--")
		return "project", "~/.agents/rules/" + projectName + "/" + srcName, projectName, srcName
	}
	return "local", "", "", ""
}

// cursorPrintRuleEntry renders one cursor rule entry's audit line to w.
func cursorPrintRuleEntry(w io.Writer, project, rulesDir, agentsHome, entryName string) {
	srcType, linkedTo, scope, srcName := cursorRuleSourceInfo(entryName, project)
	if srcType == "local" {
		fmt.Fprintf(w, auditLocalFileIndentedFmt, ui.Dim, ui.Reset, entryName, ui.Dim, ui.Reset)
		return
	}
	f := filepath.Join(rulesDir, entryName)
	// Resolve the canonical source under agentsHome with filepath.Join rather
	// than tilde string-substitution: on Windows agentsHome can contain an 8.3
	// short-path segment (e.g. RUNNER~1) whose literal '~' a naive
	// strings.Replace(…, "~", …) would clobber, corrupting the path so the
	// healthy hard link is misreported as "not linked".
	srcPath := filepath.Join(agentsHome, "rules", scope, srcName)
	if linked, _ := links.AreHardlinked(f, srcPath); linked {
		fmt.Fprintf(w, "      %s✓%s %s %s← %s%s\n", ui.Green, ui.Reset, entryName, ui.Dim, linkedTo, ui.Reset)
	} else {
		fmt.Fprintf(w, "      %s!%s %s %s(not linked to %s)%s\n", ui.Yellow, ui.Reset, entryName, ui.Dim, linkedTo, ui.Reset)
	}
}

// cursorPrintRules renders all valid cursor rule entries in rulesDir to w;
// returns the count of entries actually rendered (used to detect empty sets).
func cursorPrintRules(w io.Writer, project, rulesDir, agentsHome string, entries []os.DirEntry) int {
	count := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".mdc") || strings.Contains(e.Name(), ".dot-agents-backup") {
			continue
		}
		cursorPrintRuleEntry(w, project, rulesDir, agentsHome, e.Name())
		count++
	}
	return count
}

// cursorPrintMCPLink renders the .cursor/mcp.json audit line to w.
func cursorPrintMCPLink(w io.Writer, repoPath string) {
	cursorMCPPath := filepath.Join(repoPath, cursorDir, cursorMCPJSON)
	if _, err := os.Lstat(cursorMCPPath); err != nil {
		fmt.Fprintf(w, "      %s-%s .cursor/mcp.json %s(not linked)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
		return
	}
	state, raw := classifyManagedLink(cursorMCPPath)
	if state == linkStateNotALink {
		fmt.Fprintf(w, "      %s✓%s .cursor/mcp.json %s(hard link or local file)%s\n", ui.Green, ui.Reset, ui.Dim, ui.Reset)
		return
	}
	if state == linkStateBroken {
		fmt.Fprintf(w, "      %s✗%s .cursor/mcp.json %s→ %s (broken)%s\n", ui.Red, ui.Reset, ui.Dim, displayDest(cursorMCPPath, raw), ui.Reset)
	} else {
		fmt.Fprintf(w, "      %s✓%s .cursor/mcp.json %s→ %s%s\n", ui.Green, ui.Reset, ui.Dim, displayDest(cursorMCPPath, raw), ui.Reset)
	}
}

func (c *cursor) SharedTargetIntents(project string) ([]ResourceIntent, error) {
	// Same repo-relative targets as Claude so duplicate intents merge in the shared plan.
	return BuildSharedAgentMirrorIntents(project, filepath.Join(".claude", "agents"))
}

// CountLinks implements LinkCounter for the cursor platform: returns the
// (ok, broken) tally of managed links under the project's repo. Mirrors the
// per-platform inline counter that previously lived in status.go's
// cursorTextBadge / countCursorRules / addManagedCounts.
//
// Healthy entries: hard-linked .cursor/rules/<scope>--<name> files (matched
// against ~/.agents/rules/<scope>/<name> with the .mdc → .md fallback),
// plus a resolvable .cursor/mcp.json, .cursor/settings.json,
// .cursor/hooks.json, or .cursorignore. Broken: a managed-rule entry whose
// canonical inode no longer matches, or a resolvable managed link whose
// target is missing.
func (c *cursor) CountLinks(project, repoPath, agentsHome string) (ok, broken int) {
	ok, broken = cursorCountRules(project, repoPath, agentsHome)
	addManagedFileCounts(&ok, &broken, []string{
		filepath.Join(repoPath, cursorDir, cursorMCPJSON),
		filepath.Join(repoPath, cursorDir, "settings.json"),
		filepath.Join(repoPath, cursorDir, cursorHooksFile),
		filepath.Join(repoPath, ".cursorignore"),
	})
	return ok, broken
}

// Badge implements StatusBadger for the cursor platform — the single source
// of truth replacing the parallel cursorTextBadge / platformStatus("Cursor", …)
// paths in status.go.
func (c *cursor) Badge(project, repoPath, agentsHome string) PlatformBadge {
	ok, broken := c.CountLinks(project, repoPath, agentsHome)
	return PlatformBadge{Name: c.DisplayName(), Present: ok > 0, Broken: broken > 0}
}

// cursorCountRules walks .cursor/rules/ and counts hardlinks to the global
// and project-scoped rules stores as ok, mismatches as warnings. Mirrors the
// historical countCursorRules in status.go.
func cursorCountRules(project, repoPath, agentsHome string) (ok, broken int) {
	rulesDir := filepath.Join(repoPath, cursorDir, "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".dot-agents-backup") || !strings.HasSuffix(e.Name(), ".mdc") {
			continue
		}
		scope, rest, isManaged := cursorEntryScope(e.Name(), project)
		if !isManaged {
			continue
		}
		f := filepath.Join(rulesDir, e.Name())
		if linked, _ := links.AreHardlinked(f, filepath.Join(agentsHome, "rules", scope, rest)); linked {
			ok++
			continue
		}
		fallback := strings.TrimSuffix(rest, ".mdc") + ".md"
		if linked, _ := links.AreHardlinked(f, filepath.Join(agentsHome, "rules", scope, fallback)); linked {
			ok++
			continue
		}
		broken++
	}
	return ok, broken
}

// cursorEntryScope returns the (scope, rest, ok) tuple for a cursor rule entry
// name. scope is "global" or project; rest is the canonical filename relative
// to ~/.agents/rules/<scope>/. ok=false when the entry is not a managed
// cursor rule for this project.
func cursorEntryScope(name, project string) (scope, rest string, ok bool) {
	switch {
	case strings.HasPrefix(name, globalRulesPrefix):
		return "global", strings.TrimPrefix(name, globalRulesPrefix), true
	case strings.HasPrefix(name, project+"--"):
		return project, strings.TrimPrefix(name, project+"--"), true
	}
	return "", "", false
}

// BrokenLinks implements BrokenLinkReporter for the cursor platform.
//
// Cursor's project-scope managed surface is .cursor/rules/<scope>--<rest>.mdc
// where scope is "global" or the project name. Each entry is a hard link to
// the canonical source under <agentsHome>/rules/<scope>/<rest> (with .mdc → .md
// fallback). An entry that is no longer hard-linked to either candidate is
// reported as broken — the canonical contract is "link shares an inode with a
// known source", not "link merely exists at the expected path".
//
// Behavior preserved from the previous lifecycle-side collectCursorBrokenLinks
// implementation: backup-artifact and non-.mdc entries are skipped silently
// (they are unmanaged); a missing .cursor/rules dir produces an empty result
// rather than an error (matches doctor/status "absent != broken").
//
// PlatformID is set on every returned BrokenLink so JSON consumers can
// self-describe per-entry (BrokenLink struct contract).
func (c *cursor) BrokenLinks(project, repoPath, agentsHome string) []BrokenLink {
	var broken []BrokenLink
	rulesDir := filepath.Join(repoPath, cursorDir, "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return broken
	}
	for _, e := range entries {
		bl, ok := cursorBrokenRuleEntry(e, rulesDir, project, agentsHome)
		if !ok {
			continue
		}
		broken = append(broken, bl)
	}
	return broken
}

// cursorBrokenRuleEntry classifies a single .cursor/rules entry. Returns the
// broken-link record and true when the entry is a managed rule that fails
// the hard-link check; returns false for unmanaged entries (directories,
// non-.mdc files, backup artifacts, foreign-scope names) and for healthy
// hard-linked rules. Extracted from BrokenLinks to keep the loop body flat
// for cognitive-complexity.
func cursorBrokenRuleEntry(entry os.DirEntry, rulesDir, project, agentsHome string) (BrokenLink, bool) {
	if entry.IsDir() {
		return BrokenLink{}, false
	}
	name := entry.Name()
	scope, rest, ok := cursorBrokenRuleScope(name, project)
	if !ok {
		return BrokenLink{}, false
	}
	linkPath := filepath.Join(rulesDir, name)
	sources := cursorRuleSources(agentsHome, scope, rest)
	if anyHardlinked(linkPath, sources) {
		return BrokenLink{}, false
	}
	// Display destination is the primary canonical (the .mdc form), matching
	// the lifecycle-side helper's behavior.
	return BrokenLink{
		PlatformID:  "cursor",
		LinkPath:    linkPath,
		Dest:        sources[0],
		DisplayDest: config.DisplayPath(sources[0]),
	}, true
}

// cursorBrokenRuleScope returns the (scope, rest) pair for a managed cursor
// rule entry, or ok=false when the entry name is not a managed rule for this
// project. Mirrors the lifecycle-side cursorRuleScope helper so doctor's
// existing classification semantics are preserved verbatim.
func cursorBrokenRuleScope(entryName, projectName string) (scope, rest string, ok bool) {
	switch {
	case strings.Contains(entryName, ".dot-agents-backup"):
		return "", "", false
	case !strings.HasSuffix(entryName, ".mdc"):
		return "", "", false
	case strings.HasPrefix(entryName, globalRulesPrefix):
		return "global", strings.TrimPrefix(entryName, globalRulesPrefix), true
	case strings.HasPrefix(entryName, projectName+"--"):
		return projectName, strings.TrimPrefix(entryName, projectName+"--"), true
	}
	return "", "", false
}
