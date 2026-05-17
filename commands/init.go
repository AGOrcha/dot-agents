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
		RunE: runInit,
	}
	return cmd
}

func runInit(cmd *cobra.Command, args []string) error {
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
		if err := osMkdirAll(d, 0755); err != nil {
			return fmt.Errorf("creating %s: %w", d, err)
		}
	}
	ui.Bullet("ok", "Created directory structure")

	// Create config.json if missing
	cfgPath := filepath.Join(agentsHome, "config.json")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) || Flags.Force {
		cfg := &config.Config{
			Version:  1,
			Projects: make(map[string]config.Project),
			Agents:   make(map[string]config.Agent),
		}
		// Detect installed platforms
		ui.Section("Detected Platforms")
		for _, p := range platform.All() {
			if p.IsInstalled() {
				cfg.SetPlatformState(p.ID(), true, p.Version())
				ver := p.Version()
				if ver != "" {
					ui.Bullet("ok", fmt.Sprintf("%s (%s)", p.DisplayName(), ver))
				} else {
					ui.Bullet("ok", p.DisplayName())
				}
			} else {
				cfg.SetPlatformState(p.ID(), false, "")
				ui.Bullet("none", p.DisplayName()+" (not detected)")
			}
		}
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("saving config: %w", err)
		}
	}

	if err := scaffoldStarterHomeAssets(agentsHome); err != nil {
		return fmt.Errorf("scaffolding starter home assets: %w", err)
	}
	ui.Bullet("ok", "Scaffolded starter home assets")

	if err := scaffoldWorkflowAssets(agentsHome); err != nil {
		return fmt.Errorf("scaffolding starter hook bundles: %w", err)
	}
	ui.Bullet("ok", "Scaffolded starter workflow hook bundles")

	if err := ensureGlobalKGMCPConfigs(agentsHome); err != nil {
		return fmt.Errorf("scaffolding starter KG MCP configs: %w", err)
	}

	// Global Claude Code settings symlink — hooks/ takes priority over settings/
	claudeHooksSrc := filepath.Join(agentsHome, "hooks", "global", "claude-code.json")
	claudeSettingsPath := filepath.Join(agentsHome, "settings", "global", "claude-code.json")
	if _, err := os.Stat(claudeHooksSrc); err == nil {
		claudeSettingsPath = claudeHooksSrc
	}
	claudePlatform := platform.ByID("claude")
	if claudePlatform != nil && claudePlatform.IsInstalled() {
		home := config.UserHome()
		claudeDir := filepath.Join(home, ".claude")
		os.MkdirAll(claudeDir, 0755)
		claudeSettings := filepath.Join(claudeDir, "settings.json")
		if _, err := os.Lstat(claudeSettings); os.IsNotExist(err) || Flags.Force {
			// --force is a deliberate replace. Route through the
			// backup-preserving path so an unmanaged user
			// ~/.claude/settings.json is kept as <path>.dot-agents-backup
			// (the established repo convention) rather than destroyed, and
			// propagate the error: links now returns ErrUnmanagedTarget for
			// unmanaged occupants, so swallowing it would print false
			// success while global setup was NOT applied.
			if err := links.SymlinkReplacing(claudeSettingsPath, claudeSettings, sidecarBackupFile); err != nil {
				return fmt.Errorf("linking %s: %w", claudeSettings, err)
			}
			ui.Bullet("ok", "Created Claude Code global settings symlink")
		} else {
			ui.Bullet("skip", "~/.claude/settings.json exists (use --force to replace)")
		}
	}

	// Global Cursor hooks hardlink
	cursorPlatform := platform.ByID("cursor")
	if cursorPlatform != nil && cursorPlatform.IsInstalled() {
		cursorHooksSrc := filepath.Join(agentsHome, "hooks", "global", "cursor.json")
		if _, err := os.Stat(cursorHooksSrc); err == nil {
			home := config.UserHome()
			cursorDir := filepath.Join(home, ".cursor")
			os.MkdirAll(cursorDir, 0755)
			cursorHooksDst := filepath.Join(cursorDir, "hooks.json")
			if _, err := os.Lstat(cursorHooksDst); os.IsNotExist(err) || Flags.Force {
				// Same contract as the Claude settings link above: --force
				// is a deliberate replace, so preserve an unmanaged user
				// ~/.cursor/hooks.json as a sidecar backup and propagate any
				// error instead of printing false success.
				if err := links.HardlinkReplacing(cursorHooksSrc, cursorHooksDst, sidecarBackupFile); err != nil {
					return fmt.Errorf("linking %s: %w", cursorHooksDst, err)
				}
				ui.Bullet("ok", "Created Cursor global hooks hardlink")
			} else {
				ui.Bullet("skip", "~/.cursor/hooks.json exists (use --force to replace)")
			}
		}
	}

	// State dir
	os.MkdirAll(config.AgentsStateDir(), 0755)
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

func scaffoldWorkflowAssets(agentsHome string) error {
	if err := osMkdirAll(config.AgentsContextDir(), 0755); err != nil {
		return err
	}
	return scaffoldhooks.CopyMissingGlobalBundles(filepath.Join(agentsHome, "hooks", "global"))
}
