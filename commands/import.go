package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/commands/internal/lifecycle"
	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/links"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/AGOrcha/dot-agents/internal/projectsync"
	"github.com/AGOrcha/dot-agents/internal/ui"
	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

type importCandidate struct {
	project    string
	sourceRoot string
	sourcePath string
	destRel    string
}

func (c importCandidate) destPath(agentsHome string) string {
	return filepath.Join(agentsHome, c.destRel)
}

type importResult struct {
	imported int
	skipped  int
}

type importOutput struct {
	destRel string
	content []byte
	// Origin is the emitting platform id for canonical hook imports (cursor, codex, claude, copilot, github).
	// When set and an on-disk conflict occurs, RFC §6 non-destructive alternate naming applies.
	Origin string
}

// importConflictReviewNote is the on-disk shape for ~/.agents/review-notes/import-conflicts/*.yaml (RFC §7).
type importConflictReviewNote struct {
	ID               string   `yaml:"id"`
	Status           string   `yaml:"status"`
	Kind             string   `yaml:"kind"`
	Bucket           string   `yaml:"bucket"`
	Scope            string   `yaml:"scope"`
	LogicalName      string   `yaml:"logical_name"`
	CanonicalTarget  string   `yaml:"canonical_target"`
	AlternateTarget  string   `yaml:"alternate_target"`
	Origin           string   `yaml:"origin"`
	Rationale        string   `yaml:"rationale,omitempty"`
	SuggestedActions []string `yaml:"suggested_actions,omitempty"`
	CreatedAt        string   `yaml:"created_at"`
}

type importedCopilotHooksFile struct {
	Hooks map[string][]importedCopilotHookAction `json:"hooks"`
}

type importedCopilotHookAction struct {
	Type       string `json:"type"`
	Bash       string `json:"bash"`
	TimeoutSec int    `json:"timeoutSec,omitempty"`
}

type importedHookManifest struct {
	Name      string                    `yaml:"name"`
	When      string                    `yaml:"when"`
	Match     importedHookManifestMatch `yaml:"match,omitempty"`
	Run       importedHookManifestRun   `yaml:"run"`
	EnabledOn []string                  `yaml:"enabled_on,omitempty"`
}

type importedHookManifestMatch struct {
	Tools      []string `yaml:"tools,omitempty"`
	Expression string   `yaml:"expression,omitempty"`
}

type importedHookManifestRun struct {
	Command   string `yaml:"command"`
	TimeoutMS int    `yaml:"timeout_ms,omitempty"`
}

type importedClaudeHooksFile struct {
	Hooks map[string][]importedClaudeHookEntry `json:"hooks"`
}

type importedClaudeHookEntry struct {
	Matcher string                     `json:"matcher"`
	Hooks   []importedClaudeHookAction `json:"hooks"`
}

type importedClaudeHookAction struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type importedCursorHooksFile struct {
	Hooks map[string][]importedCursorHookEntry `json:"hooks"`
}

