package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
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

	relClaudeSettingsJSON    = ".claude/settings.json"
	relCursorSettingsJSON    = ".cursor/settings.json"
	relCursorMCPJSON         = ".cursor/mcp.json"
	relCursorHooksJSON       = ".cursor/hooks.json"
	relCursorIgnore          = ".cursorignore"
	relClaudeSettingsLocal   = ".claude/settings.local.json"
	relMCPJSON               = ".mcp.json"
	relVSCodeMCPJSON         = ".vscode/mcp.json"
	relOpenCodeJSON          = "opencode.json"
	relAgentsMD              = "AGENTS.md"
	relCodexInstructionsMD   = ".codex/instructions.md"
	relCodexRulesMD          = ".codex/rules.md"
	relCodexConfigTOML       = ".codex/config.toml"
	relCodexHooksJSON        = ".codex/hooks.json"
	relCopilotInstructionsMD = ".github/copilot-instructions.md"
	relClaudeREADME          = ".claude/CLAUDE.md"
	relCursorRulesDir        = ".cursor/rules/"
	relAgentsSkillsDir       = ".agents/skills/"
	relClaudeSkillsDir       = ".claude/skills/"
	relGitHubAgentsDir       = ".github/agents/"
	relCodexAgentsDir        = ".codex/agents/"
	relOpenCodeAgentsDir     = ".opencode/agent/"
	relGitHubHooksDir        = ".github/hooks/"
	relAgentMarkdownSuffix   = ".agent.md"
	relJSONSuffix            = ".json"
	agentsHooksPrefix        = "hooks/"
)

var projectImportSingles = []string{
	relCursorSettingsJSON,
	relCursorMCPJSON,
	relCursorHooksJSON,
	relCursorIgnore,
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
}

var projectImportWalkDirs = []string{
	".cursor/rules",
	".agents/skills",
	".claude/skills",
	".github/agents",
	".codex/agents",
	".opencode/agent",
	".github/hooks",
}

var globalImportSingles = []string{
	relClaudeSettingsJSON,
	relCursorSettingsJSON,
	relCursorMCPJSON,
	relCursorHooksJSON,
	relClaudeREADME,
	relCodexConfigTOML,
	relCodexHooksJSON,
}

func NewImportCmd() *cobra.Command {
	scope := "all"
	cmd := &cobra.Command{
		Use:   "import [project]",
		Short: "Import configs from project/global scope into ~/.agents/",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectFilter := ""
			if len(args) > 0 {
				projectFilter = args[0]
			}
			return runImport(projectFilter, scope)
		},
	}
	cmd.Flags().StringVar(&scope, "scope", "all", "Import scope: project, global, or all")
	return cmd
}

func runImportFromRefresh(projectFilter, scope string) error {
	return runImportInternal(projectFilter, scope, true)
}

func runImport(projectFilter, scope string) error {
	return runImportInternal(projectFilter, scope, false)
}

func runImportInternal(projectFilter, scope string, skipRelink bool) error {
	scope, err := normalizeImportScope(scope)
	if err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	agentsHome := config.AgentsHome()

	ui.Header("dot-agents import")

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
	result := importResult{}
	for _, c := range candidates {
		delta := processImportCandidate(c, agentsHome, timestamp)
		result.imported += delta.imported
		result.skipped += delta.skipped
	}

	if !skipRelink && scope != importScopeGlobal {
		relinkImportedProjects(cfg, projectSet)
	}

	ui.Success(fmt.Sprintf("Import complete: %d imported, %d skipped.", result.imported, result.skipped))
	return nil
}

