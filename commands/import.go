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
	scope = strings.ToLower(strings.TrimSpace(scope))
	if scope != "project" && scope != "global" && scope != "all" {
		return fmt.Errorf("invalid scope %q (expected: project|global|all)", scope)
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	agentsHome := config.AgentsHome()

	ui.Header("dot-agents import")

	var candidates []importCandidate
	projectSet := map[string]bool{}
	if scope == "project" || scope == "all" {
		projectCandidates, err := scanProjectImportCandidates(cfg, projectFilter)
		if err != nil {
			return err
		}
		candidates = append(candidates, projectCandidates...)
		for _, c := range projectCandidates {
			projectSet[c.project] = true
		}
	}
	if scope == "global" || scope == "all" {
		candidates = append(candidates, scanGlobalImportCandidates()...)
	}

	if len(candidates) == 0 {
		ui.Info("No import candidates found.")
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].project == candidates[j].project {
			return candidates[i].sourcePath < candidates[j].sourcePath
		}
		return candidates[i].project < candidates[j].project
	})

	timestamp := time.Now().Format("20060102-150405")
	imported := 0
	skipped := 0
	for _, c := range candidates {
		dest := c.destPath(agentsHome)
		if isManagedSymlink(c.sourcePath, agentsHome) {
			continue
		}

		srcInfo, srcErr := os.Stat(c.sourcePath)
		if srcErr != nil || srcInfo.IsDir() {
			continue
		}

		destInfo, destErr := os.Stat(dest)
		if os.IsNotExist(destErr) {
			if Flags.DryRun {
				ui.DryRun(fmt.Sprintf("Import %s -> %s", config.DisplayPath(c.sourcePath), c.destRel))
				imported++
				continue
			}
			mirrorBackup(c.project, c.sourceRoot, c.sourcePath, timestamp)
			_ = os.MkdirAll(filepath.Dir(dest), 0755)
			if err := copyFile(c.sourcePath, dest); err != nil {
				ui.Bullet("warn", fmt.Sprintf("Failed to import %s: %v", config.DisplayPath(c.sourcePath), err))
				skipped++
				continue
			}
			ui.Bullet("ok", fmt.Sprintf("Imported %s -> %s", config.DisplayPath(c.sourcePath), c.destRel))
			imported++
			continue
		}
		if destErr != nil {
			ui.Bullet("warn", fmt.Sprintf("Failed to inspect %s: %v", c.destRel, destErr))
			skipped++
			continue
		}

		different, err := filesDifferent(c.sourcePath, dest)
		if err != nil {
			ui.Bullet("warn", fmt.Sprintf("Failed to compare %s and %s: %v", config.DisplayPath(c.sourcePath), c.destRel, err))
			skipped++
			continue
		}
		if !different {
			continue
		}

		sourceNewer := srcInfo.ModTime().After(destInfo.ModTime())
		msg := fmt.Sprintf("Import newer=%s into %s? (src=%s, dest=%s)",
			map[bool]string{true: "source", false: "destination"}[sourceNewer],
			c.destRel,
			srcInfo.ModTime().Format(time.RFC3339),
			destInfo.ModTime().Format(time.RFC3339),
		)
		if !ui.Confirm(msg, Flags.Yes) {
			skipped++
			continue
		}
		if Flags.DryRun {
			ui.DryRun(fmt.Sprintf("Replace %s from %s", c.destRel, config.DisplayPath(c.sourcePath)))
			imported++
			continue
		}

		// Preserve current ~/.agents value before overwrite.
		mirrorBackup(c.project, agentsHome, dest, timestamp)
		// Preserve imported source in resources snapshot as well.
		mirrorBackup(c.project, c.sourceRoot, c.sourcePath, timestamp)
		if err := copyFile(c.sourcePath, dest); err != nil {
			ui.Bullet("warn", fmt.Sprintf("Failed to import %s: %v", config.DisplayPath(c.sourcePath), err))
			skipped++
			continue
		}
		ui.Bullet("ok", fmt.Sprintf("Updated %s from %s", c.destRel, config.DisplayPath(c.sourcePath)))
		imported++
	}

	if !skipRelink && scope != "global" {
		relinkImportedProjects(cfg, projectSet)
	}

	ui.Success(fmt.Sprintf("Import complete: %d imported, %d skipped.", imported, skipped))
	return nil
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
	var out []importCandidate
	addIfMapped := func(rel string) {
		src := filepath.Join(projectPath, rel)
		if isBackupArtifact(filepath.Base(rel)) {
			return
		}
		if _, err := os.Lstat(src); err != nil {
			return
		}
		destRel := mapResourceRelToDest(project, rel)
		if destRel == "" {
			return
		}
		out = append(out, importCandidate{
			project:    project,
			sourceRoot: projectPath,
			sourcePath: src,
			destRel:    destRel,
		})
	}

	single := []string{
		".cursor/settings.json", ".cursor/mcp.json", ".cursor/hooks.json", ".cursorignore",
		".claude/settings.local.json", ".mcp.json", ".vscode/mcp.json",
		"opencode.json", "AGENTS.md", ".codex/instructions.md", ".codex/rules.md",
		".codex/config.toml", ".github/copilot-instructions.md",
	}
	for _, rel := range single {
		addIfMapped(rel)
	}

	walkDirs := []string{
		".cursor/rules", ".agents/skills", ".claude/skills", ".github/agents", ".codex/agents",
		".github/hooks",
	}
	for _, relDir := range walkDirs {
		root := filepath.Join(projectPath, relDir)
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if isBackupArtifact(d.Name()) {
				return nil
			}
			rel, err := filepath.Rel(projectPath, path)
			if err != nil {
				return nil
			}
			rel = filepath.ToSlash(rel)
			destRel := mapResourceRelToDest(project, rel)
			if destRel == "" {
				return nil
			}
			out = append(out, importCandidate{
				project:    project,
				sourceRoot: projectPath,
				sourcePath: path,
				destRel:    destRel,
			})
			return nil
		})
	}
	return out
}

func scanGlobalImportCandidates() []importCandidate {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	cases := []string{
		".claude/settings.json",
		".cursor/settings.json",
		".cursor/mcp.json",
		".cursor/hooks.json",
		".claude/CLAUDE.md",
		".codex/config.toml",
	}
	var out []importCandidate
	for _, rel := range cases {
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
	case ".claude/settings.json":
		return "settings/global/claude-code.json"
	case ".cursor/settings.json":
		return "settings/global/cursor.json"
	case ".cursor/mcp.json":
		return "mcp/global/mcp.json"
	case ".cursor/hooks.json":
		return "hooks/global/cursor.json"
	case ".claude/CLAUDE.md":
		return "rules/global/agents.md"
	case ".codex/config.toml":
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
