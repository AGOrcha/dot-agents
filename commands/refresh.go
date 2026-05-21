package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/projectsync"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// Version, Commit, and Describe are set at build time via ldflags.
var Version = "dev"
var Commit = ""
var Describe = ""
var refreshImport bool

// refreshConfigLoader is the narrow collaborator refresh.go's
// fault-injectable LoadConfig operation needs (interface-DI per
// docs/TEST_SEAMS.md). Single-method, file-prefixed -er form; file-scoped
// — do not share with other commands files.
type refreshConfigLoader interface {
	LoadConfig() (*config.Config, error)
}

// stdRefreshConfigLoader is the production refreshConfigLoader backed by
// internal/config.Load.
type stdRefreshConfigLoader struct{}

func (stdRefreshConfigLoader) LoadConfig() (*config.Config, error) { return config.Load() }

func NewRefreshCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "refresh [project]",
		Short: "Refresh managed setup in projects from ~/.agents/",
		Long: `Re-applies links and config from ~/.agents/ into project directories.
Use after pulling changes to ~/.agents/ or when a project's agent config is out of sync.`,
		Example: ExampleBlock(
			"  da refresh",
			"  da refresh billing-api",
			"  da refresh --import --dry-run",
		),
		Args: MaximumNArgsWithHints(1, "Optionally pass one managed project name to limit the refresh."),
		RunE: func(cmd *cobra.Command, args []string) error {
			filter := ""
			if len(args) > 0 {
				filter = args[0]
			}
			return runRefresh(filter, stdRefreshConfigLoader{}, stdImportDeps{}, stdAddDeps{})
		},
	}
	cmd.Flags().BoolVar(&refreshImport, "import", false, "Also import global user configs into ~/.agents before relinking")
	return cmd
}

func runRefresh(projectFilter string, deps refreshConfigLoader, importD importDeps, addD addDeps) error {
	if err := runImportFromRefresh(projectFilter, refreshImportScope(), importD); err != nil {
		return fmt.Errorf("import before refresh: %w", err)
	}

	cfg, err := deps.LoadConfig()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if len(cfg.Projects) == 0 {
		ui.Info("No managed projects. Add one with: da add <path>")
		return nil
	}

	ui.Header("da refresh")

	enabledPlatforms := reportEnabledPlatforms(cfg)
	cfg.Save()
	if len(enabledPlatforms) == 0 {
		ui.Warn("No enabled platforms in config.json. Nothing to refresh.")
		return nil
	}

	installedEnabled := platform.InstalledEnabledPlatforms(cfg)
	refreshCommit, refreshDescribe := resolveRefreshCommit()

	projects, err := resolveRefreshProjects(cfg, projectFilter)
	if err != nil {
		return err
	}

	total := len(projects)
	count := 0
	var failed []string
	for i, name := range projects {
		path := cfg.GetProjectPath(name)
		if !checkRefreshProjectPath(name, path) {
			continue
		}
		announceRefreshProject(name, path, i, total)
		noteManifestGitSources(path)

		projectFailed := refreshOneProject(name, path, enabledPlatforms, installedEnabled, addD)

		stamped := finalizeProjectRefresh(name, path, projectFailed, refreshCommit, refreshDescribe)
		if stamped {
			count++
		} else if !Flags.DryRun {
			failed = append(failed, name)
		}
	}

	fmt.Fprintln(os.Stdout)
	if count == 0 && len(failed) == 0 {
		ui.Info("Nothing to refresh.")
	} else if count > 0 {
		ui.Success(fmt.Sprintf("Refreshed %d project(s).", count))
	}
	if len(failed) > 0 {
		return ErrorWithHints(
			fmt.Sprintf("refresh incomplete for %d project(s): %s", len(failed), strings.Join(failed, ", ")),
			"The listed projects were NOT marked refreshed (partial application). Re-run after resolving the warnings above; unmanaged files in the way must be imported, backed up, or removed.",
		)
	}
	return nil
}

