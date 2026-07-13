package lifecycle

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sys/execabs"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/gitremote"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

const (
	statusHooksJSON               = "hooks.json"
	statusCodexDir                = ".codex"
	statusAgentsDir               = ".agents"
	statusOpenCodeDir             = ".opencode"
	statusLocalFileFmt            = "    %s○%s %s %s(local file)%s\n"
	statusClaudeDir               = ".claude"
	statusClaudeSettingsLocalJSON = "settings.local.json"
	statusClaudeSettingsJSON      = "settings.json"
	// statusAuditLinkOkFormat and statusAuditLinkBrokenFormat are shared by
	// the surviving printSymlinkDirAudit helper (still exported as
	// PrintSymlinkDirAudit) so its audit output stays byte-identical. The
	// per-platform audit renderers themselves moved to the
	// internal/platform/<name>.go PrintAudit implementations in Phase 5.
	// Keep the 6-leading-space indentation; tests rely on it.
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

// statusJSONProject is the fleet/link-health view of a managed project. Per
// §7A.6, status sheds all config inspection (manifest presence, lockfile drift,
// last-refresh metadata) — `da config explain` is the single effective-config
// truth surface. This struct therefore carries only project identity and
// per-platform link health.
type statusJSONProject struct {
	Name       string               `json:"name"`
	Path       string               `json:"path"`
	PathExists bool                 `json:"path_exists"`
	Platforms  []statusJSONPlatform `json:"platforms"`
}

// StatusConfigLoader is the narrow collaborator status.go's fault-injectable
// LoadConfig operation needs (interface-DI per docs/TEST_SEAMS.md).
// Single-method, file-prefixed -er form; file-scoped — do not share with
// other commands files.
//
// Exported during the t08→t11 window so commands/seams_test.go (still in root
// until t11 splits it per cluster) can construct test doubles via the root
// shim's RunStatus entry point.
type StatusConfigLoader interface {
	LoadConfig() (*config.Config, error)
}

// stdStatusConfigLoader is the production StatusConfigLoader backed by
// internal/config.Load.
type stdStatusConfigLoader struct{}

func (stdStatusConfigLoader) LoadConfig() (*config.Config, error) { return config.Load() }

// resolveLinkDest normalizes a managed-link target to an absolute path so it
// can be stat'd. Junction targets are already absolute; POSIX symlinks may be
// relative to the link's directory.
//
// Duplicated from commands/doctor.go for the t08→t09 window per SHAPE.md OD-2.
// doctor.go (still in root) keeps its definition. Once t09 moves doctor into
// this package, the duplicate is collapsed (the doctor copy is removed and
// this becomes the canonical lifecycle definition).
func resolveLinkDest(linkPath, dest string) string {
	if dest == "" || filepath.IsAbs(dest) {
		return dest
	}
	return filepath.Clean(filepath.Join(filepath.Dir(linkPath), dest))
}

// managedLinkBroken reports, for a single managed link path, whether it is a
// resolvable managed link (POSIX symlink / Windows junction), its resolved
// target for display, and whether that target is missing (the link is
// broken).
//
// A Windows hard-linked managed *file* has no reparse point and therefore no
// resolvable target — ManagedLinkTarget returns ("", false). Such a file
// cannot dangle (its target inode must exist), so it is reported isLink=false
// and broken=false here; the OK-count path handles healthy hard links via
// links.AreHardlinked instead. This keeps POSIX behavior identical (symlinks
// still resolve via ManagedLinkTarget) while not misreporting Windows hard
// links as broken.
//
// Duplicated from commands/doctor.go for the t08→t09 window per SHAPE.md OD-2;
// see resolveLinkDest above.
func managedLinkBroken(linkPath string) (dest string, isLink, broken bool) {
	raw, ok := links.ManagedLinkTarget(linkPath)
	if !ok {
		return "", false, false
	}
	resolved := resolveLinkDest(linkPath, raw)
	if _, err := os.Stat(resolved); err != nil {
		return raw, true, true
	}
	return raw, true, false
}

// statusNoArgsHint mirrors commands.NoArgsWithHints but uses the deps-supplied
// UsageError helper so the lifecycle subpackage avoids importing the parent
// commands package (which would create an import cycle since commands imports
// lifecycle). NoArgsWithHints is not in lifecycle.Deps (only ExactArgs,
// MaximumNArgs, RangeArgs are) so status's "no positional args" check is
// implemented locally to preserve the original UX message.
func statusNoArgsHint(deps Deps, hint string) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return nil
		}
		return deps.UsageError(
			fmt.Sprintf("%s does not accept positional arguments (got %d)", cmd.CommandPath(), len(args)),
			fmt.Sprintf("Usage: %s", cmd.UseLine()),
			hint,
		)
	}
}

