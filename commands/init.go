package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/links"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize ~/.agents/ directory structure",
		Long: `Creates the ~/.agents/ directory structure with starter templates.
Safe to run multiple times - existing files are preserved unless --force.`,
		RunE: runInit,
	}
	return cmd
}

func runInit(cmd *cobra.Command, args []string) error {
	agentsHome := config.AgentsHome()

	ui.Header("dot-agents init")

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
		filepath.Join(agentsHome, "skills", "global", "agent-start"),
		filepath.Join(agentsHome, "skills", "global", "agent-handoff"),
		filepath.Join(agentsHome, "skills", "global", "self-review"),
		filepath.Join(agentsHome, "agents", "global"),
		filepath.Join(agentsHome, "hooks", "global"),
		filepath.Join(agentsHome, "scripts"),
		filepath.Join(agentsHome, "local"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
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

	// Create starter rules.mdc
	rulesPath := filepath.Join(agentsHome, "rules", "global", "rules.mdc")
	if _, err := os.Stat(rulesPath); os.IsNotExist(err) {
		content := "---\ndescription: Global rules for all AI agents\n---\n\n# Global Rules\n\nAdd your global agent rules here.\n"
		os.WriteFile(rulesPath, []byte(content), 0644)
	}

	// Create starter claude-code.json settings
	claudeSettingsPath := filepath.Join(agentsHome, "settings", "global", "claude-code.json")
	if _, err := os.Stat(claudeSettingsPath); os.IsNotExist(err) {
		os.WriteFile(claudeSettingsPath, []byte("{}\n"), 0644)
	}

	// Create .gitignore
	gitignorePath := filepath.Join(agentsHome, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		content := "local/\n*.dot-agents-backup\n"
		os.WriteFile(gitignorePath, []byte(content), 0644)
	}

	// Create README.md
	readmePath := filepath.Join(agentsHome, "README.md")
	if _, err := os.Stat(readmePath); os.IsNotExist(err) {
		content := "# ~/.agents/\n\nManaged by [dot-agents](https://github.com/NikashPrakash/dot-agents).\n\n" +
			"## Stage 1 Canonical Buckets\n\n" +
			"- `rules/` for shared instructions\n" +
			"- `settings/` for platform settings and current Cursor ignore support\n" +
			"- `mcp/` for MCP configs\n" +
			"- `skills/` for canonical skills\n" +
			"- `agents/` for canonical agent definitions\n" +
			"- `hooks/` for canonical hook configs\n" +
			"- `resources/` for backups and restore state\n"
		os.WriteFile(readmePath, []byte(content), 0644)
	}

	ui.Bullet("ok", "Created template files")

	// Global Claude Code settings symlink — hooks/ takes priority over settings/
	claudeHooksSrc := filepath.Join(agentsHome, "hooks", "global", "claude-code.json")
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
			links.Symlink(claudeSettingsPath, claudeSettings)
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
				links.Hardlink(cursorHooksSrc, cursorHooksDst)
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
		"Add your first project: dot-agents add ~/path/to/project",
		"See the canonical layout: dot-agents explain structure",
		"Team member with manifest: dot-agents install  (instead of add)",
		"Set up git sync: dot-agents sync init",
		"Check health: dot-agents doctor",
	)
	return nil
}

// refreshMarkerContent generates the .agents-refresh marker file content.
func refreshMarkerContent(version, commit, describe string) []byte {
	now := time.Now().UTC().Format(time.RFC3339)
	content := "# dot-agents refresh marker — do not edit\n"
	content += "version=" + version + "\n"
	if commit != "" {
		content += "commit=" + commit + "\n"
	}
	if describe != "" {
		content += "describe=" + describe + "\n"
	}
	content += "refreshed_at=" + now + "\n"
	return []byte(content)
}