func normalizeImportScope(scope string) (string, error) {
	scope = strings.ToLower(strings.TrimSpace(scope))
	switch scope {
	case importScopeProject, importScopeGlobal, importScopeAll:
		return scope, nil
	default:
		return "", fmt.Errorf("invalid scope %q (expected: project|global|all)", scope)
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

func processImportCandidate(c importCandidate, agentsHome, timestamp string) importResult {
	if isManagedSymlink(c.sourcePath, agentsHome) {
		return importResult{}
	}

	srcInfo, err := os.Stat(c.sourcePath)
	if err != nil || srcInfo.IsDir() {
		return importResult{}
	}

	if result, ok := processCanonicalHookBundleImport(c, agentsHome, timestamp, srcInfo); ok {
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

func importMissingCandidate(c importCandidate, dest, timestamp string) importResult {
	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("Import %s -> %s", config.DisplayPath(c.sourcePath), c.destRel))
		return importResult{imported: 1}
	}

	mirrorBackup(c.project, c.sourceRoot, c.sourcePath, timestamp)
	_ = os.MkdirAll(filepath.Dir(dest), 0755)
	if err := copyFile(c.sourcePath, dest); err != nil {
		ui.Bullet("warn", fmt.Sprintf("Failed to import %s: %v", config.DisplayPath(c.sourcePath), err))
		return importResult{skipped: 1}
	}

	ui.Bullet("ok", fmt.Sprintf("Imported %s -> %s", config.DisplayPath(c.sourcePath), c.destRel))
	return importResult{imported: 1}
}

func replaceImportCandidate(c importCandidate, agentsHome, dest, timestamp string, srcInfo, destInfo os.FileInfo) importResult {
	if !ui.Confirm(importReplaceMessage(c, srcInfo, destInfo), Flags.Yes) {
		return importResult{skipped: 1}
	}
	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("Replace %s from %s", c.destRel, config.DisplayPath(c.sourcePath)))
		return importResult{imported: 1}
	}

	mirrorBackup(c.project, agentsHome, dest, timestamp)
	mirrorBackup(c.project, c.sourceRoot, c.sourcePath, timestamp)
	if err := copyFile(c.sourcePath, dest); err != nil {
		ui.Bullet("warn", fmt.Sprintf("Failed to import %s: %v", config.DisplayPath(c.sourcePath), err))
		return importResult{skipped: 1}
	}

	ui.Bullet("ok", fmt.Sprintf("Updated %s from %s", c.destRel, config.DisplayPath(c.sourcePath)))
	return importResult{imported: 1}
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
			return nil, fmt.Errorf("project not found: %s", projectFilter)
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
	if destRel == "" {
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
	if destRel == "" {
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
	home, err := os.UserHomeDir()
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
	case relClaudeREADME:
		return "rules/global/agents.md"
	case relCodexConfigTOML:
		return "settings/global/codex.toml"
	case relCodexHooksJSON:
		return "hooks/global/codex.json"
	default:
		return ""
	}
}

func processCanonicalHookBundleImport(c importCandidate, agentsHome, timestamp string, srcInfo os.FileInfo) (importResult, bool) {
	outputs, ok, err := canonicalImportOutputs(c)
	if !ok {
		return importResult{}, false
	}
	if err != nil {
		ui.Bullet("warn", fmt.Sprintf("Failed to canonicalize %s: %v", config.DisplayPath(c.sourcePath), err))
		return importResult{skipped: 1}, true
	}

	total := importResult{}
	for _, output := range outputs {
		delta := processImportOutput(c, output, agentsHome, timestamp, srcInfo)
		total.imported += delta.imported
		total.skipped += delta.skipped
	}
	return total, true
}

func processImportOutput(c importCandidate, output importOutput, agentsHome, timestamp string, srcInfo os.FileInfo) importResult {
	resolved := c
	resolved.destRel = output.destRel
	dest := resolved.destPath(agentsHome)

	destInfo, err := os.Stat(dest)
	if os.IsNotExist(err) {
		return importMissingContentCandidate(resolved, dest, output.content, timestamp)
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

	return replaceImportContentCandidate(resolved, agentsHome, dest, output.content, timestamp, srcInfo, destInfo)
}

func importMissingContentCandidate(c importCandidate, dest string, content []byte, timestamp string) importResult {
	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("Import %s -> %s", config.DisplayPath(c.sourcePath), c.destRel))
		return importResult{imported: 1}
	}

	mirrorBackup(c.project, c.sourceRoot, c.sourcePath, timestamp)
	_ = os.MkdirAll(filepath.Dir(dest), 0755)
	if err := os.WriteFile(dest, content, 0644); err != nil {
		ui.Bullet("warn", fmt.Sprintf("Failed to import %s: %v", config.DisplayPath(c.sourcePath), err))
		return importResult{skipped: 1}
	}

	ui.Bullet("ok", fmt.Sprintf("Imported %s -> %s", config.DisplayPath(c.sourcePath), c.destRel))
	return importResult{imported: 1}
}

