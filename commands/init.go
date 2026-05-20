package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	scaffoldhome "github.com/NikashPrakash/dot-agents/internal/scaffold/home"
	scaffoldhooks "github.com/NikashPrakash/dot-agents/internal/scaffold/hooks"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// initDirMaker is the narrow collaborator init.go's fault-injectable
// operations need (interface-DI per docs/TEST_SEAMS.md). Single-method
// today, named with the -er suffix per Go style; rename to a multi-method
// role name (cf. dirCleaner, schemaCompiler) if init.go grows additional
// seam needs later. File-scoped — do not share with other commands files.
type initDirMaker interface {
	MkdirAll(path string, perm os.FileMode) error
}

// stdInitDirMaker is the production initDirMaker backed by the os package.
type stdInitDirMaker struct{}

func (stdInitDirMaker) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func NewInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize ~/.agents/ directory structure",
		Long: `Creates the ~/.agents/ directory structure with starter templates.
Safe to run multiple times - existing files are preserved unless --force.

Run this once per machine before using add, install, refresh, or workflow
commands that expect the shared store to exist.`,
		Example: ExampleBlock(
			"  da init",
			"  da init --dry-run",
			"  da init --force",
		),
		Args: NoArgsWithHints("`da init` bootstraps the shared store and does not take a project path."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(cmd, args, stdInitDirMaker{})
		},
	}
	return cmd
}

func runInit(cmd *cobra.Command, args []string, deps initDirMaker) error {
	agentsHome := config.AgentsHome()

	ui.Header("da init")

	// Check existing
	ui.Step("Checking existing installation...")
	if _, err := os.Stat(agentsHome); err == nil {
		if !Flags.Force {
			ui.Bullet("found", "Existing ~/.agents/ directory found")
			fmt.Fprintln(os.Stdout, "\n  Use --force to reinitialize (creates backup first)")
			return nil
		}
		ui.Bullet("warn", "Will reinitialize (--force)")
	} else {
		ui.Bullet("none", "No existing ~/.agents/ found")
	}

	if Flags.DryRun {
		ui.DryRun("Create ~/.agents/ directory structure")
		fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
		return nil
	}

	if !Flags.Yes {
		if !ui.Confirm("Proceed with initialization?", false) {
			ui.Info("Initialization cancelled.")
			return nil
		}
	}

	ui.Step("Creating directories and files...")

	if err := createInitialAgentsDirs(agentsHome, deps); err != nil {
		return err
	}
	ui.Bullet("ok", "Created directory structure")

	if err := seedInitialConfig(agentsHome); err != nil {
		return err
	}

	if err := scaffoldStarterHomeAssets(agentsHome); err != nil {
		return fmt.Errorf("scaffolding starter home assets: %w", err)
	}
	ui.Bullet("ok", "Scaffolded starter home assets")

	if err := scaffoldWorkflowAssets(agentsHome, deps); err != nil {
		return fmt.Errorf("scaffolding starter hook bundles: %w", err)
	}
	ui.Bullet("ok", "Scaffolded starter workflow hook bundles")

	if err := ensureGlobalKGMCPConfigs(agentsHome); err != nil {
		return fmt.Errorf("scaffolding starter KG MCP configs: %w", err)
	}

	// Global Claude Code settings symlink — hooks/ takes priority over settings/
	if err := linkClaudeGlobalSettings(agentsHome, deps); err != nil {
		return err
	}
	if err := linkCursorGlobalHooks(agentsHome, deps); err != nil {
		return err
	}

	// State dir — best-effort idempotent create.
	_ = deps.MkdirAll(config.AgentsStateDir(), 0755)
	ui.Bullet("ok", "Created state directory")

	ui.SuccessBox("Initialization complete!",
		"Add your first project: da add ~/path/to/project",
		"See the canonical layout: da explain structure",
		"Team member with manifest: da install  (instead of add)",
		"Set up git sync: da sync init",
		"Check health: da doctor",
	)
	return nil
}

// sidecarBackupFile preserves an unmanaged occupant before links replaces it
// with a managed link in init's --force path. It mirrors the established
// internal/platform convention: write the existing bytes to a sibling
// <path>.dot-agents-backup. links calls this BEFORE removing the entry and
// only proceeds with replacement if it returns nil, so a backup failure
// aborts the replace and leaves the user's file intact (no data loss).
func sidecarBackupFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s for backup: %w", path, err)
	}
	bak := path + ".dot-agents-backup"
	if err := os.WriteFile(bak, data, 0644); err != nil {
		return fmt.Errorf("write backup %s: %w", bak, err)
	}
	return nil
}

func scaffoldStarterHomeAssets(agentsHome string) error {
	return scaffoldhome.CopyMissingStarterAssets(agentsHome)
}

func scaffoldWorkflowAssets(agentsHome string, deps initDirMaker) error {
	if err := deps.MkdirAll(config.AgentsContextDir(), 0755); err != nil {
		return err
	}
	return scaffoldhooks.CopyMissingGlobalBundles(filepath.Join(agentsHome, "hooks", "global"))
}