// reportEnabledPlatforms prints the "Enabled Platforms" section and returns
// the slice of platforms enabled in cfg. Installed platforms have their
// version recorded back into cfg so the caller can persist via cfg.Save().
func reportEnabledPlatforms(cfg *config.Config) []platform.Platform {
	ui.Section("Enabled Platforms")
	enabled := []platform.Platform{}
	for _, p := range platform.All() {
		if !cfg.IsPlatformEnabled(p.ID()) {
			continue
		}
		enabled = append(enabled, p)
		if !p.IsInstalled() {
			ui.Bullet("none", p.DisplayName()+" (enabled, not detected)")
			continue
		}
		ver := p.Version()
		cfg.SetPlatformState(p.ID(), true, ver)
		if ver != "" {
			ui.Bullet("ok", fmt.Sprintf("%s (%s)", p.DisplayName(), ver))
		} else {
			ui.Bullet("ok", p.DisplayName())
		}
	}
	return enabled
}

// resolveRefreshProjects returns the project list to refresh: every managed
// project, or just the filter target when one was provided. An unknown filter
// produces a typed error with a recovery hint.
func resolveRefreshProjects(cfg *config.Config, projectFilter string) ([]string, error) {
	if projectFilter == "" {
		return cfg.ListProjects(), nil
	}
	if cfg.GetProjectPath(projectFilter) == "" {
		return nil, ErrorWithHints(
			fmt.Sprintf("project not found: %s", projectFilter),
			"Run `da status` to see the registered project names.",
		)
	}
	return []string{projectFilter}, nil
}

// checkRefreshProjectPath reports whether the project's recorded path is a
// real, present directory. It emits the user-facing warn on skip so callers
// just consult the bool.
func checkRefreshProjectPath(name, path string) bool {
	if path == "" || path == "." {
		ui.Warn("Skipping " + name + ": path not found")
		return false
	}
	if _, err := os.Stat(path); err != nil {
		ui.Warn("Skipping " + name + ": directory not found at " + path)
		return false
	}
	return true
}

// announceRefreshProject prints the per-project banner — StepN heading when
// processing multiple projects, plain bold name for a single-project run —
// followed by the dimmed display path.
func announceRefreshProject(name, path string, i, total int) {
	if total > 1 {
		ui.StepN(i+1, total, name)
	} else {
		fmt.Fprintf(os.Stdout, "\n%s\n", ui.BoldText(name))
	}
	fmt.Fprintf(os.Stdout, "  %s\n", ui.DimText(config.DisplayPath(path)))
}

// noteManifestGitSources prints the one-shot hint that the project's manifest
// has git sources and `install` (not `refresh`) is the way to re-resolve them.
// A missing or unreadable manifest is silently skipped — refresh is
// well-defined for manifest-less projects.
func noteManifestGitSources(path string) {
	rc, err := config.LoadAgentsRC(path)
	if err != nil {
		return
	}
	for _, src := range rc.Sources {
		if src.Type == "git" {
			fmt.Fprintf(os.Stdout, "  %sℹ  .agentsrc.json has git sources — use 'da install' to re-resolve%s\n", ui.Dim, ui.Reset)
			return
		}
	}
}

// refreshOneProject performs the per-project body: optional restore-from-
// resources, shared-target projection, and CreateLinks across every enabled
// platform. Returns true when ANY sub-step failed so the caller can withhold
// the success-stamp from a partial application.
func refreshOneProject(name, path string, enabledPlatforms, installedEnabled []platform.Platform, addD addDeps) bool {
	projectFailed := false
	if !Flags.DryRun {
		projectsync.CreateProjectDirs(name)
		if err := restoreFromResources(name, path, addD); err != nil {
			ui.Bullet("warn", fmt.Sprintf("restore from resources: %v", err))
			projectFailed = true
		}
	}

	config.SetWindowsMirrorContext(path)

	if runSharedTargetsForRefresh(name, path, installedEnabled) {
		projectFailed = true
	}
	if recreatePlatformLinks(name, path, enabledPlatforms) {
		projectFailed = true
	}
	return projectFailed
}

// runSharedTargetsForRefresh runs the shared-target projection and prints any
// dry-run plan lines. Returns true when a non-dry-run projection failed
// (caller withholds the success stamp); dry-run failures are surfaced as
// warnings but do not propagate.
func runSharedTargetsForRefresh(name, path string, installedEnabled []platform.Platform) bool {
	lines, err := platform.RunSharedTargetProjection(name, path, installedEnabled, Flags.DryRun)
	if err != nil {
		if Flags.DryRun {
			ui.Bullet("warn", fmt.Sprintf("shared targets plan: %v", err))
			return false
		}
		ui.Bullet("warn", fmt.Sprintf("shared targets: %v", err))
		return true
	}
	for _, line := range lines {
		ui.DryRun(line)
	}
	return false
}