func replaceImportContentCandidate(c importCandidate, agentsHome, dest string, content []byte, timestamp string, srcInfo, destInfo os.FileInfo) importResult {
	if !ui.Confirm(importReplaceMessage(c, srcInfo, destInfo), Flags.Yes) {
		return importResult{skipped: 1}
	}
	if Flags.DryRun {
		ui.DryRun(fmt.Sprintf("Replace %s from %s", c.destRel, config.DisplayPath(c.sourcePath)))
		return importResult{imported: 1}
	}

	mirrorBackup(c.project, agentsHome, dest, timestamp)
	mirrorBackup(c.project, c.sourceRoot, c.sourcePath, timestamp)
	if err := os.WriteFile(dest, content, 0644); err != nil {
		ui.Bullet("warn", fmt.Sprintf("Failed to import %s: %v", config.DisplayPath(c.sourcePath), err))
		return importResult{skipped: 1}
	}

	ui.Bullet("ok", fmt.Sprintf("Updated %s from %s", c.destRel, config.DisplayPath(c.sourcePath)))
	return importResult{imported: 1}
}

func canonicalImportOutputs(c importCandidate) ([]importOutput, bool, error) {
	rel, err := filepath.Rel(c.sourceRoot, c.sourcePath)
	if err != nil {
		return nil, false, err
	}
	rel = filepath.ToSlash(rel)

	switch rel {
	case relCursorHooksJSON:
		return canonicalHookBundleOutputsFromCursorFile(c.project, c.sourcePath)
	case relCodexHooksJSON:
		return canonicalHookBundleOutputsFromCodexFile(c.project, c.sourcePath)
	case relClaudeSettingsLocal, relClaudeSettingsJSON:
		return canonicalHookBundleOutputsFromClaudeCompatFile(c.project, c.sourcePath)
	}

	if name, ok := githubHookBundleName(rel); ok {
		if outputs, canonOK, err := canonicalHookBundleOutputsFromCopilotFile(c.project, c.sourcePath, name); err == nil && canonOK {
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

func canonicalHookBundleContentFromCopilotFile(path, hookName string) ([]byte, error) {
	outputs, ok, err := canonicalHookBundleOutputsFromCopilotFile("ignored", path, hookName)
	if err != nil {
		return nil, err
	}
	if !ok || len(outputs) != 1 {
		return nil, fmt.Errorf("expected exactly one canonical copilot hook output")
	}
	return outputs[0].content, nil
}

func canonicalHookBundleOutputsFromCopilotFile(scope, path, hookName string) ([]importOutput, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, true, err
	}

	var payload importedCopilotHooksFile
	if err := json.Unmarshal(content, &payload); err != nil {
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

	outputs := buildCanonicalHookOutputs(scope, specs)
	if len(outputs) == 0 {
		return nil, false, nil
	}
	return outputs, true, nil
}

func canonicalHookBundleOutputsFromCursorFile(scope, path string) ([]importOutput, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, true, err
	}
	var payload importedCursorHooksFile
	if err := json.Unmarshal(content, &payload); err != nil {
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
	outputs := buildCanonicalHookOutputs(scope, specs)
	if len(outputs) == 0 {
		return nil, false, nil
	}
	return outputs, true, nil
}

func canonicalHookBundleOutputsFromCodexFile(scope, path string) ([]importOutput, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, true, err
	}
	var payload importedClaudeHooksFile
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, false, nil
	}
	if len(payload.Hooks) == 0 {
		return nil, false, nil
	}
	specs := make([]importedHookSpec, 0)
	for event, entries := range payload.Hooks {
		when, ok := canonicalHookWhenFromCodexEvent(event)
		if !ok {
			return nil, false, nil
		}
		for _, entry := range entries {
			for _, action := range entry.Hooks {
				if action.Type != "command" || strings.TrimSpace(action.Command) == "" {
					return nil, false, nil
				}
				specs = append(specs, importedHookSpec{
					when:      when,
					matcher:   strings.TrimSpace(entry.Matcher),
					command:   strings.TrimSpace(action.Command),
					enabledOn: []string{"codex"},
					platform:  "codex",
				})
			}
		}
	}
	outputs := buildCanonicalHookOutputs(scope, specs)
	if len(outputs) == 0 {
		return nil, false, nil
	}
	return outputs, true, nil
}

func canonicalHookBundleOutputsFromClaudeCompatFile(scope, path string) ([]importOutput, bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, true, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(content, &top); err != nil {
		return nil, false, nil
	}
	for key := range top {
		if key != "hooks" && key != "$schema" {
			return nil, false, nil
		}
	}
	var payload importedClaudeHooksFile
	if err := json.Unmarshal(content, &payload); err != nil {
		return nil, false, nil
	}
	if len(payload.Hooks) == 0 {
		return nil, false, nil
	}
	specs := make([]importedHookSpec, 0)
	for event, entries := range payload.Hooks {
		when, ok := canonicalHookWhenFromClaudeEvent(event)
		if !ok {
			return nil, false, nil
		}
		for _, entry := range entries {
			for _, action := range entry.Hooks {
				if action.Type != "command" || strings.TrimSpace(action.Command) == "" {
					return nil, false, nil
				}
				specs = append(specs, importedHookSpec{
					when:      when,
					matcher:   strings.TrimSpace(entry.Matcher),
					command:   strings.TrimSpace(action.Command),
					enabledOn: []string{"claude", "copilot"},
					platform:  "claude",
				})
			}
		}
	}
	outputs := buildCanonicalHookOutputs(scope, specs)
	if len(outputs) == 0 {
		return nil, false, nil
	}
	return outputs, true, nil
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
			destRel: agentsHooksPrefix + scope + "/" + name + "/HOOK.yaml",
			content: append(content, '\n'),
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
		if total == 1 {
			return uniqueImportedHookName(hintPart, used)
		}
		if shouldPreferMatcherInImportedHookName(cmdPart) && matcherPart != "" {
			cmdPart = matcherPart + "-" + cmdPart
		}
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
	if shouldPreferMatcherInImportedHookName(cmdPart) && matcherPart != "" {
		cmdPart = matcherPart + "-" + cmdPart
	}
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

func isManagedSymlink(path, agentsHome string) bool {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	dest, err := os.Readlink(path)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(dest) {
		dest = filepath.Clean(filepath.Join(filepath.Dir(path), dest))
	}
	agentsHome = filepath.Clean(agentsHome) + string(filepath.Separator)
	return strings.HasPrefix(filepath.Clean(dest)+string(filepath.Separator), agentsHome)
}

func relinkImportedProjects(cfg *config.Config, projects map[string]bool) {
	for project := range projects {
		path := cfg.GetProjectPath(project)
		if path == "" {
			continue
		}
		for _, p := range platform.All() {
			if !p.IsInstalled() {
				continue
			}
			_ = p.CreateLinks(project, path)
		}
	}
}
