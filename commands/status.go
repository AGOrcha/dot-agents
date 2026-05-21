package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

const (
	statusHooksJSON               = "hooks.json"
	statusCodexDir                = ".codex"
	statusAgentsDir               = ".agents"
	statusOpenCodeDir             = ".opencode"
	statusGitHubDir               = ".github"
	statusLocalFileFmt            = "    %s○%s %s %s(local file)%s\n"
	statusCursorDir               = ".cursor"
	statusAgentsMarkdown          = "AGENTS.md"
	statusCopilotInstructions     = "copilot-instructions.md"
	statusCopilotMCPJSON          = "mcp.json"
	statusClaudeDir               = ".claude"
	statusClaudeSettingsLocalJSON = "settings.local.json"
	statusClaudeSettingsJSON      = "settings.json"
	globalRulesPrefix             = "global--"
	statusClaudeMCPJSON           = ".mcp.json"
	statusCodexConfigToml         = "config.toml"
	statusOpenCodeJSON            = "opencode.json"
	statusVSCodeDir               = ".vscode"
	// statusAuditLinkOkFormat and statusAuditLinkBrokenFormat are shared by
	// the printSymlinkDirAudit / printSymlinkAudit helpers so per-platform
	// audit output stays byte-identical across rules, MCP, agents, skills,
	// hooks. Keep the 6-leading-space indentation; tests rely on it.
	statusAuditLinkOkFormat     = "      %s✓%s %s %s→ %s%s\n"
	statusAuditLinkBrokenFormat = "      %s✗%s %s %s→ %s (broken)%s\n"
)

type platformBadge struct {
	name    string
	present bool
	broken  bool
}

type statusJSONReport struct {
	AgentsHome     string                    `json:"agents_home"`
	Git            statusJSONGit             `json:"git"`
	CanonicalStore map[string]statusJSONItem `json:"canonical_store"`
	Plugins        []statusJSONPlugin        `json:"plugins,omitempty"`
	UserConfig     []statusJSONPlatform      `json:"user_config"`
	Projects       []statusJSONProject       `json:"projects"`
}

type statusJSONGit struct {
	Initialized bool   `json:"initialized"`
	Branch      string `json:"branch,omitempty"`
	Remote      string `json:"remote,omitempty"`
}

type statusJSONItem struct {
	Scopes int `json:"scopes"`
	Items  int `json:"items"`
}

type statusJSONPlugin struct {
	Name  string `json:"name"`
	Scope string `json:"scope"`
}

type statusJSONPlatform struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	Broken  bool   `json:"broken"`
}

type statusJSONProject struct {
	Name          string               `json:"name"`
	Path          string               `json:"path"`
	PathExists    bool                 `json:"path_exists"`
	Platforms     []statusJSONPlatform `json:"platforms"`
	ManifestFound bool                 `json:"manifest_found"`
	LastRefreshed string               `json:"last_refreshed,omitempty"`
}

// statusConfigLoader is the narrow collaborator status.go's fault-injectable
// LoadConfig operation needs (interface-DI per docs/TEST_SEAMS.md).
// Single-method, file-prefixed -er form; file-scoped — do not share with
// other commands files.
type statusConfigLoader interface {
	LoadConfig() (*config.Config, error)
}

// stdStatusConfigLoader is the production statusConfigLoader backed by
// internal/config.Load.
type stdStatusConfigLoader struct{}

func (stdStatusConfigLoader) LoadConfig() (*config.Config, error) { return config.Load() }

func NewStatusCmd() *cobra.Command {
	var audit bool
	var agentFilter string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show managed projects and link health",
		Long: `Summarizes the shared ~/.agents/ store, managed projects, and per-platform
link health so you can quickly see whether configuration is present, stale, or broken.

The manifest line reflects declared skills, agents, hooks, MCP, and settings in
.agentsrc.json; canonical hook bundle inventory on disk is da hooks list
(or hooks show).

Use --audit when you need file-level detail suitable for debugging or for an AI
agent that must reason about the exact managed outputs.`,
		Example: ExampleBlock(
			"  da status",
			"  da status --audit",
			"  da status --agent codex",
		),
		Args: NoArgsWithHints("Use `--agent` to filter by platform instead of passing a positional argument."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(audit, agentFilter, stdStatusConfigLoader{})
		},
	}
	cmd.Flags().BoolVar(&audit, "audit", false, "Show detailed link audit for each project")
	cmd.Flags().StringVar(&agentFilter, "agent", "", "Filter to specific agent (cursor, claude, codex, opencode, copilot)")
	return cmd
}

// agentsHomeGitProbe captures ~/.agents git metadata without formatting output.
type agentsHomeGitProbe struct {
	IsRepo bool
	Branch string
	Remote string
}

func probeAgentsHomeGit(agentsHome string) agentsHomeGitProbe {
	gitDir := filepath.Join(agentsHome, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return agentsHomeGitProbe{}
	}
	branchOut, _ := exec.Command("git", "-C", agentsHome, "rev-parse", "--abbrev-ref", "HEAD").Output()
	branch := strings.TrimSpace(string(branchOut))
	remoteOut, _ := exec.Command("git", "-C", agentsHome, "remote", "get-url", "origin").Output()
	remote := strings.TrimSpace(string(remoteOut))
	return agentsHomeGitProbe{IsRepo: true, Branch: branch, Remote: remote}
}

func printAgentsHomeGitStatusLine(agentsHome string) {
	g := probeAgentsHomeGit(agentsHome)
	if !g.IsRepo {
		fmt.Fprintf(os.Stdout, "  %s! not a git repo — run: da sync init%s\n", ui.Yellow, ui.Reset)
		return
	}
	if g.Remote != "" {
		fmt.Fprintf(os.Stdout, "  %sgit:%s %s%s%s %s(%s)%s\n", ui.Dim, ui.Reset, ui.Bold, g.Branch, ui.Reset, ui.Dim, g.Remote, ui.Reset)
		return
	}
	fmt.Fprintf(os.Stdout, "  %sgit:%s %s%s%s  %s! no remote — run: da sync init%s\n", ui.Dim, ui.Reset, ui.Bold, g.Branch, ui.Reset, ui.Yellow, ui.Reset)
}