type importedCursorHookEntry struct {
	Command string `json:"command"`
	Matcher string `json:"matcher,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

type importedHookSpec struct {
	nameHint  string
	when      string
	matcher   string
	command   string
	timeoutMS int
	enabledOn []string
	platform  string
}

const (
	importScopeProject = "project"
	importScopeGlobal  = "global"
	importScopeAll     = "all"
	importFailedFmt    = "Failed to import %s: %v"

	// The relX / agentsHooksPrefix constants below alias the canonical
	// definitions in commands/lifecycle/resource_map.go (lifted in
	// root-command-decomposition t02b). Keeping local aliases lets
	// import.go's interior references stay unchanged until the parent
	// command moves into commands/lifecycle/ in t06.
	relClaudeSettingsJSON    = lifecycle.RelClaudeSettingsJSON
	relCursorSettingsJSON    = lifecycle.RelCursorSettingsJSON
	relCursorMCPJSON         = lifecycle.RelCursorMCPJSON
	relCursorHooksJSON       = lifecycle.RelCursorHooksJSON
	relCursorIgnore          = lifecycle.RelCursorIgnore
	relCursorIndexingIgnore  = lifecycle.RelCursorIndexingIgnore
	relClaudeSettingsLocal   = lifecycle.RelClaudeSettingsLocal
	relMCPJSON               = lifecycle.RelMCPJSON
	relVSCodeMCPJSON         = lifecycle.RelVSCodeMCPJSON
	relOpenCodeJSON          = lifecycle.RelOpenCodeJSON
	relAgentsMD              = lifecycle.RelAgentsMD
	relCodexInstructionsMD   = lifecycle.RelCodexInstructionsMD
	relCodexRulesMD          = lifecycle.RelCodexRulesMD
	relCodexConfigTOML       = lifecycle.RelCodexConfigTOML
	relCodexHooksJSON        = lifecycle.RelCodexHooksJSON
	relCopilotInstructionsMD = lifecycle.RelCopilotInstructionsMD
	relCursorCommandsDir     = lifecycle.RelCursorCommandsDir
	relClaudeCommandsDir     = lifecycle.RelClaudeCommandsDir
	relOpenCodeCommandsDir   = lifecycle.RelOpenCodeCommandsDir
	relClaudeOutputStylesDir = lifecycle.RelClaudeOutputStylesDir
	relOpenCodeModesDir      = lifecycle.RelOpenCodeModesDir
	relOpenCodeThemesDir     = lifecycle.RelOpenCodeThemesDir
	relGitHubPromptsDir      = lifecycle.RelGitHubPromptsDir
	relCursorRulesDir        = lifecycle.RelCursorRulesDir
	relAgentsSkillsDir       = lifecycle.RelAgentsSkillsDir
	relClaudeSkillsDir       = lifecycle.RelClaudeSkillsDir
	relGitHubAgentsDir       = lifecycle.RelGitHubAgentsDir
	relCodexAgentsDir        = lifecycle.RelCodexAgentsDir
	relOpenCodeAgentsDir     = lifecycle.RelOpenCodeAgentsDir
	relGitHubHooksDir        = lifecycle.RelGitHubHooksDir
	relJSONSuffix            = lifecycle.RelJSONSuffix
	agentsHooksPrefix        = lifecycle.AgentsHooksPrefix

	// import-only constants that the resource-map lift did not pull
	// out (still file-scoped to import.go's plugin / marketplace flow).
	relCopilotPluginManifest = "plugin.json"
	relGitHubPluginManifest  = ".github/plugin/plugin.json"
	relGitHubPluginDir       = ".github/plugin/"
	relCopilotPluginMarket   = ".github/plugin/marketplace.json"
	relCodexPluginMarket     = ".agents/plugins/marketplace.json"
	relOpenCodePluginsDir    = ".opencode/plugins/"
	relClaudePluginDir       = ".claude-plugin/"
	relCursorPluginDir       = ".cursor-plugin/"
	relCodexPluginDir        = ".codex-plugin/"
	relClaudeREADME          = ".claude/CLAUDE.md"
	relHookManifestYAML      = "HOOK.yaml"
)

var projectImportSingles = []string{
	relCursorSettingsJSON,
	relCursorMCPJSON,
	relCursorHooksJSON,
	relCursorIgnore,
	relCursorIndexingIgnore,
	relClaudeSettingsLocal,
	relMCPJSON,
	relVSCodeMCPJSON,
	relOpenCodeJSON,
	relAgentsMD,
	relCodexInstructionsMD,
	relCodexRulesMD,
	relCodexConfigTOML,
	relCodexHooksJSON,
	relCopilotInstructionsMD,
	relCopilotPluginManifest,
	relGitHubPluginManifest,
	relCopilotPluginMarket,
	relCodexPluginMarket,
}

var projectImportWalkDirs = []string{
	"commands",
	"output-styles",
	"ignore",
	"modes",
	"plugins",
	"themes",
	"prompts",
	".cursor/rules",
	".cursor/commands",
	".agents/skills",
	".claude/skills",
	".claude/commands",
	".claude/output-styles",
	".github/agents",
	".codex/agents",
	".opencode/commands",
	".opencode/agent",
	".opencode/modes",
	".opencode/themes",
	".opencode/plugins",
	".claude-plugin",
	".cursor-plugin",
	".codex-plugin",
	".github/plugin",
	".github/hooks",
	".github/prompts",
}

var globalImportSingles = []string{
	relClaudeSettingsJSON,
	relCursorSettingsJSON,
	relCursorMCPJSON,
	relCursorHooksJSON,
	relCursorIgnore,
	relCursorIndexingIgnore,
	relClaudeREADME,
	relCodexConfigTOML,
	relCodexHooksJSON,
}

var globalImportWalkDirs = []string{
	".cursor/commands",
	".claude/commands",
	".claude/output-styles",
	".opencode/commands",
	".opencode/modes",
	".opencode/themes",
	".github/prompts",
}

// importDeps is the multi-method collaborator runImport and its helpers
// need (interface-DI per docs/TEST_SEAMS.md). File-scoped — do not share
// with other commands files. Multi-method role name without -er suffix
// per the convention.
type importDeps interface {
	MkdirAll(path string, perm os.FileMode) error
	WriteFile(name string, data []byte, perm os.FileMode) error
	LoadConfig() (*config.Config, error)
}

// stdImportDeps is the production importDeps backed by os and config.
type stdImportDeps struct{}

func (stdImportDeps) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}
func (stdImportDeps) WriteFile(name string, data []byte, perm os.FileMode) error {
	return os.WriteFile(name, data, perm)
}
func (stdImportDeps) LoadConfig() (*config.Config, error) { return config.Load() }

func NewImportCmd() *cobra.Command {
	scope := "all"
	cmd := &cobra.Command{
		Use:   "import [project]",
		Short: "Import configs from project/global scope into ~/.agents/",
		Long: `Scans project-managed files and user-level AI configuration, then copies
those artifacts into the canonical ~/.agents/ layout so future refresh and install
operations can treat them as shared source of truth.

Hook imports are written as canonical bundles under ~/.agents/hooks/<scope>/<name>/HOOK.yaml
when the source can be normalized (see da hooks list / hooks show).

This is most useful when adopting dot-agents in an existing setup or when you want
to normalize hand-edited config back into the managed store.`,
		Example: ExampleBlock(
			"  da import",
			"  da import billing-api --scope project",
			"  da import --scope global --dry-run",
		),
		Args: MaximumNArgsWithHints(1, "Optionally pass one managed project name to restrict project-scope imports."),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectFilter := ""
			if len(args) > 0 {
				projectFilter = args[0]
			}
			return runImport(projectFilter, scope, stdImportDeps{})
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "all", "Import scope: project, global, or all")
	return cmd
}

// runImportFromRefresh runs the import pipeline as part of da refresh.
// Policy: sources are the authoritative truth (uv-style auto-sync). Non-hook
// managed files are auto-replaced when the source has newer content; the
// idempotency fix (ii-import-idempotent) ensures unchanged sources are no-ops
// so only genuine updates reach this path. Backup sidecars are written before
// any replacement. This policy is locked by TestRunImportFromRefresh_Policy.
func runImportFromRefresh(projectFilter, scope string, deps importDeps) error {
	oldYes := Flags.Yes
	Flags.Yes = true
	defer func() {
		Flags.Yes = oldYes
	}()
	return runImportInternal(projectFilter, scope, true, deps)
}

func runImport(projectFilter, scope string, deps importDeps) error {
	return runImportInternal(projectFilter, scope, false, deps)
}

func runImportInternal(projectFilter, scope string, skipRelink bool, deps importDeps) error {
	scope, err := normalizeImportScope(scope)
	if err != nil {
		return err
	}

	cfg, err := deps.LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	agentsHome := config.AgentsHome()

	ui.Header("da import")

	candidates, projectSet, err := collectImportCandidates(cfg, projectFilter, scope)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		ui.Info("No import candidates found.")
		return nil
	}

	sortImportCandidates(candidates)

	timestamp := time.Now().Format("20060102-150405")
	result := foldImportCandidates(candidates, agentsHome, timestamp, deps)

	if !skipRelink && scope != importScopeGlobal {
		relinkImportedProjects(cfg, projectSet)
	}

	ui.Success(fmt.Sprintf("Import complete: %d imported, %d skipped.", result.imported, result.skipped))
	return nil
}

// foldImportCandidates runs the import pipeline for each candidate in stable order.
func foldImportCandidates(candidates []importCandidate, agentsHome, timestamp string, deps importDeps) importResult {
	result := importResult{}
	for _, c := range candidates {
		delta := processImportCandidate(c, agentsHome, timestamp, deps)
		result.imported += delta.imported
		result.skipped += delta.skipped
	}
	return result
}

func normalizeImportScope(scope string) (string, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	switch scope {
	case importScopeProject, importScopeGlobal, importScopeAll:
		return scope, nil
	default:
		return "", UsageError(
			fmt.Sprintf("invalid scope %q", scope),
			"Supported values are `project`, `global`, and `all`.",
		)
	}
}

func collectImportCandidates(cfg *config.Config, projectFilter, scope string) ([]importCandidate, map[string]bool, error) {
	candidates := []importCandidate{}
	projectSet := map[string]bool{}
	if scope == importScopeProject || scope == importScopeAll {
		projectCandidates, err := scanProjectImportCandidates(cfg, projectFilter)
		if err != nil {
			return nil, nil, err
		}
		candidates = append(candidates, projectCandidates...)
		for _, c := range projectCandidates {
			projectSet[c.project] = true
		}
	}
	if scope == importScopeGlobal || scope == importScopeAll {
		candidates = append(candidates, scanGlobalImportCandidates()...)
	}
	return candidates, projectSet, nil
}

func sortImportCandidates(candidates []importCandidate) {
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].project == candidates[j].project {
			return candidates[i].sourcePath < candidates[j].sourcePath
		}
		return candidates[i].project < candidates[j].project
	})
}

func processImportCandidate(c importCandidate, agentsHome, timestamp string, deps importDeps) importResult {
	if isManagedImportSource(c, agentsHome) {
		return importResult{}
	}

	rel, err := filepath.Rel(c.sourceRoot, c.sourcePath)
	if err == nil && supportsCanonicalImportPath(filepath.ToSlash(rel)) {
		srcInfo, skipResult, ok := statImportSourceCandidate(c)
		if !ok {
			return skipResult
		}
		if result, ok := processCanonicalHookBundleImport(c, agentsHome, timestamp, srcInfo, deps); ok {
			return result
		}
		return importResult{}
	}

	srcInfo, skipResult, ok := statImportSourceCandidate(c)
	if !ok {
		return skipResult
	}

	if result, ok := processCanonicalHookBundleImport(c, agentsHome, timestamp, srcInfo, deps); ok {
		return result
	}

	dest := c.destPath(agentsHome)
	destInfo, err := os.Stat(dest)
	if os.IsNotExist(err) {
		return importMissingCandidate(c, dest, timestamp)
	}
	if err != nil {
		ui.Bullet("warn", fmt.Sprintf("Failed to inspect %s: %v", c.destRel, err))
		return importResult{skipped: 1}
	}

	different, err := filesDifferent(c.sourcePath, dest)
	if err != nil {
		ui.Bullet("warn", fmt.Sprintf("Failed to compare %s and %s: %v", config.DisplayPath(c.sourcePath), c.destRel, err))
		return importResult{skipped: 1}
	}
	if !different {
		return importResult{}
	}

	return replaceImportCandidate(c, agentsHome, dest, timestamp, srcInfo, destInfo)
}

// statImportSourceCandidate stats the candidate's source path, distinguishing
// a real Stat failure (permission denied, a directory made unreadable
// mid-walk, a broken mount, ...) from legitimate absence. The candidate list
// was built by an earlier directory scan, so by the time processing reaches
// here the source may simply be gone (raced away, a transient scan
// artifact) — that stays a silent no-op, mirroring the destination-side
// os.IsNotExist branch in processImportCandidate. A real error is a
// different signal entirely and must not vanish the same way: it is warned
// and counted as a skip, mirroring the destination Stat's err != nil branch
// just below.
func statImportSourceCandidate(c importCandidate) (os.FileInfo, importResult, bool) {
	srcInfo, err := os.Stat(c.sourcePath)
	if os.IsNotExist(err) {
		return nil, importResult{}, false
	}
	if err != nil {
		ui.Bullet("warn", fmt.Sprintf("Failed to inspect %s: %v", config.DisplayPath(c.sourcePath), err))
		return nil, importResult{skipped: 1}, false
	}
	if srcInfo.IsDir() {
		return nil, importResult{}, false
	}
	return srcInfo, importResult{}, true
}

func isManagedImportSource(c importCandidate, agentsHome string) bool {
	if isManagedSymlink(c.sourcePath, agentsHome) {
		return true
	}

	rel, err := filepath.Rel(c.sourceRoot, c.sourcePath)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)

	if isManagedCodexAgentTomlImportSource(rel, c.sourcePath) {
		return true
	}

	if c.project == "global" {
		destRel := mapGlobalRelToDest(rel)
		if destRel == "" {
			return false
		}
		linked, err := links.AreHardlinked(c.sourcePath, filepath.Join(agentsHome, destRel))
		return err == nil && linked
	}

	return isManagedProjectOutput(c.project, c.sourceRoot, c.sourcePath, agentsHome)
}

// isManagedCodexAgentTomlImportSource reports whether rel/sourcePath is a
// dot-agents managed rendered codex agent `.toml` under `.codex/agents/`
// (t2c). The project-scope scan already walks `.codex/agents/`
// (projectImportWalkDirs) as an ordinary import candidate; without this
// check every dot-agents render would be re-"imported" into
// ~/.agents/agents/<project>/ as archival noise on every `da import`/
// `da refresh` — a marked render is dot-agents' own output, never foreign
// content to import. A genuinely unmarked (foreign or pre-marker-upgrade)
// `.codex/agents/*.toml` is unaffected and still flows through the normal
// candidate path below: platform.writeCodexAgentTomlFile resolves any
// render-time collision (byte-identical adopt, or diverged
// preserve-plus-review-note) at projection time, and a preserved sibling
// this check does not recognize (it lacks the marker) is then swept up by
// that SAME normal candidate path on the next import/refresh pass — the
// existing import-to-scope machinery, not a parallel one.
func isManagedCodexAgentTomlImportSource(rel, sourcePath string) bool {
	if !strings.HasPrefix(rel, relCodexAgentsDir) || !strings.HasSuffix(rel, ".toml") {
		return false
	}
	managed, err := platform.IsManagedCodexAgentTomlFile(sourcePath)
	return err == nil && managed
}

func importMissingCandidate(c importCandidate, dest, timestamp string) importResult {
	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("Import %s -> %s", config.DisplayPath(c.sourcePath), c.destRel))
		return importResult{imported: 1}
	}

	mirrorBackup(c.project, c.sourceRoot, c.sourcePath, timestamp)
	if err := projectsync.CopyFile(c.sourcePath, dest); err != nil {
		ui.Bullet("warn", fmt.Sprintf(importFailedFmt, config.DisplayPath(c.sourcePath), err))
		return importResult{skipped: 1}
	}

	ui.Bullet("ok", fmt.Sprintf("Imported %s -> %s", config.DisplayPath(c.sourcePath), c.destRel))
	return importResult{imported: 1}
}

func replaceImportCandidate(c importCandidate, agentsHome, dest, timestamp string, srcInfo, destInfo os.FileInfo) importResult {
	if !confirmImportReplace(importReplaceMessage(c, srcInfo, destInfo)) {
		return importResult{skipped: 1}
	}
	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("Replace %s from %s", c.destRel, config.DisplayPath(c.sourcePath)))
		return importResult{imported: 1}
	}

	mirrorBackup(c.project, agentsHome, dest, timestamp)
	mirrorBackup(c.project, c.sourceRoot, c.sourcePath, timestamp)
	if err := projectsync.CopyFile(c.sourcePath, dest); err != nil {
		ui.Bullet("warn", fmt.Sprintf(importFailedFmt, config.DisplayPath(c.sourcePath), err))
		return importResult{skipped: 1}
	}

	ui.Bullet("ok", fmt.Sprintf("Updated %s from %s", c.destRel, config.DisplayPath(c.sourcePath)))
	return importResult{imported: 1}
}

// confirmImportReplace decides whether an existing managed file may be
// overwritten by a newer source. Under --yes it auto-confirms; in a real
// terminal it prompts. Otherwise (CI, `da refresh`, piped/redirected stdin)
// it never blocks on a prompt and defaults to the safe, non-destructive
// action: skip the replacement and preserve the existing file.
func confirmImportReplace(message string) bool {
	note := fmt.Sprintf("Non-interactive: skipped (preserved existing). %s", message)
	return ui.ConfirmInteractive(message, note, Flags.Yes)
}

func importReplaceMessage(c importCandidate, srcInfo, destInfo os.FileInfo) string {
	sourceNewer := srcInfo.ModTime().After(destInfo.ModTime())
	newer := map[bool]string{true: "source", false: "destination"}[sourceNewer]
	return fmt.Sprintf("Import newer=%s into %s? (src=%s, dest=%s)",
		newer,
		c.destRel,
		srcInfo.ModTime().Format(time.RFC3339),
		destInfo.ModTime().Format(time.RFC3339),
	)
}

func scanProjectImportCandidates(cfg *config.Config, projectFilter string) ([]importCandidate, error) {
	projects := cfg.ListProjects()
	if projectFilter != "" {
		path := cfg.GetProjectPath(projectFilter)
		if path == "" {
			return nil, ErrorWithHints(
				fmt.Sprintf("project not found: %s", projectFilter),
				"Run `da status` to list the managed project names.",
			)
		}
		projects = []string{projectFilter}
	}

	candidates := []importCandidate{}
	for _, project := range projects {
		projectPath := cfg.GetProjectPath(project)
		if projectPath == "" {
			continue
		}
		found := gatherProjectCandidates(project, projectPath)
		candidates = append(candidates, found...)
	}
	return candidates, nil
}

func gatherProjectCandidates(project, projectPath string) []importCandidate {
	out := []importCandidate{}
	for _, rel := range projectImportSingles {
		if candidate, ok := projectImportCandidate(project, projectPath, rel); ok {
			out = append(out, candidate)
		}
	}
	for _, relDir := range projectImportWalkDirs {
		out = append(out, walkProjectImportCandidates(project, projectPath, relDir)...)
	}
	out = append(out, gatherDirectPackagePluginCandidates(project, projectPath)...)
	return out
}

func projectImportCandidate(project, projectPath, rel string) (importCandidate, bool) {
	src := filepath.Join(projectPath, rel)
	if isBackupArtifact(filepath.Base(rel)) {
		return importCandidate{}, false
	}
	if _, err := os.Lstat(src); err != nil {
		return importCandidate{}, false
	}
	destRel := mapResourceRelToDest(project, rel)
	if destRel == "" && !supportsCanonicalImportPath(rel) {
		return importCandidate{}, false
	}
	return importCandidate{
		project:    project,
		sourceRoot: projectPath,
		sourcePath: src,
		destRel:    destRel,
	}, true
}

func walkProjectImportCandidates(project, projectPath, relDir string) []importCandidate {
	root := filepath.Join(projectPath, relDir)
	out := []importCandidate{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		candidate, ok := walkedImportCandidate(project, projectPath, path, d, err)
		if ok {
			out = append(out, candidate)
		}
		return nil
	})
	return out
}

func walkedImportCandidate(project, projectPath, path string, d os.DirEntry, err error) (importCandidate, bool) {
	if err != nil || d.IsDir() || isBackupArtifact(d.Name()) {
		return importCandidate{}, false
	}
	rel, err := filepath.Rel(projectPath, path)
	if err != nil {
		return importCandidate{}, false
	}
	rel = filepath.ToSlash(rel)
	destRel := mapResourceRelToDest(project, rel)
	if destRel == "" && !supportsCanonicalImportPath(rel) {
		return importCandidate{}, false
	}
	return importCandidate{
		project:    project,
		sourceRoot: projectPath,
		sourcePath: path,
		destRel:    destRel,
	}, true
}

func scanGlobalImportCandidates() []importCandidate {
	home, err := config.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []importCandidate
	for _, rel := range globalImportSingles {
		src := filepath.Join(home, rel)
		if _, err := os.Lstat(src); err != nil {
			continue
		}
		destRel := mapGlobalRelToDest(filepath.ToSlash(rel))
		if destRel == "" {
			continue
		}
		out = append(out, importCandidate{
			project:    "global",
			sourceRoot: home,
			sourcePath: src,
			destRel:    destRel,
		})
	}
	for _, relDir := range globalImportWalkDirs {
		out = append(out, walkGlobalImportCandidates(home, relDir)...)
	}
	return out
}

func walkGlobalImportCandidates(sourceRoot, relDir string) []importCandidate {
	root := filepath.Join(sourceRoot, relDir)
	out := []importCandidate{}
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		candidate, ok := walkedImportCandidate("global", sourceRoot, path, d, err)
		if ok {
			out = append(out, candidate)
		}
		return nil
	})
	return out
}

func mapGlobalRelToDest(rel string) string {
	switch rel {
	case relClaudeSettingsJSON:
		return "settings/global/claude-code.json"
	case relCursorSettingsJSON:
		return "settings/global/cursor.json"
	case relCursorMCPJSON:
		return "mcp/global/mcp.json"
	case relCursorHooksJSON:
		return "hooks/global/cursor.json"
	case relCursorIgnore:
		return "settings/global/cursorignore"
	case relClaudeREADME:
		return "rules/global/agents.md"
	case relCodexConfigTOML:
		return "settings/global/codex.toml"
	case relCodexHooksJSON:
		return "hooks/global/codex.json"
	case relCursorIndexingIgnore:
		return platform.CanonicalBucketScopePath(platform.CanonicalBucketIgnore, "global", "cursorindexingignore")
	default:
		return ""
	}
}

func processCanonicalHookBundleImport(c importCandidate, agentsHome, timestamp string, srcInfo os.FileInfo, deps importDeps) (importResult, bool) {
	outputs, ok, err := canonicalImportOutputs(c, agentsHome)
	if !ok {
		return importResult{}, false
	}
	if err != nil {
		ui.Bullet("warn", fmt.Sprintf("Failed to canonicalize %s: %v", config.DisplayPath(c.sourcePath), err))
		return importResult{skipped: 1}, true
	}

	total := importResult{}
	for _, output := range outputs {
		delta := processImportOutput(c, output, agentsHome, timestamp, srcInfo, deps)
		total.imported += delta.imported
		total.skipped += delta.skipped
	}
	return total, true
}

func processImportOutput(c importCandidate, output importOutput, agentsHome, timestamp string, srcInfo os.FileInfo, deps importDeps) importResult {
	resolved := c
	resolved.destRel = output.destRel
	dest := resolved.destPath(agentsHome)

	destInfo, err := os.Stat(dest)
	if os.IsNotExist(err) {
		return importMissingContentCandidate(resolved, dest, output.content, timestamp, deps)
	}
	if err != nil {
		ui.Bullet("warn", fmt.Sprintf("Failed to inspect %s: %v", resolved.destRel, err))
		return importResult{skipped: 1}
	}

	existing, err := os.ReadFile(dest)
	if err != nil {
		ui.Bullet("warn", fmt.Sprintf("Failed to compare %s and %s: %v", config.DisplayPath(resolved.sourcePath), resolved.destRel, err))
		return importResult{skipped: 1}
	}
	if string(existing) == string(output.content) {
		return importResult{}
	}

	if output.Origin != "" {
		// Idempotency: a prior import of this same source already preserved
		// this exact canonical content under one of the origin-prefixed
		// alternate slots. Re-importing must recognize that output and be a
		// no-op rather than stacking another alternate (cursor-demo ->
		// cursor-demo-2 -> ...), which bloats ~/.agents/hooks/ on every run.
		if importConflictAlreadyPreserved(agentsHome, output) {
			return importResult{}
		}
		if altRel, ok := importConflictFirstFreeAlternateDestRel(agentsHome, output.destRel, output.Origin); ok {
			altDest := filepath.Join(agentsHome, altRel)
			if _, err := os.Stat(altDest); os.IsNotExist(err) {
				resolved := c
				resolved.destRel = altRel
				return importPreservedConflictCandidate(resolved, agentsHome, output, altRel, altDest, timestamp, deps)
			}
		}
	}

	return replaceImportContentCandidate(replaceImportArgs{
		candidate:  resolved,
		agentsHome: agentsHome,
		dest:       dest,
		content:    output.content,
		timestamp:  timestamp,
		srcInfo:    srcInfo,
		destInfo:   destInfo,
	}, deps)
}

func importMissingContentCandidate(c importCandidate, dest string, content []byte, timestamp string, deps importDeps) importResult {
	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("Import %s -> %s", config.DisplayPath(c.sourcePath), c.destRel))
		return importResult{imported: 1}
	}

	mirrorBackup(c.project, c.sourceRoot, c.sourcePath, timestamp)
	_ = deps.MkdirAll(filepath.Dir(dest), 0755)
	if err := deps.WriteFile(dest, content, 0644); err != nil {
		ui.Bullet("warn", fmt.Sprintf(importFailedFmt, config.DisplayPath(c.sourcePath), err))
		return importResult{skipped: 1}
	}

	ui.Bullet("ok", fmt.Sprintf("Imported %s -> %s", config.DisplayPath(c.sourcePath), c.destRel))
	return importResult{imported: 1}
}

// replaceImportArgs bundles the inputs to replaceImportContentCandidate so
// the call site stays under Sonar's S107 7-parameter cap. The struct holds
// the exact same values the prior positional parameter list carried —
// candidate, agentsHome, destination path + content, backup timestamp, and
// the pre-resolved source/destination FileInfos. The injected importDeps
// stays a separate parameter so test fakes substitute without populating
// every other field.
type replaceImportArgs struct {
	candidate  importCandidate
	agentsHome string
	dest       string
	content    []byte
	timestamp  string
	srcInfo    os.FileInfo
	destInfo   os.FileInfo
}

func replaceImportContentCandidate(args replaceImportArgs, deps importDeps) importResult {
	c := args.candidate
	if !confirmImportReplace(importReplaceMessage(c, args.srcInfo, args.destInfo)) {
		return importResult{skipped: 1}
	}
	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("Replace %s from %s", c.destRel, config.DisplayPath(c.sourcePath)))
		return importResult{imported: 1}
	}

	mirrorBackup(c.project, args.agentsHome, args.dest, args.timestamp)
	mirrorBackup(c.project, c.sourceRoot, c.sourcePath, args.timestamp)
	if err := deps.WriteFile(args.dest, args.content, 0644); err != nil {
		ui.Bullet("warn", fmt.Sprintf(importFailedFmt, config.DisplayPath(c.sourcePath), err))
		return importResult{skipped: 1}
	}

	ui.Bullet("ok", fmt.Sprintf("Updated %s from %s", c.destRel, config.DisplayPath(c.sourcePath)))
	return importResult{imported: 1}
}

func importPreservedConflictCandidate(c importCandidate, agentsHome string, output importOutput, altRel, altDest string, timestamp string, deps importDeps) importResult {
	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("Import conflict: preserve %s; write alternate %s", output.destRel, altRel))
		return importResult{imported: 1}
	}

	if err := writeImportConflictReviewNote(agentsHome, c.project, output.destRel, altRel, output.Origin, deps); err != nil {
		ui.Bullet("warn", fmt.Sprintf("could not write import conflict review note: %v", err))
	}

	mirrorBackup(c.project, c.sourceRoot, c.sourcePath, timestamp)
	if err := deps.MkdirAll(filepath.Dir(altDest), 0755); err != nil {
		ui.Bullet("warn", fmt.Sprintf("Failed to create %s: %v", altRel, err))
		return importResult{skipped: 1}
	}
	if err := deps.WriteFile(altDest, output.content, 0644); err != nil {
		ui.Bullet("warn", fmt.Sprintf(importFailedFmt, config.DisplayPath(c.sourcePath), err))
		return importResult{skipped: 1}
	}

	ui.Bullet("ok", fmt.Sprintf("Preserved %s; imported alternate -> %s", output.destRel, altRel))
	return importResult{imported: 1}
}

// importConflictBaseName derives the origin-prefixed base bundle name
// (sanitized origin + "-" + sanitized logical, with "import"/"hook"
// fallbacks for empty parts) shared by the free-slot finder and the
// already-preserved idempotency check. Centralizing it keeps the
// "cannot disagree" invariant structural: both paths walk the same
// base / base-2 / base-3 … sequence.
func importConflictBaseName(origin, logical string) string {
	o := sanitizeHookNamePart(origin)
	if o == "" {
		o = "import"
	}
	log := sanitizeHookNamePart(logical)
	if log == "" {
		log = "hook"
	}
	return o + "-" + log
}

// importConflictStableBundleName picks the first free logical name using origin-prefixed base, then -2, -3, … suffixes.
func importConflictStableBundleName(logical, origin string, taken func(name string) bool) string {
	base := importConflictBaseName(origin, logical)
	if !taken(base) {
		return base
	}
	n := 2
	for {
		cand := fmt.Sprintf("%s-%d", base, n)
		if !taken(cand) {
			return cand
		}
		n++
	}
}

// importConflictAlternateShape describes how an origin-prefixed alternate
// path is built for a primary hooks dest, so the "next free slot" and the
// idempotency "already preserved" checks share one naming scheme.
type importConflictAlternateShape struct {
	scope   string
	logical string
	// destRelFor turns a logical bundle name into its full hooks-relative path.
	destRelFor func(name string) string
}

// importConflictAlternateShapeFor decodes a primary hooks dest into the
// shared alternate-naming shape, or reports false if the path is not a
// recognized hook bundle / json hook file.
func importConflictAlternateShapeFor(primaryDestRel string) (importConflictAlternateShape, bool) {
	primaryDestRel = filepath.ToSlash(primaryDestRel)
	if !strings.HasPrefix(primaryDestRel, agentsHooksPrefix) {
		return importConflictAlternateShape{}, false
	}
	trim := strings.TrimPrefix(primaryDestRel, agentsHooksPrefix)
	parts := strings.Split(trim, "/")
	if len(parts) == 3 && parts[2] == relHookManifestYAML {
		scope, logical := parts[0], parts[1]
		return importConflictAlternateShape{
			scope:   scope,
			logical: logical,
			destRelFor: func(name string) string {
				return agentsHooksPrefix + scope + "/" + name + "/" + relHookManifestYAML
			},
		}, true
	}
	if len(parts) == 2 && strings.HasSuffix(parts[1], ".json") {
		scope := parts[0]
		stem := strings.TrimSuffix(parts[1], ".json")
		return importConflictAlternateShape{
			scope:   scope,
			logical: stem,
			destRelFor: func(name string) string {
				return agentsHooksPrefix + scope + "/" + name + ".json"
			},
		}, true
	}
	return importConflictAlternateShape{}, false
}

// importConflictFirstFreeAlternateDestRel returns a hooks-relative path under agentsHome that does not yet exist.
func importConflictFirstFreeAlternateDestRel(agentsHome, primaryDestRel, origin string) (string, bool) {
	shape, ok := importConflictAlternateShapeFor(primaryDestRel)
	if !ok {
		return "", false
	}
	taken := func(name string) bool {
		_, err := os.Stat(filepath.Join(agentsHome, shape.destRelFor(name)))
		return err == nil
	}
	name := importConflictStableBundleName(shape.logical, origin, taken)
	return shape.destRelFor(name), true
}

// importConflictAlreadyPreserved reports whether a prior import already wrote
// this exact canonical content under one of the origin-prefixed alternate
// slots for output's primary dest. It walks the same name sequence
// importConflictStableBundleName generates (origin-logical, origin-logical-2,
// …) over existing files only, so re-importing an unchanged source whose
// canonical form conflicts with the primary is a no-op instead of minting
// yet another -N alternate.
func importConflictAlreadyPreserved(agentsHome string, output importOutput) bool {
	shape, ok := importConflictAlternateShapeFor(output.destRel)
	if !ok {
		return false
	}
	base := importConflictBaseName(output.Origin, shape.logical)
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		altPath := filepath.Join(agentsHome, shape.destRelFor(name))
		existing, err := os.ReadFile(altPath)
		if os.IsNotExist(err) {
			// First gap in the sequence ends the search: stable naming never
			// skips a slot, so no later alternate can exist past this point.
			return false
		}
		if err != nil {
			return false
		}
		if string(existing) == string(output.content) {
			return true
		}
	}
}

func logicalNameFromHooksDest(destRel string) string {
	destRel = filepath.ToSlash(destRel)
	trim := strings.TrimPrefix(destRel, agentsHooksPrefix)
	parts := strings.Split(trim, "/")
	if len(parts) == 3 && parts[2] == relHookManifestYAML {
		return parts[1]
	}
	if len(parts) == 2 && strings.HasSuffix(parts[1], ".json") {
		return strings.TrimSuffix(parts[1], ".json")
	}
	return ""
}

func writeImportConflictReviewNote(agentsHome, project, primaryRel, alternateRel, origin string, deps importDeps) error {
	if Flags.DryRun {
		return nil
	}
	dir := filepath.Join(agentsHome, "review-notes", "import-conflicts")
	if err := deps.MkdirAll(dir, 0755); err != nil {
		return err
	}
	id := fmt.Sprintf("ic-%d", time.Now().UnixNano())
	logical := logicalNameFromHooksDest(primaryRel)
	note := importConflictReviewNote{
		ID:              id,
		Status:          "pending",
		Kind:            "duplicate_name",
		Bucket:          "hooks",
		Scope:           project,
		LogicalName:     logical,
		CanonicalTarget: primaryRel,
		AlternateTarget: alternateRel,
		Origin:          origin,
		Rationale:       "Import produced different canonical hook content than the existing managed file; alternate path preserves both variants per resource-intent-centralization RFC §6.",
		SuggestedActions: []string{
			"Compare canonical_target vs alternate_target and reconcile hook bundles manually if needed.",
			"Delete the alternate after merging if it is redundant.",
		},
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	data, err := yaml.Marshal(&note)
	if err != nil {
		return err
	}
	fn := filepath.Join(dir, id+".yaml")
	return deps.WriteFile(fn, append(data, '\n'), 0644)
}

// canonicalImportOutputs converts one import candidate into the canonical
// artifacts it should produce. agentsHome is the home whose existing
// canonical hook bundles establish provenance for hook-shaped sources; pass
// "" only when no home is in play (no bundle can then claim ownership and
// every parsed entry is treated as hand-authored).
func canonicalImportOutputs(c importCandidate, agentsHome string) ([]importOutput, bool, error) {
	rel, err := filepath.Rel(c.sourceRoot, c.sourcePath)
	if err != nil {
		return nil, false, err
	}
	rel = filepath.ToSlash(rel)

	if outputs, ok, err := canonicalPluginOutputs(c, rel); ok {
		return outputs, true, err
	}
	return canonicalImportOutputsNonPlugin(c, rel, agentsHome)
}

// init wires two lifecycle seams whose canonical-import internals
// (canonicalImportOutputs + 30+ hook-bundle helpers, importCandidate
// struct with unexported fields) still live in commands/import.go until
// t06 moves the import command itself into commands/lifecycle/:
//
//   - RestoreCanonicalResourceFileFn: the canonical-resource branch of
//     lifecycle.RestoreFromResourcesCountedWithDeps.
//   - CanonicalImportOutputs: the lifecycle-facing entry point that
//     converts a lifecycle.ImportCandidate into []lifecycle.ImportOutput
//     by delegating to canonicalImportOutputs.
func init() {
	lifecycle.RestoreCanonicalResourceFileFn = restoreCanonicalResourceFileImpl
	lifecycle.CanonicalImportOutputs = canonicalImportOutputsImpl
}

// restoreCanonicalResourceFileImpl is the implementation registered into
// lifecycle.RestoreCanonicalResourceFileFn. Extracted from init() so the
// init function stays inside Sonar's cognitive-complexity limit (S3776).
func restoreCanonicalResourceFileImpl(project, resourcesDir, agentsHome, path string, deps lifecycle.AddDeps) (int, bool, error) {
	candidate := importCandidate{
		project:    project,
		sourceRoot: resourcesDir,
		sourcePath: path,
	}
	outputs, ok, canonErr := canonicalImportOutputs(candidate, agentsHome)
	if !ok {
		return 0, false, nil
	}
	if canonErr != nil {
		return 0, true, fmt.Errorf("canonical import for %s: %w", path, canonErr)
	}
	count := 0
	for _, output := range outputs {
		destPath := filepath.Join(agentsHome, output.destRel)
		if err := deps.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return count, true, fmt.Errorf("creating dir for %s: %w", destPath, err)
		}
		if err := deps.WriteFile(destPath, output.content, 0644); err != nil {
			return count, true, fmt.Errorf("writing %s: %w", destPath, err)
		}
		count++
	}
	return count, true, nil
}

// canonicalImportOutputsImpl is the implementation registered into
// lifecycle.CanonicalImportOutputs. Extracted from init() so the init
// function stays inside Sonar's cognitive-complexity limit (S3776).
func canonicalImportOutputsImpl(c lifecycle.ImportCandidate) ([]lifecycle.ImportOutput, bool, error) {
	candidate := importCandidate{
		project:    c.Project,
		sourceRoot: c.SourceRoot,
		sourcePath: c.SourcePath,
		destRel:    c.DestRel,
	}
	outputs, ok, err := canonicalImportOutputs(candidate, c.AgentsHome)
	if !ok || err != nil {
		return nil, ok, err
	}
	converted := make([]lifecycle.ImportOutput, 0, len(outputs))
	for _, o := range outputs {
		converted = append(converted, lifecycle.ImportOutput{
			DestRel: o.destRel,
			Content: o.content,
			Origin:  o.Origin,
		})
	}
	return converted, true, nil
}

// canonicalImportOutputsNonPlugin handles hook/settings paths after package-plugin routing.
func canonicalImportOutputsNonPlugin(c importCandidate, rel, agentsHome string) ([]importOutput, bool, error) {
	switch rel {
	case relCursorHooksJSON:
		return canonicalHookBundleOutputsFromCursorFile(c.project, c.sourcePath, agentsHome)
	case relCodexHooksJSON:
		return canonicalHookBundleOutputsFromCodexFile(c.project, c.sourcePath, agentsHome)
	case relClaudeSettingsLocal, relClaudeSettingsJSON:
		return canonicalHookBundleOutputsFromClaudeCompatFile(c.project, c.sourcePath, agentsHome)
	}

	if name, ok := githubHookBundleName(rel); ok {
		if outputs, canonOK, err := canonicalHookBundleOutputsFromCopilotFile(c.project, c.sourcePath, name, agentsHome); err == nil && canonOK {
			return outputs, true, nil
		}
		// Preserve unsupported hook files without data loss until the shared import
		// path can canonicalize more native hook shapes.
		raw, readErr := os.ReadFile(c.sourcePath)
		if readErr != nil {
			return nil, true, readErr
		}
		return []importOutput{{
			destRel: agentsHooksPrefix + c.project + "/" + name + ".json",
			content: raw,
			Origin:  "github",
		}}, true, nil
	}

	return nil, false, nil
}

func githubHookBundleName(rel string) (string, bool) {
	if !strings.HasPrefix(rel, relGitHubHooksDir) || !strings.HasSuffix(rel, relJSONSuffix) {
		return "", false
	}
	return strings.TrimSuffix(filepath.Base(rel), relJSONSuffix), true
}

// isJSONHookSyntaxError reports whether err represents genuinely malformed
// JSON — a *json.SyntaxError — as opposed to well-formed JSON that simply
// isn't shaped like the bundle the caller expects (an ordinary field-type
// mismatch, or content this reader legitimately does not recognize as a
// hook bundle). Only a true syntax error is loud-worthy: everything else is
// "not a hook bundle" content and must stay silently false so unrelated
// managed files aren't flagged as corrupt.
func isJSONHookSyntaxError(err error) bool {
	var syntaxErr *json.SyntaxError
	return errors.As(err, &syntaxErr)
}

// warnIfCorruptHookJSON emits a loud warning when a candidate hook/plugin
// file failed to parse because of genuinely malformed JSON (a true syntax
// error). Any other unmarshal error means "not a hook bundle / not a plugin
// manifest" content and stays silent so unrelated managed files are not
// flagged as corrupt.
func warnIfCorruptHookJSON(path string, err error) {
	if isJSONHookSyntaxError(err) {
		ui.Bullet("warn", fmt.Sprintf("%s is not valid JSON, skipping: %v", config.DisplayPath(path), err))
	}
}

func canonicalHookBundleContentFromCopilotFile(path, hookName string) ([]byte, error) {
	outputs, ok, err := canonicalHookBundleOutputsFromCopilotFile("ignored", path, hookName, "")
	if err != nil {
		return nil, err
	}
	if !ok || len(outputs) != 1 {
		return nil, fmt.Errorf("expected exactly one canonical copilot hook output")
	}
	return outputs[0].content, nil
}

func canonicalHookBundleOutputsFromCopilotFile(scope, path, hookName, agentsHome string) ([]importOutput, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, true, err
	}

	var payload importedCopilotHooksFile
	if err := json.Unmarshal(content, &payload); err != nil {
		warnIfCorruptHookJSON(path, err)
		return nil, false, nil
	}
	if len(payload.Hooks) == 0 {
		return nil, false, nil
	}

	eventNames := make([]string, 0, len(payload.Hooks))
	for event := range payload.Hooks {
		eventNames = append(eventNames, event)
	}
	sort.Strings(eventNames)

	specs := make([]importedHookSpec, 0)
	for _, event := range eventNames {
		when, ok := canonicalHookWhenFromCopilotEvent(event)
		if !ok {
			return nil, false, nil
		}
		for _, action := range payload.Hooks[event] {
			if action.Type != "command" || strings.TrimSpace(action.Bash) == "" {
				return nil, false, nil
			}
			specs = append(specs, importedHookSpec{
				nameHint:  hookName,
				when:      when,
				command:   strings.TrimSpace(action.Bash),
				timeoutMS: action.TimeoutSec * 1000,
				enabledOn: []string{"copilot"},
				platform:  "copilot",
			})
		}
	}

	return canonicalHookOutputsForSpecs(scope, agentsHome, specs)
}

func canonicalHookBundleOutputsFromCursorFile(scope, path, agentsHome string) ([]importOutput, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, true, err
	}
	var payload importedCursorHooksFile
	if err := json.Unmarshal(content, &payload); err != nil {
		warnIfCorruptHookJSON(path, err)
		return nil, false, nil
	}
	if len(payload.Hooks) == 0 {
		return nil, false, nil
	}
	specs := make([]importedHookSpec, 0)
	for event, entries := range payload.Hooks {
		when, ok := canonicalHookWhenFromCursorEvent(event)
		if !ok {
			return nil, false, nil
		}
		for _, entry := range entries {
			command := strings.TrimSpace(entry.Command)
			if command == "" {
				return nil, false, nil
			}
			specs = append(specs, importedHookSpec{
				when:      when,
				matcher:   strings.TrimSpace(entry.Matcher),
				command:   command,
				timeoutMS: entry.Timeout * 1000,
				enabledOn: []string{"cursor"},
				platform:  "cursor",
			})
		}
	}
	return canonicalHookOutputsForSpecs(scope, agentsHome, specs)
}

func canonicalHookBundleOutputsFromCodexFile(scope, path, agentsHome string) ([]importOutput, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, true, err
	}
	var payload importedClaudeHooksFile
	if err := json.Unmarshal(content, &payload); err != nil {
		warnIfCorruptHookJSON(path, err)
		return nil, false, nil
	}
	if len(payload.Hooks) == 0 {
		return nil, false, nil
	}
	specs, ok := collectImportedCommandHookSpecs(payload, canonicalHookWhenFromCodexEvent, []string{"codex"}, "codex")
	if !ok {
		return nil, false, nil
	}
	return canonicalHookOutputsForSpecs(scope, agentsHome, specs)
}

func canonicalHookBundleOutputsFromClaudeCompatFile(scope, path, agentsHome string) ([]importOutput, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, true, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(content, &top); err != nil {
		warnIfCorruptHookJSON(path, err)
		return nil, false, nil
	}
	if !hasOnlyClaudeCompatKeys(top) {
		return nil, false, nil
	}
	var payload importedClaudeHooksFile
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, false, nil
	}
	if len(payload.Hooks) == 0 {
		return nil, false, nil
	}
	specs, ok := collectImportedCommandHookSpecs(payload, canonicalHookWhenFromClaudeEvent, []string{"claude", "copilot"}, "claude")
	if !ok {
		return nil, false, nil
	}
	return canonicalHookOutputsForSpecs(scope, agentsHome, specs)
}

func hasOnlyClaudeCompatKeys(top map[string]json.RawMessage) bool {
	for key := range top {
		if key != "hooks" && key != "$schema" {
			return false
		}
	}
	return true
}

func collectImportedCommandHookSpecs(
	payload importedClaudeHooksFile,
	eventWhen func(string) (string, bool),
	enabledOn []string,
	platformID string,
) ([]importedHookSpec, bool) {
	specs := make([]importedHookSpec, 0)
	for event, entries := range payload.Hooks {
		when, ok := eventWhen(event)
		if !ok {
			return nil, false
		}
		for _, entry := range entries {
			matcher := strings.TrimSpace(entry.Matcher)
			for _, action := range entry.Hooks {
				command := strings.TrimSpace(action.Command)
				if action.Type != "command" || command == "" {
					return nil, false
				}
				specs = append(specs, importedHookSpec{
					when:      when,
					matcher:   matcher,
					command:   command,
					enabledOn: enabledOn,
					platform:  platformID,
				})
			}
		}
	}
	return specs, true
}

// canonicalHookOutputsForSpecs is the single choke point every hook-shaped
// import source funnels through once its native entries have been parsed
// into importedHookSpec values.
//
// Two things happen here, in this order, and the order is load-bearing:
//
//  1. Entries that `da refresh` itself rendered from an existing canonical
//     bundle are dropped (see dropBundleRenderedHookSpecs). Import exists to
//     capture hooks dot-agents does not already own; re-capturing its own
//     render output is what made refresh → import → refresh a divergent
//     loop instead of a fixed point.
//  2. Only the surviving specs are named. Filtering BEFORE naming is what
//     keeps names stable: importedHookName's `used` counter and the `total`
//     hint both depend on the spec list, so filtering afterwards would shift
//     a surviving entry's name (`stop-gate` → `stop-gate-2`) purely because
//     a bundle-owned sibling happened to sort first — reintroducing the
//     churn from the other end.
//
// A recognized hook source whose entries are ALL bundle-owned returns
// (nil, true, nil): handled, nothing to import. Returning handled=false
// there would drop the source through to the generic file-copy import,
// which for `.github/hooks/*.json` would preserve the rendered file as a
// brand new legacy hook — the same duplication by another route.
func canonicalHookOutputsForSpecs(scope, agentsHome string, specs []importedHookSpec) ([]importOutput, bool, error) {
	if len(specs) == 0 {
		return nil, false, nil
	}
	return buildCanonicalHookOutputs(scope, dropBundleRenderedHookSpecs(agentsHome, specs)), true, nil
}

// dropBundleRenderedHookSpecs removes the specs whose command is owned by a
// canonical hook bundle that already exists under agentsHome — that is, the
// entries a prior `da refresh` rendered out of those bundles.
//
// Ownership is decided by command, never by name: the importer's derived
// name for a rendered entry (`pre-compact-gate`) rarely equals the name of
// the bundle it came from (`isp-gate`), so name equality only ever
// deduplicated by coincidence, and any bundle whose author picked a
// different name — or that renders under more than one event — duplicated
// on every cycle instead.
//
// The index is rebuilt per source file rather than once per import run, and
// that is deliberate: a bundle minted while processing an earlier candidate
// must be visible to later ones. Without it, one hand-authored command
// present in both .claude/settings.json and .codex/hooks.json would be
// captured twice under two different derived names.
func dropBundleRenderedHookSpecs(agentsHome string, specs []importedHookSpec) []importedHookSpec {
	provenance := platform.NewHookProvenance(agentsHome)
	kept := make([]importedHookSpec, 0, len(specs))
	for _, spec := range specs {
		if _, owned := provenance.Owner(spec.command); owned {
			continue
		}
		kept = append(kept, spec)
	}
	return kept
}

func buildCanonicalHookOutputs(scope string, specs []importedHookSpec) []importOutput {
	used := map[string]int{}
	outputs := make([]importOutput, 0, len(specs))
	for _, spec := range specs {
		name := importedHookName(spec.nameHint, len(specs), spec.when, spec.matcher, spec.command, used)
		manifest := importedHookManifest{
			Name:      name,
			When:      spec.when,
			Run:       importedHookManifestRun{Command: spec.command, TimeoutMS: spec.timeoutMS},
			EnabledOn: append([]string{}, spec.enabledOn...),
		}
		if tools := canonicalMatchToolsFromMatcher(spec.matcher); len(tools) > 0 {
			manifest.Match.Tools = tools
		}
		if shouldSetCanonicalMatchExpression(spec.matcher) {
			manifest.Match.Expression = strings.TrimSpace(spec.matcher)
		}
		content, err := yaml.Marshal(manifest)
		if err != nil {
			continue
		}
		outputs = append(outputs, importOutput{
			destRel: agentsHooksPrefix + scope + "/" + name + "/" + relHookManifestYAML,
			content: append(content, '\n'),
			Origin:  spec.platform,
		})
	}
	return outputs
}

func importedHookName(nameHint string, total int, when, matcher, command string, used map[string]int) string {
	eventPart := sanitizeHookNamePart(strings.ReplaceAll(when, "_", "-"))
	cmdPart := sanitizeHookNamePart(commandStem(command))
	matcherPart := sanitizeHookNamePart(matcherNameHint(matcher))
	hintPart := sanitizeHookNamePart(nameHint)

	if hintPart != "" {
		return importedHookNameWithHint(hintPart, total, cmdPart, matcherPart, used)
	}
	return importedHookNameWithoutHint(eventPart, cmdPart, matcherPart, used)
}

func importedHookNameWithHint(hintPart string, total int, cmdPart, matcherPart string, used map[string]int) string {
	if total == 1 {
		return uniqueImportedHookName(hintPart, used)
	}
	cmdPart = importedHookCommandPart(cmdPart, matcherPart)
	cmdPart = trimRedundantPrefix(cmdPart, hintPart)
	if cmdPart == "" && matcherPart != "" {
		cmdPart = trimRedundantPrefix(matcherPart, hintPart)
	}
	base := hintPart
	if cmdPart != "" {
		base = base + "-" + cmdPart
	}
	return uniqueImportedHookName(base, used)
}

func importedHookNameWithoutHint(eventPart, cmdPart, matcherPart string, used map[string]int) string {
	cmdPart = importedHookCommandPart(cmdPart, matcherPart)
	cmdPart = trimRedundantPrefix(cmdPart, eventPart)
	if cmdPart == "" {
		if matcherPart != "" {
			cmdPart = trimRedundantPrefix(matcherPart, eventPart)
		} else {
			cmdPart = "hook"
		}
	}
	base := strings.Trim(strings.Join([]string{eventPart, cmdPart}, "-"), "-")
	if base == "" {
		base = "hook"
	}
	return uniqueImportedHookName(base, used)
}

func importedHookCommandPart(commandPart, matcherPart string) string {
	if shouldPreferMatcherInImportedHookName(commandPart) && matcherPart != "" {
		return matcherPart + "-" + commandPart
	}
	return commandPart
}

func uniqueImportedHookName(base string, used map[string]int) string {
	used[base]++
	if used[base] == 1 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, used[base])
}

func shouldPreferMatcherInImportedHookName(commandStem string) bool {
	switch commandStem {
	case "", "run", "hook", "script", "main", "index":
		return true
	default:
		return false
	}
}

func matcherNameHint(matcher string) string {
	tools := canonicalMatchToolsFromMatcher(matcher)
	if len(tools) == 0 {
		return ""
	}
	if len(tools) == 1 {
		return tools[0]
	}
	return tools[0] + "-" + tools[1]
}

func trimRedundantPrefix(value, prefix string) string {
	value = strings.TrimSpace(value)
	prefix = strings.TrimSpace(prefix)
	if value == "" || prefix == "" {
		return value
	}
	if value == prefix {
		return ""
	}
	if strings.HasPrefix(value, prefix+"-") {
		return strings.TrimPrefix(value, prefix+"-")
	}
	return value
}

func commandStem(command string) string {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return ""
	}
	first := filepath.Base(parts[0])
	first = strings.TrimSuffix(first, filepath.Ext(first))
	return first
}

func sanitizeHookNamePart(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func canonicalMatchToolsFromMatcher(matcher string) []string {
	matcher = strings.TrimSpace(matcher)
	if matcher == "" || matcher == "*" {
		return nil
	}
	parts := strings.Split(matcher, "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		token := strings.TrimSpace(part)
		if token == "" || !isSimpleHookToken(token) {
			return nil
		}
		out = append(out, token)
	}
	return out
}

func isSimpleHookToken(token string) bool {
	for _, r := range token {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func shouldSetCanonicalMatchExpression(matcher string) bool {
	matcher = strings.TrimSpace(matcher)
	if matcher == "" || matcher == "*" {
		return false
	}
	tools := canonicalMatchToolsFromMatcher(matcher)
	return len(tools) == 0 || strings.Join(tools, "|") != matcher
}

func canonicalHookWhenFromCopilotEvent(event string) (string, bool) {
	switch event {
	case "sessionStart":
		return "session_start", true
	case "userPromptSubmitted":
		return "user_prompt_submit", true
	case "preToolUse":
		return "pre_tool_use", true
	default:
		return "", false
	}
}

func canonicalHookWhenFromCursorEvent(event string) (string, bool) {
	switch event {
	case "preToolUse":
		return "pre_tool_use", true
	case "beforeSubmitPrompt":
		return "user_prompt_submit", true
	case "stop":
		return "stop", true
	case "sessionStart":
		return "session_start", true
	default:
		return "", false
	}
}

func canonicalHookWhenFromCodexEvent(event string) (string, bool) {
	switch event {
	case "SessionStart":
		return "session_start", true
	case "PreToolUse":
		return "pre_tool_use", true
	case "PostToolUse":
		return "post_tool_use", true
	case "UserPromptSubmit":
		return "user_prompt_submit", true
	case "Stop":
		return "stop", true
	default:
		return "", false
	}
}

func canonicalHookWhenFromClaudeEvent(event string) (string, bool) {
	switch event {
	case "PreToolUse":
		return "pre_tool_use", true
	case "PostToolUse":
		return "post_tool_use", true
	case "PostToolUseFailure":
		return "post_tool_use_failure", true
	case "Notification":
		return "notification", true
	case "UserPromptSubmit":
		return "user_prompt_submit", true
	case "SessionStart":
		return "session_start", true
	case "SessionEnd":
		return "session_end", true
	case "Stop":
		return "stop", true
	case "SubagentStart":
		return "subagent_start", true
	case "SubagentStop":
		return "subagent_stop", true
	case "PreCompact":
		return "pre_compact", true
	case "PermissionRequest":
		return "permission_request", true
	default:
		return "", false
	}
}

func filesDifferent(a, b string) (bool, error) {
	ab, err := os.ReadFile(a)
	if err != nil {
		return false, err
	}
	bb, err := os.ReadFile(b)
	if err != nil {
		return false, err
	}
	if len(ab) != len(bb) {
		return true, nil
	}
	for i := range ab {
		if ab[i] != bb[i] {
			return true, nil
		}
	}
	return false, nil
}

// isManagedSymlink is a thin alias around lifecycle.IsManagedSymlink
// (lifted in t02b). Kept as a function-var alias so existing call sites
// in import.go and add.go stay unchanged until those commands move.
var isManagedSymlink = lifecycle.IsManagedSymlink

func relinkImportedProjects(cfg *config.Config, projects map[string]bool) {
	for project := range projects {
		path := cfg.GetProjectPath(project)
		if path == "" {
			continue
		}
		var installed []platform.Platform
		for _, p := range platform.All() {
			if p.IsInstalled() {
				installed = append(installed, p)
			}
		}
		// Shared-target plan materializes cross-platform paths (repo
		// .codex/agents/*.toml, Claude shared-skills projection) BEFORE the
		// per-platform CreateLinks loop, mirroring add/install/refresh.
		if _, err := platform.RunSharedTargetProjection(project, path, installed, Flags.DryRun); err != nil {
			ui.Bullet("warn", fmt.Sprintf("shared targets: %v", err))
		}
		for _, p := range installed {
			_ = p.CreateLinks(project, path)
		}
	}
}
