package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dot-agents/dot-agents/internal/config"
	"github.com/dot-agents/dot-agents/internal/platform"
	"github.com/dot-agents/dot-agents/internal/ui"
	"github.com/spf13/cobra"
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
	relCopilotInstructionsMD = ".github/copilot-instructions.md"
	relClaudeREADME          = ".claude/CLAUDE.md"
	relCursorRulesDir        = ".cursor/rules/"
	relAgentsSkillsDir       = ".agents/skills/"
	relClaudeSkillsDir       = ".claude/skills/"
	relGitHubAgentsDir       = ".github/agents/"
	relCodexAgentsDir        = ".codex/agents/"
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
	relCopilotInstructionsMD,
}

var projectImportWalkDirs = []string{
	".cursor/rules",
	".agents/skills",
	".claude/skills",
	".github/agents",
	".codex/agents",
	".github/hooks",
}

var globalImportSingles = []string{
	relClaudeSettingsJSON,
	relCursorSettingsJSON,
	relCursorMCPJSON,
	relCursorHooksJSON,
	relClaudeREADME,
	relCodexConfigTOML,
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
	default:
		return ""
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