// collectProjectTextBadges builds the same per-platform row shown in text-mode status.
func collectProjectTextBadges(path, agentsHome string) []platformBadge {
	return []platformBadge{
		cursorTextBadge(path, agentsHome),
		claudeTextBadge(path),
		codexTextBadge(path),
		opencodeTextBadge(path),
		copilotTextBadge(path),
	}
}

// cursorTextBadge counts cursor rules/managed files for the badge row.
func cursorTextBadge(path, agentsHome string) platformBadge {
	cursorOK, cursorWarn := countCursorRules(path, agentsHome)
	addManagedCounts(&cursorOK, &cursorWarn, []string{
		filepath.Join(path, statusCursorDir, statusCopilotMCPJSON),
		filepath.Join(path, statusCursorDir, statusClaudeSettingsJSON),
		filepath.Join(path, statusCursorDir, statusHooksJSON),
		filepath.Join(path, ".cursorignore"),
	}, nil)
	return platformBadge{"Cursor", cursorOK > 0, cursorWarn > 0}
}

// countCursorRules walks .cursor/rules/ and counts hardlinks to the global
// rules store as ok, mismatches as warnings.
func countCursorRules(path, agentsHome string) (int, int) {
	ok, warn := 0, 0
	cursorRulesDir := filepath.Join(path, statusCursorDir, "rules")
	entries, err := os.ReadDir(cursorRulesDir)
	if err != nil {
		return ok, warn
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".dot-agents-backup") || !strings.HasSuffix(e.Name(), ".mdc") {
			continue
		}
		if !strings.HasPrefix(e.Name(), globalRulesPrefix) {
			continue
		}
		f := filepath.Join(cursorRulesDir, e.Name())
		srcName := strings.TrimPrefix(e.Name(), globalRulesPrefix)
		src := filepath.Join(agentsHome, "rules", "global", srcName)
		if linked, _ := links.AreHardlinked(f, src); linked {
			ok++
			continue
		}
		srcMD := strings.TrimSuffix(srcName, ".mdc") + ".md"
		src2 := filepath.Join(agentsHome, "rules", "global", srcMD)
		if linked, _ := links.AreHardlinked(f, src2); linked {
			ok++
			continue
		}
		warn++
	}
	return ok, warn
}

// claudeTextBadge counts the .claude/rules symlinks plus managed files.
func claudeTextBadge(path string) platformBadge {
	claudeOK, claudeWarn := countClaudeRules(path)
	addManagedCounts(&claudeOK, &claudeWarn, []string{
		filepath.Join(path, statusClaudeMCPJSON),
		filepath.Join(path, statusClaudeDir, statusClaudeSettingsLocalJSON),
	}, []string{
		filepath.Join(path, statusClaudeDir, "agents"),
		filepath.Join(path, statusClaudeDir, "skills"),
	})
	return platformBadge{"Claude", claudeOK > 0, claudeWarn > 0}
}

// countClaudeRules walks .claude/rules/ symlinks and reports ok/warn counts.
func countClaudeRules(path string) (int, int) {
	ok, warn := 0, 0
	claudeRulesDir := filepath.Join(path, statusClaudeDir, "rules")
	entries, err := os.ReadDir(claudeRulesDir)
	if err != nil {
		return ok, warn
	}
	for _, e := range entries {
		linkPath := filepath.Join(claudeRulesDir, e.Name())
		// Resolvable managed link (POSIX symlink / Windows junction).
		if _, isLink, isBroken := managedLinkBroken(linkPath); isLink {
			if isBroken {
				warn++
			} else {
				ok++
			}
			continue
		}
		// Windows managed file links are hard links with no reparse point.
		// A managed rule shares its inode with the canonical source (link
		// count > 1); a standalone regular file dropped here does not and is
		// skipped, matching the POSIX "Readlink fails -> skip" behavior.
		if hasMultipleHardLinks(linkPath) {
			ok++
		}
	}
	return ok, warn
}

// codexTextBadge counts AGENTS.md / codex-config / hooks managed files.
func codexTextBadge(path string) platformBadge {
	codexOK, codexWarn := 0, 0
	addManagedCounts(&codexOK, &codexWarn, []string{
		filepath.Join(path, statusAgentsMarkdown),
		filepath.Join(path, statusCodexDir, statusCodexConfigToml),
		filepath.Join(path, statusCodexDir, statusHooksJSON),
	}, []string{
		filepath.Join(path, statusCodexDir, "agents"),
		filepath.Join(path, statusAgentsDir, "skills"),
	})
	return platformBadge{"Codex", codexOK > 0, codexWarn > 0}
}

// opencodeTextBadge counts opencode.json plus its sibling agent/skill dirs.
func opencodeTextBadge(path string) platformBadge {
	opencodeOK, opencodeWarn := 0, 0
	addManagedCounts(&opencodeOK, &opencodeWarn, []string{
		filepath.Join(path, statusOpenCodeJSON),
	}, []string{
		filepath.Join(path, statusOpenCodeDir, "agent"),
		filepath.Join(path, statusAgentsDir, "skills"),
	})
	return platformBadge{"OpenCode", opencodeOK > 0, opencodeWarn > 0}
}

// copilotTextBadge counts copilot-instructions / mcp / settings files.
func copilotTextBadge(path string) platformBadge {
	copilotOK, copilotWarn := 0, 0
	addManagedCounts(&copilotOK, &copilotWarn, []string{
		filepath.Join(path, statusGitHubDir, statusCopilotInstructions),
		filepath.Join(path, statusVSCodeDir, statusCopilotMCPJSON),
		filepath.Join(path, statusClaudeDir, statusClaudeSettingsLocalJSON),
	}, []string{
		filepath.Join(path, statusGitHubDir, "agents"),
		filepath.Join(path, statusGitHubDir, "hooks"),
		filepath.Join(path, statusAgentsDir, "skills"),
	})
	return platformBadge{"Copilot", copilotOK > 0, copilotWarn > 0}
}