// recreatePlatformLinks re-runs CreateLinks for every enabled+installed
// platform. Returns true when any platform's CreateLinks failed.
func recreatePlatformLinks(name, path string, enabledPlatforms []platform.Platform) bool {
	failed := false
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
			failed = true
			continue
		}
		ui.Bullet("ok", p.DisplayName()+" links refreshed")
	}
	return failed
}

// finalizeProjectRefresh writes the refresh metadata stamp when the project
// finished cleanly. Returns true on a successful stamp (counted toward the
// success total) and false on dry-run, partial application, or stamp failure.
// Dry-run is treated as success for the counter but skips the manifest write.
func finalizeProjectRefresh(name, path string, projectFailed bool, refreshCommit, refreshDescribe string) bool {
	if Flags.DryRun {
		msg := "Update .agentsrc.json refresh details"
		if refreshCommit != "" {
			msg += " (commit=" + refreshCommit[:8] + ")"
		}
		ui.DryRun(msg)
		return true
	}
	if projectFailed {
		ui.Bullet("warn", "skipping refresh metadata for "+name+" — refresh was partial")
		return false
	}
	if err := projectsync.WriteRefreshToAgentsRC(name, path, Version, refreshCommit, refreshDescribe); err != nil {
		ui.Bullet("warn", fmt.Sprintf("manifest refresh metadata: %v", err))
		return false
	}
	return true
}

func refreshImportScope() string {
	if refreshImport {
		return importScopeAll
	}
	return importScopeProject
}

// resolveRefreshCommit returns the commit hash and describe string embedded at build time.
// Falls back to empty strings for dev builds.
func resolveRefreshCommit() (string, string) {
	return Commit, Describe
}

// restoreFromResources restores files from ~/.agents/resources/<project>/.
// It returns a non-nil error if any walk/mkdir/write/copy failed so callers
// that stamp success metadata can treat a partial restore as a failure
// instead of a silent false-success.
func restoreFromResources(project, projectPath string, deps addDeps) error {
	_, err := restoreFromResourcesCountedWithDeps(project, projectPath, deps)
	return err
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
	case relCursorIndexingIgnore:
		return platform.CanonicalBucketScopePath(platform.CanonicalBucketIgnore, project, "cursorindexingignore")
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

	// Directory-bucket mappings. The relPath is a full walked file path like
	// ".cursor/commands/foo.md"; the constants are directory prefixes ending
	// in "/". These MUST be prefix matches (not exact-string switch cases) or
	// the bucket files silently fall through and are dropped from recovery.
	for _, m := range []struct {
		prefix string
		bucket platform.CanonicalBucket
	}{
		{relCursorCommandsDir, platform.CanonicalBucketCommands},
		{relClaudeCommandsDir, platform.CanonicalBucketCommands},
		{relOpenCodeCommandsDir, platform.CanonicalBucketCommands},
		{relClaudeOutputStylesDir, platform.CanonicalBucketOutputStyles},
		{relOpenCodeModesDir, platform.CanonicalBucketModes},
		{relOpenCodeThemesDir, platform.CanonicalBucketThemes},
		{relGitHubPromptsDir, platform.CanonicalBucketPrompts},
	} {
		if strings.HasPrefix(relPath, m.prefix) {
			return platform.CanonicalBucketScopePath(m.bucket, project, strings.TrimPrefix(relPath, m.prefix))
		}
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
	for _, prefix := range []string{
		"rules/",
		"settings/",
		"mcp/",
		"skills/",
		"agents/",
		agentsHooksPrefix,
		string(platform.CanonicalBucketCommands) + "/",
		string(platform.CanonicalBucketOutputStyles) + "/",
		string(platform.CanonicalBucketIgnore) + "/",
		string(platform.CanonicalBucketModes) + "/",
		string(platform.CanonicalBucketPlugins) + "/",
		string(platform.CanonicalBucketThemes) + "/",
		string(platform.CanonicalBucketPrompts) + "/",
	} {
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