// createInitialAgentsDirs creates the canonical ~/.agents/ directory
// shape plus per-platform CanonicalStoreBucket scope roots. Each
// MkdirAll goes through the injected deps so the error branch is
// fault-injectable. Idempotent on re-run.
func createInitialAgentsDirs(agentsHome string, deps initDirMaker) error {
	dirs := []string{
		agentsHome,
		filepath.Join(agentsHome, "resources"),
		filepath.Join(agentsHome, "rules", "global"),
		filepath.Join(agentsHome, "settings", "global"),
		filepath.Join(agentsHome, "mcp", "global"),
		filepath.Join(agentsHome, "skills", "global"),
		filepath.Join(agentsHome, "agents", "global"),
		filepath.Join(agentsHome, "hooks", "global"),
		config.AgentsContextDir(),
		filepath.Join(agentsHome, "scripts"),
		filepath.Join(agentsHome, "local"),
	}
	for _, bucket := range platform.CanonicalStoreBucketSpecs() {
		dirs = append(dirs, platform.CanonicalBucketScopeRoot(agentsHome, bucket.Name, "global"))
	}
	for _, d := range dirs {
		if err := deps.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}
	return nil
}

// seedInitialConfig writes ~/.agents/config.json when it does not
// already exist (or under --force). Detects installed platforms and
// records their state + version so subsequent commands can rely on a
// pre-populated registry.
func seedInitialConfig(agentsHome string) error {
	cfgPath := filepath.Join(agentsHome, "config.json")
	if _, err := os.Stat(cfgPath); !(os.IsNotExist(err) || Flags.Force) {
		return nil
	}
	cfg := &config.Config{
		Version:  1,
		Projects: make(map[string]config.Project),
		Agents:   make(map[string]config.Agent),
	}
	ui.Section("Detected Platforms")
	for _, p := range platform.All() {
		recordPlatformState(cfg, p)
	}
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	return nil
}

// recordPlatformState writes one platform's detected presence + version
// into cfg and renders the bullet line. Pulled out so seedInitialConfig
// has a flat control flow (one branch per platform, no nested
// installed/version conditionals).
func recordPlatformState(cfg *config.Config, p platform.Platform) {
	if !p.IsInstalled() {
		cfg.SetPlatformState(p.ID(), false, "")
		ui.Bullet("none", p.DisplayName()+" (not detected)")
		return
	}
	ver := p.Version()
	cfg.SetPlatformState(p.ID(), true, ver)
	if ver != "" {
		ui.Bullet("ok", fmt.Sprintf("%s (%s)", p.DisplayName(), ver))
	} else {
		ui.Bullet("ok", p.DisplayName())
	}
}

// linkClaudeGlobalSettings creates the global ~/.claude/settings.json
// symlink when Claude Code is installed. Hooks/global/claude-code.json
// takes priority over settings/global/claude-code.json. --force routes
// through the backup-preserving link so an unmanaged user file is kept
// as <path>.dot-agents-backup rather than destroyed; link errors are
// propagated (links returns ErrUnmanagedTarget for unmanaged occupants
// and swallowing it would print false success).
func linkClaudeGlobalSettings(agentsHome string, deps initDirMaker) error {
	claudePlatform := platform.ByID("claude")
	if claudePlatform == nil || !claudePlatform.IsInstalled() {
		return nil
	}
	claudeHooksSrc := filepath.Join(agentsHome, "hooks", "global", "claude-code.json")
	claudeSettingsPath := filepath.Join(agentsHome, "settings", "global", "claude-code.json")
	if _, err := os.Stat(claudeHooksSrc); err == nil {
		claudeSettingsPath = claudeHooksSrc
	}
	home := config.UserHome()
	claudeDir := filepath.Join(home, ".claude")
	_ = deps.MkdirAll(claudeDir, 0755) // best-effort; SymlinkReplacing surfaces any real failure
	claudeSettings := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Lstat(claudeSettings); !(os.IsNotExist(err) || Flags.Force) {
		ui.Bullet("skip", "~/.claude/settings.json exists (use --force to replace)")
		return nil
	}
	if err := links.SymlinkReplacing(claudeSettingsPath, claudeSettings, sidecarBackupFile); err != nil {
		return fmt.Errorf("linking %s: %w", claudeSettings, err)
	}
	ui.Bullet("ok", "Created Claude Code global settings symlink")
	return nil
}

// linkCursorGlobalHooks creates the global ~/.cursor/hooks.json hardlink
// when Cursor is installed and the source hooks file exists. Same
// backup-preserving contract as linkClaudeGlobalSettings.
func linkCursorGlobalHooks(agentsHome string, deps initDirMaker) error {
	cursorPlatform := platform.ByID("cursor")
	if cursorPlatform == nil || !cursorPlatform.IsInstalled() {
		return nil
	}
	cursorHooksSrc := filepath.Join(agentsHome, "hooks", "global", "cursor.json")
	if _, err := os.Stat(cursorHooksSrc); err != nil {
		return nil
	}
	home := config.UserHome()
	cursorDir := filepath.Join(home, ".cursor")
	_ = deps.MkdirAll(cursorDir, 0755) // best-effort; HardlinkReplacing surfaces any real failure
	cursorHooksDst := filepath.Join(cursorDir, "hooks.json")
	if _, err := os.Lstat(cursorHooksDst); !(os.IsNotExist(err) || Flags.Force) {
		ui.Bullet("skip", "~/.cursor/hooks.json exists (use --force to replace)")
		return nil
	}
	if err := links.HardlinkReplacing(cursorHooksSrc, cursorHooksDst, sidecarBackupFile); err != nil {
		return fmt.Errorf("linking %s: %w", cursorHooksDst, err)
	}
	ui.Bullet("ok", "Created Cursor global hooks hardlink")
	return nil
}