func printStatusProjectManifestSummary(path string) {
	rc, rcErr := config.LoadAgentsRC(path)
	if rcErr != nil {
		fmt.Fprintf(os.Stdout, "  %s○%s manifest  %snot found — run: da install --generate%s\n",
			ui.Yellow, ui.Reset, ui.Dim, ui.Reset)
		return
	}
	sourceDesc := "local"
	for _, src := range rc.Sources {
		if src.Type == "git" && src.URL != "" {
			u := src.URL
			for _, prefix := range []string{"https://", "http://", "git@"} {
				u = strings.TrimPrefix(u, prefix)
			}
			u = strings.TrimSuffix(u, ".git")
			sourceDesc = "git: " + u
			break
		}
	}
	parts := []string{}
	if len(rc.Skills) > 0 {
		parts = append(parts, fmt.Sprintf("%d skill(s)", len(rc.Skills)))
	}
	if len(rc.Agents) > 0 {
		parts = append(parts, fmt.Sprintf("%d agent(s)", len(rc.Agents)))
	}
	if rc.Hooks.IsEnabled() {
		parts = append(parts, "hooks")
	}
	if rc.MCP.IsEnabled() {
		parts = append(parts, "mcp")
	}
	detail := sourceDesc
	if len(parts) > 0 {
		detail += "  •  " + strings.Join(parts, "  ")
	}
	fmt.Fprintf(os.Stdout, "  %s✓%s manifest  %s%s%s\n",
		ui.Green, ui.Reset, ui.Dim, detail, ui.Reset)
}

func runStatus(audit bool, agentFilter string, deps statusConfigLoader) error {
	cfg, err := deps.LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	agentsHome := config.AgentsHome()

	if Flags.JSON {
		report, err := buildStatusJSONReport(cfg, agentsHome, agentFilter)
		if err != nil {
			return err
		}
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal status json: %w", err)
		}
		fmt.Fprintln(os.Stdout, string(data))
		return nil
	}

	displayHome := config.DisplayPath(agentsHome)

	ui.Header("da status")
	fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Dim, displayHome, ui.Reset)

	printAgentsHomeGitStatusLine(agentsHome)

	printCanonicalStoreSection(agentsHome)

	// User-level config summary (home directory)
	printUserConfigSection(agentsHome, audit, agentFilter)

	names := cfg.ListProjects()
	sort.Strings(names)

	if len(names) == 0 {
		fmt.Fprintln(os.Stdout, "\n  No managed projects.")
		fmt.Fprintln(os.Stdout, "  Add one with: da add <path>")
		return nil
	}

	for _, name := range names {
		printStatusProjectBlock(name, cfg, agentsHome, audit, agentFilter)
	}

	fmt.Fprintln(os.Stdout)
	return nil
}

// printStatusProjectBlock prints one managed project's status entry:
// header, optional path line (suppressed when path matches ~/name),
// missing-directory bullet, badge row, manifest summary, last-refreshed
// timestamp, and the audit block when requested.
func printStatusProjectBlock(name string, cfg *config.Config, agentsHome string, audit bool, agentFilter string) {
	path := cfg.GetProjectPath(name)
	displayPath := config.DisplayPath(path)

	fmt.Fprintf(os.Stdout, "\n  %s%s%s\n", ui.Bold, name, ui.Reset)

	homeDir, _ := config.UserHomeDir()
	expectedSimplePath := "~/" + name
	actualDisplayPath := strings.Replace(path, homeDir, "~", 1)
	if actualDisplayPath != expectedSimplePath {
		fmt.Fprintf(os.Stdout, "  %s%s%s\n", ui.Dim, displayPath, ui.Reset)
	}

	if _, err := os.Stat(path); err != nil {
		ui.Bullet("error", "Directory not found")
		return
	}

	printBadgeRow(collectProjectTextBadges(path, agentsHome))
	printStatusProjectManifestSummary(path)

	if ts := readRefreshTimestamp(path); ts != "" {
		fmt.Fprintf(os.Stdout, "  %slast refreshed: %s%s\n", ui.Dim, ts, ui.Reset)
	}

	if audit {
		printAudit(name, path, agentsHome, agentFilter, cfg)
	}
}

func buildStatusJSONReport(cfg *config.Config, agentsHome, agentFilter string) (*statusJSONReport, error) {
	report := &statusJSONReport{
		AgentsHome:     agentsHome,
		Git:            statusGitInfo(agentsHome),
		CanonicalStore: make(map[string]statusJSONItem),
		UserConfig:     collectUserConfigPlatforms(agentFilter),
	}

	for _, bucket := range platform.CanonicalStoreBucketSpecs() {
		root := platform.CanonicalBucketRoot(agentsHome, bucket.Name)
		scopes, items := summarizeCanonicalBucket(root, bucket.CountDirs, bucket.MarkerFile)
		report.CanonicalStore[string(bucket.Name)] = statusJSONItem{Scopes: scopes, Items: items}
	}

	specs, err := platform.ListPluginSpecs(agentsHome, "")
	if err == nil {
		for _, spec := range specs {
			scope := spec.Scope
			if scope == "" {
				scope = "global"
			}
			report.Plugins = append(report.Plugins, statusJSONPlugin{Name: spec.Name, Scope: scope})
		}
	}

	names := cfg.ListProjects()
	sort.Strings(names)
	for _, name := range names {
		path := cfg.GetProjectPath(name)
		project := statusJSONProject{
			Name:          name,
			Path:          path,
			PathExists:    pathExists(path),
			Platforms:     collectProjectPlatforms(path),
			ManifestFound: pathExists(filepath.Join(path, config.AgentsRCFile)),
			LastRefreshed: readRefreshTimestamp(path),
		}
		report.Projects = append(report.Projects, project)
	}

	return report, nil
}

func statusGitInfo(agentsHome string) statusJSONGit {
	g := probeAgentsHomeGit(agentsHome)
	if !g.IsRepo {
		return statusJSONGit{}
	}
	return statusJSONGit{Initialized: true, Branch: g.Branch, Remote: g.Remote}
}

