package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// Version, Commit, and Describe are set at build time via ldflags.
var Version = "dev"
var Commit = ""
var Describe = ""
var refreshImport bool

func NewRefreshCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refresh [project]",
		Short: "Refresh managed setup in projects from ~/.agents/",
		Long: `Re-applies links and config from ~/.agents/ into project directories.
Use after pulling changes to ~/.agents/ or when a project's agent config is out of sync.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			return runRefresh(filter)
		},
	}
	cmd.Flags().BoolVar(&refreshImport, "import", false, "Import project/global configs into ~/.agents before relinking")
	return cmd
}

func runRefresh(projectFilter string) error {
	if refreshImport {
		if err := runImportFromRefresh(projectFilter, "all"); err != nil {
			return fmt.Errorf("import before refresh: %w", err)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.Projects) == 0 {
		ui.Info("No managed projects. Add one with: dot-agents add <path>")
		return nil
	}

	ui.Header("dot-agents refresh")

	// Determine which platforms are enabled
	ui.Section("Enabled Platforms")
	enabledPlatforms := []platform.Platform{}
	for _, p := range platform.All() {
		if !cfg.IsPlatformEnabled(p.ID()) {
			continue
		}
		enabledPlatforms = append(enabledPlatforms, p)
		if p.IsInstalled() {
			ver := p.Version()
			// Update version in config
			cfg.SetPlatformState(p.ID(), true, ver)
			if ver != "" {
				ui.Bullet("ok", fmt.Sprintf("%s (%s)", p.DisplayName(), ver))
			} else {
				ui.Bullet("ok", p.DisplayName())
			}
		} else {
			ui.Bullet("none", p.DisplayName()+" (enabled, not detected)")
		}
	}
	cfg.Save()

	if len(enabledPlatforms) == 0 {
		ui.Warn("No enabled platforms in config.json. Nothing to refresh.")
		return nil
	}

	// Resolve dot-agents git commit
	refreshCommit, refreshDescribe := resolveRefreshCommit()

	// Projects to process
	projects := cfg.ListProjects()
	if projectFilter != "" {
		path := cfg.GetProjectPath(projectFilter)
		if path == "" {
			return fmt.Errorf("project not found: %s", projectFilter)
		}
		projects = []string{projectFilter}
	}

	total := len(projects)
	count := 0
	for i, name := range projects {
		path := cfg.GetProjectPath(name)
		if path == "" || path == "." {
			ui.Warn("Skipping " + name + ": path not found")
			continue
		}
		if _, err := os.Stat(path); err != nil {
			ui.Warn("Skipping " + name + ": directory not found at " + path)
			continue
		}

		if total > 1 {
			ui.StepN(i+1, total, name)
		} else {
			fmt.Fprintf(os.Stdout, "\n%s\n", ui.BoldText(name))
		}
		fmt.Fprintf(os.Stdout, "  %s\n", ui.DimText(config.DisplayPath(path)))

		if !Flags.DryRun {
			createProjectDirs(name)
			restoreFromResources(name, path)
		}

		config.SetWindowsMirrorContext(path)
		for _, p := range enabledPlatforms {
			if !p.IsInstalled() {
				ui.Skip(p.DisplayName() + " (not installed)")
				continue
			}
			if Flags.DryRun {
				ui.DryRun("Refresh " + p.DisplayName() + " links")
				continue
			}
			if err := p.CreateLinks(name, path); err != nil {
				ui.Bullet("warn", fmt.Sprintf("%s: %v", p.DisplayName(), err))
			} else {
				ui.Bullet("ok", p.DisplayName()+" links refreshed")
			}
		}

		if !Flags.DryRun {
			writeRefreshMarker(path, refreshCommit, refreshDescribe)
		} else {
			msg := "Write .agents-refresh"
			if refreshCommit != "" {
				msg += " (commit=" + refreshCommit[:8] + ")"
			}
			ui.DryRun(msg)
		}

		count++
	}

	fmt.Fprintln(os.Stdout)
	if count == 0 {
		ui.Info("Nothing to refresh.")
	} else {
		ui.Success(fmt.Sprintf("Refreshed %d project(s).", count))
	}
	return nil
}

// resolveRefreshCommit returns the commit hash and describe string embedded at build time.
// Falls back to empty strings for dev builds.
func resolveRefreshCommit() (string, string) {
	return Commit, Describe
}

func writeRefreshMarker(projectPath, commit, describe string) {
	markerPath := filepath.Join(projectPath, ".agents-refresh")
	content := refreshMarkerContent(Version, commit, describe)
	os.WriteFile(markerPath, content, 0644)
	ensureGitignoreEntry(projectPath, ".agents-refresh")
}

func restoreFromResources(project, projectPath string) {
	restoreFromResourcesCounted(project, projectPath)
}

func mapResourceRelToDest(project, relPath string) string {
	// Explicit repo-relative → ~/.agents-relative mappings.
	// All platform MCP sources normalize into the same canonical mcp.json.
	switch relPath {
	case relCursorSettingsJSON:
		return "settings/" + project + "/cursor.json"
	case relCursorMCPJSON:
		return "mcp/" + project + "/mcp.json"
	case relCursorHooksJSON:
		return agentsHooksPrefix + project + "/cursor.json"
	case relCursorIgnore:
		return "settings/" + project + "/cursorignore"
	case relClaudeSettingsLocal:
		return "settings/" + project + "/claude-code.json"
	case relMCPJSON:
		return "mcp/" + project + "/mcp.json"
	case relVSCodeMCPJSON:
		return "mcp/" + project + "/mcp.json"
	case relOpenCodeJSON:
		return "settings/" + project + "/opencode.json"
	case relAgentsMD:
		return "rules/" + project + "/agents.md"
	case relCodexInstructionsMD, relCodexRulesMD:
		return "rules/" + project + "/agents.md"
	case relCodexConfigTOML:
		return "settings/" + project + "/codex.toml"
	case relCodexHooksJSON:
		return agentsHooksPrefix + project + "/codex.json"
	case relCopilotInstructionsMD:
		return "rules/" + project + "/copilot-instructions.md"
	}

	// .cursor/rules/ → rules/
	if strings.HasPrefix(relPath, relCursorRulesDir) {
		name := filepath.Base(relPath)
		if strings.HasPrefix(name, "global--") {
			return "rules/global/" + strings.TrimPrefix(name, "global--")
		} else if strings.HasPrefix(name, project+"--") {
			return "rules/" + project + "/" + strings.TrimPrefix(name, project+"--")
		} else if strings.HasSuffix(name, ".mdc") || strings.HasSuffix(name, ".md") {
			return "rules/" + project + "/" + name
		}
		return ""
	}

	// .agents/skills/<name>/<path> → skills/<project>/<name>/<path>
	if strings.HasPrefix(relPath, relAgentsSkillsDir) {
		rest := strings.TrimPrefix(relPath, relAgentsSkillsDir)
		return "skills/" + project + "/" + rest
	}
	// .claude/skills/<name>/<path> → skills/<project>/<name>/<path>
	if strings.HasPrefix(relPath, relClaudeSkillsDir) {
		rest := strings.TrimPrefix(relPath, relClaudeSkillsDir)
		return "skills/" + project + "/" + rest
	}

	// .github/agents/<name>.agent.md → agents/<project>/<name>/AGENT.md
	if strings.HasPrefix(relPath, relGitHubAgentsDir) && strings.HasSuffix(relPath, relAgentMarkdownSuffix) {
		name := strings.TrimSuffix(filepath.Base(relPath), relAgentMarkdownSuffix)
		return "agents/" + project + "/" + name + "/AGENT.md"
	}

	// .codex/agents/<name>/<path> → agents/<project>/<name>/<path>
	if strings.HasPrefix(relPath, relCodexAgentsDir) {
		rest := strings.TrimPrefix(relPath, relCodexAgentsDir)
		return "agents/" + project + "/" + rest
	}

	// .opencode/agent/<name>.md → agents/<project>/<name>/AGENT.md
	if strings.HasPrefix(relPath, relOpenCodeAgentsDir) && strings.HasSuffix(relPath, ".md") {
		name := strings.TrimSuffix(filepath.Base(relPath), ".md")
		return "agents/" + project + "/" + name + "/AGENT.md"
	}

	// .github/hooks/<name>.json → hooks/<project>/<name>.json
	if strings.HasPrefix(relPath, relGitHubHooksDir) && strings.HasSuffix(relPath, relJSONSuffix) {
		name := strings.TrimSuffix(filepath.Base(relPath), relJSONSuffix)
		return agentsHooksPrefix + project + "/" + name + "/HOOK.yaml"
	}

	// Pass-through: paths already under known ~/.agents dirs
	for _, prefix := range []string{"rules/", "settings/", "mcp/", "skills/", "agents/", agentsHooksPrefix} {
		if strings.HasPrefix(relPath, prefix) {
			return relPath
		}
	}

	// Root-level flat files → settings/<project>/
	if !strings.Contains(relPath, "/") {
		return "settings/" + project + "/" + relPath
	}
	return ""
}
