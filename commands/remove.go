package commands

import (
	"fmt"
	"os"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewRemoveCmd() *cobra.Command {
	var cleanDirs bool

	cmd := &cobra.Command{
		Use:   "remove <project>",
		Short: "Remove a project from dot-agents management",
		Long: `Unregisters a project from dot-agents and removes config symlinks.

With --clean, also removes project directories from ~/.agents/.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(args[0], cleanDirs)
		},
	}
	cmd.Flags().BoolVar(&cleanDirs, "clean", false, "Also remove project directories from ~/.agents/")
	return cmd
}

func runRemove(projectName string, cleanDirs bool) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	projectPath := cfg.GetProjectPath(projectName)
	if projectPath == "" {
		return fmt.Errorf("project not found: %s\n\nRun 'dot-agents status' to see registered projects", projectName)
	}

	displayPath := config.DisplayPath(projectPath)

	ui.Header("dot-agents remove")
	fmt.Fprintf(os.Stdout, "Removing project: %s\n", ui.BoldText(projectName))
	fmt.Fprintf(os.Stdout, "Path: %s\n", ui.DimText(displayPath))

	ui.Step("Analyzing project...")
	if _, err := os.Stat(projectPath); err == nil {
		ui.Bullet("ok", "Project directory found")
	} else {
		ui.Bullet("warn", "Project directory not found (links may have been moved)")
	}

	ui.Step("The following will be removed:")
	ui.PreviewSection("From "+displayPath+":",
		".cursor/rules/global--*.mdc     (hard links)",
		".cursor/rules/"+projectName+"--*.mdc (hard links)",
		".claude/rules/"+projectName+"--*.md      (symlinks)",
		"AGENTS.md                       (symlink)",
		"opencode.json and .opencode/agent/* (symlinks)",
		".github/copilot-instructions.md (symlink)",
		".agents/skills/* and .github/agents/*.agent.md (symlinks)",
		".vscode/mcp.json and .claude/settings.local.json (symlinks)",
	)
	ui.PreviewSection("From ~/.agents/config.json:",
		"Project registration for '"+projectName+"'",
	)

	if cleanDirs {
		ui.WarnBox("Destructive Action",
			"The --clean flag will permanently delete:",
			"  ~/.agents/rules/"+projectName+"/",
			"  ~/.agents/settings/"+projectName+"/",
			"  ~/.agents/mcp/"+projectName+"/",
			"  ~/.agents/skills/"+projectName+"/",
			"  ~/.agents/agents/"+projectName+"/",
		)
	}

	if Flags.DryRun {
		fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
		return nil
	}

	if !Flags.Yes && !Flags.Force {
		if !ui.Confirm("Proceed with removal?", false) {
			ui.Info("Removal cancelled.")
			return nil
		}
	}

	ui.Step("Removing project...")

	if _, err := os.Stat(projectPath); err == nil {
		config.SetWindowsMirrorContext(projectPath)
		for _, p := range platform.All() {
			if err := p.RemoveLinks(projectName, projectPath); err != nil {
				ui.Bullet("warn", fmt.Sprintf("%s: %v", p.DisplayName(), err))
			} else {
				ui.Bullet("ok", p.DisplayName()+" links removed")
			}
		}
	} else {
		ui.Bullet("skip", "Skipped link removal (directory not found)")
	}

	cfg.RemoveProject(projectName)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	ui.Bullet("ok", "Unregistered from config.json")

	if cleanDirs {
		ui.Step("Cleaning project directories...")
		removeProjectDirs(projectName)
		ui.Bullet("ok", "Removed project directories")
	}

	if cleanDirs {
		ui.SuccessBox(fmt.Sprintf("Project '%s' removed completely!", projectName),
			"Verify removal: dot-agents status",
		)
	} else {
		ui.SuccessBox(fmt.Sprintf("Project '%s' unlinked successfully!", projectName),
			"Verify removal: dot-agents status",
			"To also remove project directories: dot-agents remove "+projectName+" --clean",
		)
	}
	return nil
}

func removeProjectDirs(project string) {
	agentsHome := config.AgentsHome()
	dirs := []string{
		agentsHome + "/rules/" + project,
		agentsHome + "/settings/" + project,
		agentsHome + "/mcp/" + project,
		agentsHome + "/skills/" + project,
		agentsHome + "/agents/" + project,
	}
	for _, d := range dirs {
		os.RemoveAll(d)
	}
}