func collectUserConfigPlatforms(agentFilter string) []statusJSONPlatform {
	homeDir, err := config.UserHomeDir()
	if err != nil {
		return nil
	}

	var out []statusJSONPlatform
	if agentFilter == "" || agentFilter == "claude" {
		out = appendPlatformIfPresent(out, "Claude", countPlatformHealth(
			[]string{
				filepath.Join(homeDir, statusClaudeDir, "CLAUDE.md"),
				filepath.Join(homeDir, statusClaudeDir, statusClaudeSettingsJSON),
			},
			[]string{
				filepath.Join(homeDir, statusClaudeDir, "agents"),
				filepath.Join(homeDir, statusClaudeDir, "skills"),
			},
		))
	}
	if agentFilter == "" || agentFilter == "codex" {
		out = appendPlatformIfPresent(out, "Codex", countPlatformHealth(
			[]string{
				filepath.Join(homeDir, statusCodexDir, statusHooksJSON),
			},
			[]string{
				filepath.Join(homeDir, statusCodexDir, "agents"),
				filepath.Join(homeDir, statusAgentsDir, "skills"),
			},
		))
	}
	if agentFilter == "" || agentFilter == "opencode" {
		out = appendPlatformIfPresent(out, "OpenCode", countPlatformHealth(nil, []string{
			filepath.Join(homeDir, statusOpenCodeDir, "agent"),
		}))
	}
	return out
}

func collectProjectPlatforms(path string) []statusJSONPlatform {
	return []statusJSONPlatform{
		platformStatus("Cursor", countPlatformHealth(
			[]string{
				filepath.Join(path, statusCursorDir, statusCopilotMCPJSON),
				filepath.Join(path, statusCursorDir, statusClaudeSettingsJSON),
				filepath.Join(path, statusCursorDir, statusHooksJSON),
				filepath.Join(path, ".cursorignore"),
			},
			[]string{
				filepath.Join(path, statusCursorDir, "rules"),
			},
		)),
		platformStatus("Claude", countPlatformHealth(
			[]string{
				filepath.Join(path, statusClaudeMCPJSON),
				filepath.Join(path, statusClaudeDir, statusClaudeSettingsLocalJSON),
			},
			[]string{
				filepath.Join(path, statusClaudeDir, "rules"),
				filepath.Join(path, statusClaudeDir, "agents"),
				filepath.Join(path, statusClaudeDir, "skills"),
			},
		)),
		platformStatus("Codex", countPlatformHealth(
			[]string{
				filepath.Join(path, statusAgentsMarkdown),
				filepath.Join(path, statusCodexDir, statusCodexConfigToml),
				filepath.Join(path, statusCodexDir, statusHooksJSON),
			},
			[]string{
				filepath.Join(path, statusCodexDir, "agents"),
				filepath.Join(path, statusAgentsDir, "skills"),
			},
		)),
		platformStatus("OpenCode", countPlatformHealth(
			[]string{
				filepath.Join(path, statusOpenCodeJSON),
			},
			[]string{
				filepath.Join(path, statusOpenCodeDir, "agent"),
				filepath.Join(path, statusAgentsDir, "skills"),
			},
		)),
		platformStatus("Copilot", countPlatformHealth(
			[]string{
				filepath.Join(path, statusGitHubDir, statusCopilotInstructions),
				filepath.Join(path, statusVSCodeDir, statusCopilotMCPJSON),
				filepath.Join(path, statusClaudeDir, statusClaudeSettingsLocalJSON),
			},
			[]string{
				filepath.Join(path, statusGitHubDir, "agents"),
				filepath.Join(path, statusGitHubDir, "hooks"),
				filepath.Join(path, statusAgentsDir, "skills"),
			},
		)),
	}
}

func countPlatformHealth(files, dirs []string) platformBadge {
	okCount, warnCount := 0, 0
	addManagedCounts(&okCount, &warnCount, files, dirs)
	return platformBadge{present: okCount > 0, broken: warnCount > 0}
}

func platformStatus(name string, badge platformBadge) statusJSONPlatform {
	return statusJSONPlatform{Name: name, Present: badge.present, Broken: badge.broken}
}

func appendPlatformIfPresent(out []statusJSONPlatform, name string, badge platformBadge) []statusJSONPlatform {
	if !badge.present && !badge.broken {
		return out
	}
	return append(out, platformStatus(name, badge))
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func printCanonicalStoreSection(agentsHome string) {
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "  Canonical Store")

	for _, bucket := range platform.CanonicalStoreBucketSpecs() {
		root := platform.CanonicalBucketRoot(agentsHome, bucket.Name)
		scopes, entries := summarizeCanonicalBucket(root, bucket.CountDirs, bucket.MarkerFile)
		if scopes == 0 && entries == 0 {
			fmt.Fprintf(os.Stdout, "  %s-%s %-14s %s(empty)%s\n", ui.Dim, ui.Reset, bucket.Name, ui.Dim, ui.Reset)
			continue
		}
		fmt.Fprintf(os.Stdout, "  %s✓%s %-14s %s%d scope(s), %d item(s)%s\n", ui.Green, ui.Reset, bucket.Name, ui.Dim, scopes, entries, ui.Reset)
	}

	printPluginsSection(agentsHome)
}

func printPluginsSection(agentsHome string) {
	specs, err := platform.ListPluginSpecs(agentsHome, "")
	if err != nil || len(specs) == 0 {
		return
	}

	ui.Section("Plugins")
	for _, spec := range specs {
		scope := spec.Scope
		if scope == "" {
			scope = "global"
		}
		fmt.Fprintf(os.Stdout, "  %s  [%s]\n", spec.Name, scope)
	}
}

func summarizeCanonicalBucket(root string, countDirs bool, markerFile string) (int, int) {
	scopeDirs, err := os.ReadDir(root)
	if err != nil {
		return 0, 0
	}
	scopeCount, itemCount := 0, 0
	for _, scopeDir := range scopeDirs {
		scopePath := filepath.Join(root, scopeDir.Name())
		if !links.IsDirEntry(scopePath) {
			continue
		}
		n := summarizeCanonicalScope(scopePath, countDirs, markerFile)
		if n > 0 {
			scopeCount++
			itemCount += n
		}
	}
	return scopeCount, itemCount
}

func summarizeCanonicalScope(scopePath string, countDirs bool, markerFile string) int {
	entries, err := os.ReadDir(scopePath)
	if err != nil {
		return 0
	}
	if countDirs {
		return countCanonicalScopedDirs(scopePath, entries, markerFile)
	}
	return countCanonicalScopedFiles(entries)
}