// statusExampleBlock joins example lines with newlines. Inlined from
// commands.ExampleBlock so lifecycle does not import the parent package.
func statusExampleBlock(lines ...string) string {
	return strings.Join(lines, "\n")
}

// NewStatusCmd builds the `da status` cobra command. The jsonOutput closure
// reports whether to emit the JSON variant — the parent shim in
// commands/status.go passes `func() bool { return commands.Flags.JSON }` so
// the package-var seam stays at the root while lifecycle stays import-cycle
// free. Tests can pass their own closure to exercise either path.
//
// ── t13a constructor-shape decision ────────────────────────────────────────
//
// NewStatusCmd retains the second jsonOutput func() bool argument (option (c)
// in the t13a fold-back observation at .agents/active/fold-back/
// t13a-respawn-lifecycle-shims-not-passthroughs.yaml) rather than folding it
// into Deps. Rationale: the parent commands/status.go shim's RunE-override
// reads Flags.JSON through the jsonFlag closure inside the commands package
// so the globalflagcov static analyzer (which loads ./commands but not
// ./commands/lifecycle) sees the Flags.JSON load it requires for handler
// coverage. Threading jsonOutput through Deps would move that read into
// lifecycle and silently drop the coverage. T13b's worker can pass
// `lifecycle.NewStatusCmd(buildStatusDeps(), func() bool { return Flags.JSON })`
// without further wrapping, or widen globalflagcov's load set to include
// ./commands/lifecycle (preferred long-term — see t13 PR description). The
// jsonOutput argument also lets tests exercise both JSON and text paths
// without mutating any package var.
//
// NewStatusCmd intentionally does NOT call applyDepsToGlobals because the
// moved status helpers do not read the lifecycle.Flags / .Version / .Commit /
// .Describe / .ErrorWithHintsFn package vars — status only consumes Deps
// directly (UsageError via statusNoArgsHint) plus the jsonOutput closure.
// Install/doctor/init all sync because their moved RunE bodies read the
// package vars; status does not.
func NewStatusCmd(deps Deps, jsonOutput func() bool) *cobra.Command {
	var audit bool
	var agentFilter string

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show managed projects and link health",
		Long: `Summarizes the shared ~/.agents/ store, managed projects, and per-platform
link health so you can quickly see whether the managed links are present or broken.

Status is fleet/link-health only. For effective-config detail (manifest sources,
declared skills/agents/hooks/MCP, lockfile state) run da config explain.

Use --audit when you need file-level detail suitable for debugging or for an AI
agent that must reason about the exact managed outputs.`,
		Example: statusExampleBlock(
			"  da status",
			"  da status --audit",
			"  da status --agent codex",
		),
		Args: statusNoArgsHint(deps, "Use `--agent` to filter by platform instead of passing a positional argument."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(audit, agentFilter, stdStatusConfigLoader{}, jsonOutput())
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
	branchOut, _ := execabs.Command("git", "-C", agentsHome, "rev-parse", "--abbrev-ref", "HEAD").Output()
	branch := strings.TrimSpace(string(branchOut))
	// Origin URL is read in-process via go-git rather than shelled out, and
	// rendered in the spec §5.2 canonical "<host>/<path>" form so the
	// status line stays consistent across SSH/HTTPS/.git-suffix transports
	// (PR #127 review). Falls back to the raw URL when canonicalization
	// produces "" (e.g. a non-URL local remote) so unusual configs still
	// surface something useful.
	remote := ""
	if raw, err := gitremote.ReadOriginURL(agentsHome); err == nil {
		if canon := gitremote.CanonicalRepoID(raw); canon != "" {
			remote = canon
		} else {
			remote = raw
		}
	}
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

// collectProjectTextBadges builds the same per-platform row shown in text-
// mode status by delegating to each platform's Badge implementation
// (P3 platform-driven diagnostics). The badge order — Cursor, Claude,
// Codex, OpenCode, Copilot — is the order returned by platform.All(); the
// previous status.go inline implementations are preserved at the
// internal/platform/<name>.go layer.
//
// The header truth source is config.json's enabled flags (AGENTS_HOME) ∧ the
// real platform install probe (platform.IsInstalled): a platform that is
// disabled in config or not installed on this machine renders as not-present
// (a dim "-") even if stray managed artifacts remain on disk, so the header
// can never contradict the per-section detail (e.g. "(no .opencode/)" /
// "(not linked)"). cfg may be nil in legacy call sites, in which case the
// raw badge value is preserved (no gating).
func collectProjectTextBadges(name, path, agentsHome string, cfg *config.Config) []platformBadge {
	enabledInstalled := installedEnabledPlatformIDs(cfg)
	out := make([]platformBadge, 0, 5)
	for _, p := range platform.All() {
		b, ok := p.(platform.StatusBadger)
		if !ok {
			continue
		}
		badge := b.Badge(name, path, agentsHome)
		entry := platformBadge{name: badge.Name, present: badge.Present, broken: badge.Broken}
		if cfg != nil && !enabledInstalled[p.ID()] {
			entry.present = false
			entry.broken = false
		}
		out = append(out, entry)
	}
	return out
}

// installedEnabledPlatformIDs returns the set of platform IDs that are both
// enabled in cfg (config.json AGENTS_HOME flags) and detected as installed on
// this machine, using the same probe as install/refresh
// (platform.InstalledEnabledPlatforms). A nil cfg yields an empty set.
func installedEnabledPlatformIDs(cfg *config.Config) map[string]bool {
	ids := make(map[string]bool)
	if cfg == nil {
		return ids
	}
	for _, p := range platform.InstalledEnabledPlatforms(cfg) {
		ids[p.ID()] = true
	}
	return ids
}

// CountClaudeRules is the exported entry point used by
// commands/seams_test.go (still in root before t11) and the legacy
// status_exports_test.go suite. Now delegates to the claude platform's
// CountLinks-style helper exposed for the test-only seam (claude.CountLinks
// covers the same rules-dir branch plus the managed-file extras).
// Returns the (ok, warn) tally for the .claude/rules directory only —
// matching the historical countClaudeRules signature so test fixtures keep
// the same assertion shape.
func CountClaudeRules(path string) (int, int) {
	return countClaudeRulesDir(filepath.Join(path, statusClaudeDir, "rules"))
}

// countClaudeRulesDir walks .claude/rules/ and reports ok/warn counts. Local
// shim that wraps platform.HasMultipleHardLinks so test-only consumers (the
// status_exports_test.go suite migrated by P3) continue exercising the
// claude rules counter end-to-end without re-pulling the per-link helper
// signatures.
func countClaudeRulesDir(rulesDir string) (int, int) {
	ok, warn := 0, 0
	entries, err := os.ReadDir(rulesDir)
	if err != nil {
		return ok, warn
	}
	for _, e := range entries {
		linkPath := filepath.Join(rulesDir, e.Name())
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
		if HasMultipleHardLinks(linkPath) {
			ok++
		}
	}
	return ok, warn
}

// RunStatus is the exported entry point used by the root commands/status.go
// shim during the t08→t11 window. After t11 splits seams_test.go into
// commands/lifecycle/seams_test.go, the only remaining caller is the shim
// itself; at that point RunStatus can be lowercased back to runStatus per
// SHAPE.md OD-2's reversal pattern.
func RunStatus(audit bool, agentFilter string, deps StatusConfigLoader, jsonOut bool) error {
	return runStatus(audit, agentFilter, deps, jsonOut)
}

// RunStatusDefault is the convenience entry point the root status shim uses
// from inside the cobra RunE closure it owns. Keeping the RunE closure in
// the commands package (instead of inheriting lifecycle.NewStatusCmd's
// closure) lets the globalflagcov static analyzer trace the Flags.JSON read
// without expanding its package load set to ./commands/lifecycle.
func RunStatusDefault(audit bool, agentFilter string, jsonOut bool) error {
	return runStatus(audit, agentFilter, stdStatusConfigLoader{}, jsonOut)
}

func runStatus(audit bool, agentFilter string, deps StatusConfigLoader, jsonOut bool) error {
	cfg, err := deps.LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	agentsHome := config.AgentsHome()

	if jsonOut {
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
// missing-directory bullet, the per-platform link-health badge row, and the
// audit block when requested. Per §7A.6 status is fleet/link-health only — it
// no longer renders manifest, lockfile, or last-refreshed config inspection
// (use `da config explain` for effective-config detail).
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

	printBadgeRow(collectProjectTextBadges(name, path, agentsHome, cfg))

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
			Name:       name,
			Path:       path,
			PathExists: pathExists(path),
			Platforms:  collectProjectPlatforms(name, path, agentsHome),
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

// collectUserConfigPlatforms builds the JSON-mode user-config platform list by
// delegating to each platform's UserBadge implementation (P4 platform-driven
// diagnostics). The order is platform.All() filtered to the UserConfigReporter
// implementors; copilot implements the interface but reports an empty/clean
// badge (its documented user-config layer is not yet wired by dot-agents — see
// PLATFORM_DIRS_DOCS), so appendPlatformIfPresent filters it out and the JSON
// snapshot stays byte-identical to the pre-P4 inline implementation. cursor,
// like claude/codex/opencode, reports its real managed ~/.cursor/hooks.json
// user-home surface. agentFilter scopes the result by p.ID() the same way the
// prior per-platform if-guards did.
func collectUserConfigPlatforms(agentFilter string) []statusJSONPlatform {
	homeDir, err := config.UserHomeDir()
	if err != nil {
		return nil
	}

	var out []statusJSONPlatform
	for _, p := range platform.All() {
		if agentFilter != "" && agentFilter != p.ID() {
			continue
		}
		r, ok := p.(platform.UserConfigReporter)
		if !ok {
			continue
		}
		badge := r.UserBadge(homeDir)
		out = appendPlatformIfPresent(out, badge.Name, platformBadge{
			name:    badge.Name,
			present: badge.Present,
			broken:  badge.Broken,
		})
	}
	return out
}

// collectProjectPlatforms builds the JSON-mode per-project platform list by
// delegating to each platform's Badge implementation (P3 platform-driven
// diagnostics). The list is byte-identical to the previous inline-counter
// implementation: same platform order, same labels, same Present/Broken
// semantics — every JSON snapshot test continues to pass without
// modification.
func collectProjectPlatforms(name, path, agentsHome string) []statusJSONPlatform {
	out := make([]statusJSONPlatform, 0, 5)
	for _, p := range platform.All() {
		b, ok := p.(platform.StatusBadger)
		if !ok {
			continue
		}
		badge := b.Badge(name, path, agentsHome)
		out = append(out, statusJSONPlatform{Name: badge.Name, Present: badge.Present, Broken: badge.Broken})
	}
	return out
}

// platformStatus remains in place as a collaborator of
// appendPlatformIfPresent. The user-config badge math now lives in each
// platform's UserBadge implementation (P4), so collectUserConfigPlatforms no
// longer calls countPlatformHealth — it is kept only because status_test.go's
// countPlatformHealth/platformStatus block exercises both helpers directly.
// Both stay package-private but live in this file rather than being re-derived
// ad hoc.
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

// PrintAudit is the exported entry point used by commands/doctor.go (still in
// root before t09) to render the per-platform audit block. After t09 lands
// doctor in this package, the only caller is intra-package and PrintAudit
// can be lowercased back to printAudit (SHAPE.md OD-2 reversal pattern).
func PrintAudit(name, path, agentsHome, agentFilter string, cfg *config.Config) {
	printAudit(name, path, agentsHome, agentFilter, cfg)
}

// printAudit renders the per-platform audit block, dispatching over the
// platform.AuditPrinter sister interface for every platform that survives the
// agentFilter. Per Phase 5 of platform-driven-diagnostics the per-platform-
// by-name helpers (printCursorAudit/printClaudeAudit/...) have moved into the
// internal/platform/<name>.go PrintAudit implementations; this loop is the
// single dispatch site, so adding a platform that implements AuditPrinter
// surfaces in `da status --audit` and `da doctor --verbose` automatically.
func printAudit(name, path, agentsHome, agentFilter string, cfg *config.Config) {
	fmt.Fprintln(os.Stdout)

	for _, p := range platform.Filter(platform.All(), agentFilter) {
		if ap, ok := p.(platform.AuditPrinter); ok {
			ap.PrintAudit(os.Stdout, name, path, agentsHome)
		}
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

// PrintSymlinkDirAudit is the exported entry point used by
// commands/seams_test.go (still in root before t11). Reversed when t11 splits
// the test file per cluster.
func PrintSymlinkDirAudit(dir, emptyLabel, nameFormat string) (int, int) {
	return printSymlinkDirAudit(dir, emptyLabel, nameFormat)
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