func countCanonicalScopedDirs(scopePath string, entries []os.DirEntry, markerFile string) int {
	count := 0
	for _, entry := range entries {
		dirPath := filepath.Join(scopePath, entry.Name())
		if !links.IsDirEntry(dirPath) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dirPath, markerFile)); err == nil {
			count++
		}
	}
	return count
}

func countCanonicalScopedFiles(entries []os.DirEntry) int {
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			count++
		}
	}
	return count
}

func countManagedFileOK(path string, warn *int) int {
	if _, err := os.Lstat(path); err != nil {
		return 0
	}
	// A resolvable managed link (POSIX symlink / Windows junction): ok if its
	// target exists, otherwise a broken-link warning. A non-resolvable but
	// present path is a regular file or a Windows hard-linked managed file
	// (no reparse point) — a healthy managed reference, counted ok.
	if _, isLink, isBroken := managedLinkBroken(path); isLink {
		if isBroken {
			*warn = *warn + 1
			return 0
		}
		return 1
	}
	return 1
}

func addManagedCounts(ok, warn *int, files []string, dirs []string) {
	for _, path := range files {
		*ok += countManagedFileOK(path, warn)
	}
	for _, dir := range dirs {
		*ok += countManagedDirEntries(dir, warn)
	}
}

func countManagedDirEntries(dir string, warn *int) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	ok := 0
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if _, err := os.Lstat(path); err != nil {
			continue
		}
		if _, isLink, isBroken := managedLinkBroken(path); isLink {
			if isBroken {
				*warn = *warn + 1
			} else {
				ok++
			}
			continue
		}
		ok++
	}
	return ok
}

func printManagedAuditPath(path string, rel func(string) string) {
	if _, err := os.Lstat(path); err != nil {
		return
	}
	if dest, isLink, isBroken := managedLinkBroken(path); isLink {
		displayDest := config.DisplayPath(resolveLinkDest(path, dest))
		if isBroken {
			fmt.Fprintf(os.Stdout, "    %s✗%s %s %s→ %s (broken)%s\n", ui.Red, ui.Reset, rel(path), ui.Dim, displayDest, ui.Reset)
		} else {
			fmt.Fprintf(os.Stdout, "    %s✓%s %s %s→ %s%s\n", ui.Green, ui.Reset, rel(path), ui.Dim, displayDest, ui.Reset)
		}
		return
	}
	fmt.Fprintf(os.Stdout, statusLocalFileFmt, ui.Dim, ui.Reset, rel(path), ui.Dim, ui.Reset)
}

func printManagedAuditDir(dir string, rel func(string) string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		printManagedAuditPath(filepath.Join(dir, entry.Name()), rel)
	}
}

func printBadgeRow(badges []platformBadge) {
	fmt.Fprintf(os.Stdout, "  ")
	for i, badge := range badges {
		if i > 0 {
			fmt.Fprintf(os.Stdout, "  ")
		}
		if badge.broken {
			fmt.Fprintf(os.Stdout, "%s!%s %s", ui.Yellow, ui.Reset, badge.name)
		} else if badge.present {
			fmt.Fprintf(os.Stdout, "%s✓%s %s", ui.Green, ui.Reset, badge.name)
		} else {
			fmt.Fprintf(os.Stdout, "%s-%s %s%s%s", ui.Dim, ui.Reset, ui.Dim, badge.name, ui.Reset)
		}
	}
	fmt.Fprintln(os.Stdout)
}

// readRefreshTimestamp prefers refresh metadata in .agentsrc.json and falls back to
// the legacy .agents-refresh marker.
func readRefreshTimestamp(projectPath string) string {
	if rc, err := config.LoadAgentsRC(projectPath); err == nil && rc.Refresh != nil && rc.Refresh.RefreshedAt != "" {
		return formatRefreshDisplay(rc.Refresh.RefreshedAt)
	}
	return readLegacyRefreshTimestamp(projectPath)
}

func formatRefreshDisplay(ts string) string {
	// Simplify ISO timestamp: 2026-03-12T05:18:11Z → 2026-03-12 05:18 UTC
	ts = strings.Replace(ts, "T", " ", 1)
	ts = strings.TrimSuffix(ts, "Z")
	if len(ts) >= 16 {
		ts = ts[:16] + " UTC"
	}
	return ts
}

func readLegacyRefreshTimestamp(projectPath string) string {
	markerPath := filepath.Join(projectPath, ".agents-refresh")
	f, err := os.Open(markerPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "refreshed_at=") {
			return formatRefreshDisplay(strings.TrimPrefix(line, "refreshed_at="))
		}
	}
	return ""
}

func printAudit(name, path, agentsHome, agentFilter string, cfg *config.Config) {
	fmt.Fprintln(os.Stdout)

	if agentFilter == "" || agentFilter == "cursor" {
		printCursorAudit(name, path, agentsHome)
	}
	if agentFilter == "" || agentFilter == "claude" {
		printClaudeAudit(name, path, agentsHome)
	}
	if agentFilter == "" || agentFilter == "codex" {
		printCodexAudit(name, path, agentsHome)
	}
	if agentFilter == "" || agentFilter == "opencode" {
		printOpenCodeAudit(name, path, agentsHome)
	}
	if agentFilter == "" || agentFilter == "copilot" {
		printCopilotAudit(name, path)
	}
	printSharedTargetRegistry(name, path, cfg)
}

// sharedTargetRegistryPlanLines returns merged shared-target lines for status/doctor audit.
// It is the same builder path as refresh/install --dry-run (DryRunSharedTargetPlanLines).
// When plats is empty, returns (nil, nil).
func sharedTargetRegistryPlanLines(project, repo string, plats []platform.Platform) ([]string, error) {
	if len(plats) == 0 {
		return nil, nil
	}
	return platform.DryRunSharedTargetPlanLines(project, repo, plats)
}

// printSharedTargetRegistry lists the merged shared-target ResourcePlan lines using the same
// code path as refresh/install dry-run (DryRunSharedTargetPlanLines).
func printSharedTargetRegistry(project, repo string, cfg *config.Config) {
	plats := platform.InstalledEnabledPlatforms(cfg)
	if len(plats) == 0 {
		fmt.Fprintf(os.Stdout, "    %sShared target registry%s\n", ui.Cyan, ui.Reset)
		fmt.Fprintf(os.Stdout, "      %s(no enabled+installed platforms — nothing to plan)%s\n", ui.Dim, ui.Reset)
		fmt.Fprintln(os.Stdout)
		return
	}
	lines, err := sharedTargetRegistryPlanLines(project, repo, plats)
	fmt.Fprintf(os.Stdout, "    %sShared target registry%s %s(same merge rules as refresh --dry-run)%s\n", ui.Cyan, ui.Reset, ui.Dim, ui.Reset)
	if err != nil {
		fmt.Fprintf(os.Stdout, "      %s! %v%s\n", ui.Yellow, err, ui.Reset)
		fmt.Fprintln(os.Stdout)
		return
	}
	for _, line := range lines {
		fmt.Fprintf(os.Stdout, "      %s%s%s\n", ui.Dim, line, ui.Reset)
	}
	fmt.Fprintln(os.Stdout)
}

// printUserConfigSection reports on user-level (home directory) config links.
func printUserConfigSection(_ string, audit bool, agentFilter string) {
	homeDir, err := config.UserHomeDir()
	if err != nil {
		return
	}

	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "  User Config")

	var badges []platformBadge

	// Claude user-level config
	if agentFilter == "" || agentFilter == "claude" {
		claudeHome := filepath.Join(homeDir, statusClaudeDir)
		claudeMD := filepath.Join(claudeHome, "CLAUDE.md")
		claudeSettings := filepath.Join(claudeHome, statusClaudeSettingsJSON)
		claudeAgents := filepath.Join(claudeHome, "agents")
		claudeSkills := filepath.Join(claudeHome, "skills")
		badges = appendUserConfigPlatformBadge(badges, "Claude", homeDir, audit,
			[]userConfigRef{
				{path: claudeMD, isDir: false},
				{path: claudeSettings, isDir: false},
				{path: claudeAgents, isDir: true},
				{path: claudeSkills, isDir: true},
			},
			[]string{claudeMD, claudeSettings},
			[]string{claudeAgents, claudeSkills},
		)
	}

	// Codex user-level config
	if agentFilter == "" || agentFilter == "codex" {
		codexAgents := filepath.Join(homeDir, statusCodexDir, "agents")
		codexHooks := filepath.Join(homeDir, statusCodexDir, statusHooksJSON)
		codexSkills := filepath.Join(homeDir, statusAgentsDir, "skills")
		badges = appendUserConfigPlatformBadge(badges, "Codex", homeDir, audit,
			[]userConfigRef{
				{path: codexAgents, isDir: true},
				{path: codexHooks, isDir: false},
				{path: codexSkills, isDir: true},
			},
			[]string{codexHooks},
			[]string{codexAgents, codexSkills},
		)
	}

	// OpenCode user-level config
	if agentFilter == "" || agentFilter == "opencode" {
		opencodeAgent := filepath.Join(homeDir, statusOpenCodeDir, "agent")
		badges = appendUserConfigPlatformBadge(badges, "OpenCode", homeDir, audit,
			[]userConfigRef{{path: opencodeAgent, isDir: true}},
			nil,
			[]string{opencodeAgent},
		)
	}

	// Badge row
	if len(badges) == 0 {
		fmt.Fprintf(os.Stdout, "  %s-%s %s(no managed user-level config detected)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
		fmt.Fprintln(os.Stdout)
		return
	}

	printBadgeRow(badges)
	fmt.Fprintln(os.Stdout)
}

// userConfigRef is one managed reference (file or directory) in the
// per-platform user-config audit block — the (path, isDir) pair lets
// appendUserConfigPlatformBadge dispatch the right print helper while
// preserving the original interleaved file/dir order per platform.
type userConfigRef struct {
	path  string
	isDir bool
}

// appendUserConfigPlatformBadge counts managed files/dirs for one
// user-level platform and appends a platformBadge if anything was detected.
// When audit is on, prints the per-file/dir audit detail in auditOrder,
// preserving the prior inline blocks' platform-specific ordering (Claude:
// files then dirs; Codex: dir, file, dir; OpenCode: dir). Returns the
// (possibly extended) badge slice.
func appendUserConfigPlatformBadge(badges []platformBadge, label, homeDir string, audit bool, auditOrder []userConfigRef, files, dirs []string) []platformBadge {
	ok, warn := 0, 0
	addManagedCounts(&ok, &warn, files, dirs)
	if ok+warn > 0 {
		badges = append(badges, platformBadge{label, ok > 0, warn > 0})
	}
	if audit {
		displayBase := homeDir + string(os.PathSeparator)
		rel := func(p string) string { return strings.TrimPrefix(p, displayBase) }
		for _, ref := range auditOrder {
			if ref.isDir {
				printManagedAuditDir(ref.path, rel)
			} else {
				printManagedAuditPath(ref.path, rel)
			}
		}
	}
	return badges
}

func printCursorAudit(name, path, agentsHome string) {
	fmt.Fprintf(os.Stdout, "    %sCursor%s\n", ui.Cyan, ui.Reset)
	rulesDir := filepath.Join(path, statusCursorDir, "rules")
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		fmt.Fprintf(os.Stdout, "      %s(no .cursor/rules/)%s\n", ui.Dim, ui.Reset)
		return
	}
	count := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".mdc") || strings.Contains(e.Name(), ".dot-agents-backup") {
			continue
		}
		f := filepath.Join(rulesDir, e.Name())
		var srcType, linkedTo string
		if strings.HasPrefix(e.Name(), globalRulesPrefix) {
			srcType = "global"
			srcName := strings.TrimPrefix(e.Name(), globalRulesPrefix)
			linkedTo = "~/.agents/rules/global/" + srcName
		} else if strings.HasPrefix(e.Name(), name+"--") {
			srcType = "project"
			srcName := strings.TrimPrefix(e.Name(), name+"--")
			linkedTo = "~/.agents/rules/" + name + "/" + srcName
		} else {
			srcType = "local"
		}

		if srcType == "local" {
			fmt.Fprintf(os.Stdout, "      %s○%s %s %s(local file)%s\n", ui.Dim, ui.Reset, e.Name(), ui.Dim, ui.Reset)
		} else {
			srcPath := strings.Replace(linkedTo, "~/.agents", agentsHome, 1)
			srcPath = strings.Replace(srcPath, "~", os.Getenv("HOME"), 1)
			if linked, _ := links.AreHardlinked(f, srcPath); linked {
				fmt.Fprintf(os.Stdout, "      %s✓%s %s %s← %s%s\n", ui.Green, ui.Reset, e.Name(), ui.Dim, linkedTo, ui.Reset)
			} else {
				fmt.Fprintf(os.Stdout, "      %s!%s %s %s(not linked to %s)%s\n", ui.Yellow, ui.Reset, e.Name(), ui.Dim, linkedTo, ui.Reset)
			}
		}
		count++
	}
	if count == 0 {
		fmt.Fprintf(os.Stdout, "      %s(no rules)%s\n", ui.Dim, ui.Reset)
	}
	// Cursor MCP link (.cursor/mcp.json)
	cursorMCPPath := filepath.Join(path, statusCursorDir, statusCopilotMCPJSON)
	if _, err := os.Lstat(cursorMCPPath); err == nil {
		if dest, isLink, isBroken := managedLinkBroken(cursorMCPPath); isLink {
			displayDest := config.DisplayPath(resolveLinkDest(cursorMCPPath, dest))
			if isBroken {
				fmt.Fprintf(os.Stdout, "      %s✗%s .cursor/mcp.json %s→ %s (broken)%s\n", ui.Red, ui.Reset, ui.Dim, displayDest, ui.Reset)
			} else {
				fmt.Fprintf(os.Stdout, "      %s✓%s .cursor/mcp.json %s→ %s%s\n", ui.Green, ui.Reset, ui.Dim, displayDest, ui.Reset)
			}
		} else {
			fmt.Fprintf(os.Stdout, "      %s✓%s .cursor/mcp.json %s(hard link or local file)%s\n", ui.Green, ui.Reset, ui.Dim, ui.Reset)
		}
	} else {
		fmt.Fprintf(os.Stdout, "      %s-%s .cursor/mcp.json %s(not linked)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
	}
	fmt.Fprintln(os.Stdout)
}

// printSymlinkDirAudit reads dir for symlink entries and prints each entry's
// status. The nameFormat is a printf format applied to the entry name (e.g.
// "%s" or ".opencode/agent/%s"). The emptyLabel is shown after the ○ marker
// when no symlinks were found. Returns the number of OK and broken entries.
func printSymlinkDirAudit(dir, emptyLabel, nameFormat string) (int, int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	okCount, brokenCount := 0, 0
	for _, e := range entries {
		linkPath := filepath.Join(dir, e.Name())
		dest, isLink, isBroken := managedLinkBroken(linkPath)
		if !isLink {
			continue
		}
		displayDest := config.DisplayPath(resolveLinkDest(linkPath, dest))
		display := fmt.Sprintf(nameFormat, e.Name())
		if isBroken {
			fmt.Fprintf(os.Stdout, statusAuditLinkBrokenFormat, ui.Red, ui.Reset, display, ui.Dim, displayDest, ui.Reset)
			brokenCount++
		} else {
			fmt.Fprintf(os.Stdout, statusAuditLinkOkFormat, ui.Green, ui.Reset, display, ui.Dim, displayDest, ui.Reset)
			okCount++
		}
	}
	if okCount == 0 && brokenCount == 0 {
		fmt.Fprintf(os.Stdout, "      %s○%s %s %s(empty)%s\n", ui.Dim, ui.Reset, emptyLabel, ui.Dim, ui.Reset)
	}
	return okCount, brokenCount
}

// printSymlinkAudit reads a single symlink and prints its ✓/✗/(not linked)
// status with the supplied display label.
func printSymlinkAudit(linkPath, label string) {
	if dest, isLink, isBroken := managedLinkBroken(linkPath); isLink {
		displayDest := config.DisplayPath(resolveLinkDest(linkPath, dest))
		if isBroken {
			fmt.Fprintf(os.Stdout, statusAuditLinkBrokenFormat, ui.Red, ui.Reset, label, ui.Dim, displayDest, ui.Reset)
		} else {
			fmt.Fprintf(os.Stdout, statusAuditLinkOkFormat, ui.Green, ui.Reset, label, ui.Dim, displayDest, ui.Reset)
		}
	} else {
		fmt.Fprintf(os.Stdout, "      %s-%s %s %s(not linked)%s\n", ui.Dim, ui.Reset, label, ui.Dim, ui.Reset)
	}
}

func printClaudeAudit(_, path, _ string) {
	fmt.Fprintf(os.Stdout, "    %sClaude Code%s\n", ui.Cyan, ui.Reset)
	rulesDir := filepath.Join(path, statusClaudeDir, "rules")
	if _, err := os.ReadDir(rulesDir); err != nil {
		fmt.Fprintf(os.Stdout, "      %s(no %s/rules/)%s\n", ui.Dim, statusClaudeDir, ui.Reset)
		fmt.Fprintln(os.Stdout)
		return
	}
	printSymlinkDirAudit(rulesDir, statusClaudeDir+"/rules/", "%s")
	// Claude MCP link (.mcp.json)
	printSymlinkAudit(filepath.Join(path, statusClaudeMCPJSON), ".mcp.json")
	fmt.Fprintln(os.Stdout)
}

func printCodexAudit(_, path, _ string) {
	fmt.Fprintf(os.Stdout, "    %sCodex%s\n", ui.Cyan, ui.Reset)
	printCodexAgentsMD(filepath.Join(path, statusAgentsMarkdown))
	printCodexSymlinkAudit(filepath.Join(path, statusCodexDir, statusCodexConfigToml), ".codex/config.toml")
	printCodexSymlinkAudit(filepath.Join(path, statusCodexDir, statusHooksJSON), ".codex/hooks.json")
	printCodexSkillsAudit(filepath.Join(path, statusAgentsDir, "skills"))
	printCodexAgentsAudit(filepath.Join(path, statusCodexDir, "agents"))
	fmt.Fprintln(os.Stdout)
}

func printCodexAgentsMD(path string) {
	if _, err := os.Lstat(path); err == nil {
		if _, isLink, _ := managedLinkBroken(path); isLink {
			printLinkedStatusLine(statusAgentsMarkdown, path)
			return
		}
		fmt.Fprintf(os.Stdout, "      %s○%s %s %s(local file)%s\n", ui.Dim, ui.Reset, statusAgentsMarkdown, ui.Dim, ui.Reset)
		return
	}
	fmt.Fprintf(os.Stdout, "      %s(no %s)%s\n", ui.Dim, statusAgentsMarkdown, ui.Reset)
}

func printCodexSymlinkAudit(path, label string) {
	if _, isLink, _ := managedLinkBroken(path); isLink {
		printLinkedStatusLine(label, path)
		return
	}
	fmt.Fprintf(os.Stdout, "      %s-%s %s %s(not linked)%s\n", ui.Dim, ui.Reset, label, ui.Dim, ui.Reset)
}

func printCodexSkillsAudit(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	okCount, brokenCount := 0, 0
	for _, entry := range entries {
		linkPath := filepath.Join(dir, entry.Name())
		if _, isLink, _ := managedLinkBroken(linkPath); !isLink {
			continue
		}
		if printLinkedStatusLine(".agents/skills/"+entry.Name(), linkPath) {
			okCount++
		} else {
			brokenCount++
		}
	}
	if okCount == 0 && brokenCount == 0 {
		fmt.Fprintf(os.Stdout, "      %s○%s .agents/skills/ %s(empty)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
	}
}

func printCodexAgentsAudit(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	okCount, brokenCount := 0, 0
	for _, entry := range entries {
		linkPath := filepath.Join(dir, entry.Name())
		if _, err := os.Stat(linkPath); err == nil {
			fmt.Fprintf(os.Stdout, "      %s✓%s .codex/agents/%s %s(native TOML)%s\n", ui.Green, ui.Reset, entry.Name(), ui.Dim, ui.Reset)
			okCount++
		} else {
			fmt.Fprintf(os.Stdout, "      %s✗%s .codex/agents/%s %s(unreadable)%s\n", ui.Red, ui.Reset, entry.Name(), ui.Dim, ui.Reset)
			brokenCount++
		}
	}
	if okCount == 0 && brokenCount == 0 {
		fmt.Fprintf(os.Stdout, "      %s○%s .codex/agents/ %s(empty)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
	}
}

func printLinkedStatusLine(label, linkPath string) bool {
	dest, _, isBroken := managedLinkBroken(linkPath)
	displayDest := config.DisplayPath(resolveLinkDest(linkPath, dest))
	if !isBroken {
		fmt.Fprintf(os.Stdout, statusAuditLinkOkFormat, ui.Green, ui.Reset, label, ui.Dim, displayDest, ui.Reset)
		return true
	}
	fmt.Fprintf(os.Stdout, statusAuditLinkBrokenFormat, ui.Red, ui.Reset, label, ui.Dim, displayDest, ui.Reset)
	return false
}

func printOpenCodeAudit(_, path, _ string) {
	fmt.Fprintf(os.Stdout, "    %sOpenCode%s\n", ui.Cyan, ui.Reset)

	// opencode.json symlink
	opencodeJSON := filepath.Join(path, statusOpenCodeJSON)
	if _, err := os.Lstat(opencodeJSON); err == nil {
		if dest, isLink, isBroken := managedLinkBroken(opencodeJSON); isLink {
			displayDest := config.DisplayPath(resolveLinkDest(opencodeJSON, dest))
			if isBroken {
				fmt.Fprintf(os.Stdout, "      %s✗%s opencode.json %s→ %s (broken)%s\n", ui.Red, ui.Reset, ui.Dim, displayDest, ui.Reset)
			} else {
				fmt.Fprintf(os.Stdout, "      %s✓%s opencode.json %s→ %s%s\n", ui.Green, ui.Reset, ui.Dim, displayDest, ui.Reset)
			}
		} else {
			fmt.Fprintf(os.Stdout, "      %s○%s opencode.json %s(local file)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
		}
	}

	// .opencode/agent/ directory
	opencodeAgentDir := filepath.Join(path, statusOpenCodeDir, "agent")
	if _, err := os.ReadDir(opencodeAgentDir); err == nil {
		printSymlinkDirAudit(opencodeAgentDir, ".opencode/agent/", ".opencode/agent/%s")
	} else {
		fmt.Fprintf(os.Stdout, "      %s(no .opencode/)%s\n", ui.Dim, ui.Reset)
	}
	fmt.Fprintln(os.Stdout)
}

func printCopilotAudit(_, path string) {
	fmt.Fprintf(os.Stdout, "    %sGitHub Copilot%s\n", ui.Cyan, ui.Reset)
	instructionsPath := filepath.Join(path, statusGitHubDir, statusCopilotInstructions)
	if _, err := os.Lstat(instructionsPath); err == nil {
		if dest, isLink, isBroken := managedLinkBroken(instructionsPath); isLink {
			displayDest := config.DisplayPath(resolveLinkDest(instructionsPath, dest))
			if isBroken {
				fmt.Fprintf(os.Stdout, "      %s✗%s .github/copilot-instructions.md %s→ %s (broken)%s\n", ui.Red, ui.Reset, ui.Dim, displayDest, ui.Reset)
			} else {
				fmt.Fprintf(os.Stdout, "      %s✓%s .github/copilot-instructions.md %s→ %s%s\n", ui.Green, ui.Reset, ui.Dim, displayDest, ui.Reset)
			}
		}
	} else {
		fmt.Fprintf(os.Stdout, "      %s-%s .github/copilot-instructions.md %s(not linked)%s\n", ui.Dim, ui.Reset, ui.Dim, ui.Reset)
	}
	// Copilot MCP link (.vscode/mcp.json)
	printSymlinkAudit(filepath.Join(path, statusVSCodeDir, statusCopilotMCPJSON), ".vscode/mcp.json")
	fmt.Fprintln(os.Stdout)
}
