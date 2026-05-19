package commands

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

func NewRemoveCmd() *cobra.Command {
	var cleanDirs bool

	cmd := &cobra.Command{
		Use:   "remove <project>",
		Short: "Remove a project from da management",
		Long: `Unregisters a project from da and removes platform links (rules, hooks,
MCP, settings, and other managed outputs) the same way install/refresh created them.

With --clean, also removes project directories from ~/.agents/.`,
		Example: ExampleBlock(
			"  da remove billing-api",
			"  da remove billing-api --clean",
			"  da status",
		),
		Args: ExactArgsWithHints(1, "Use the managed project name from `da status`."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRemove(args[0], cleanDirs)
		},
	}
	cmd.Flags().BoolVar(&cleanDirs, "clean", false, "Also remove project directories from ~/.agents/")
	return cmd
}

func runRemove(projectName string, cleanDirs bool) error {
	cfg, err := configLoad()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	projectPath := cfg.GetProjectPath(projectName)
	if projectPath == "" {
		return ErrorWithHints(
			fmt.Sprintf("project not found: %s", projectName),
			"Run `da status` to see registered projects.",
		)
	}

	displayPath := config.DisplayPath(projectPath)

	ui.Header("da remove")
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
		".cursor/hooks.json, .codex/hooks.json (managed links)",
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

	// Warn about git source cache if manifest has git sources
	if rc, err := config.LoadAgentsRC(projectPath); err == nil {
		for _, src := range rc.Sources {
			if src.Type == "git" && src.URL != "" {
				ui.Warn("Git source cache not cleaned automatically")
				fmt.Fprintf(os.Stdout, "  Cache: %s~/.cache/dot-agents/sources/%s\n", ui.Dim, ui.Reset)
				fmt.Fprintf(os.Stdout, "  To clean: %srm -rf ~/.cache/dot-agents/sources/%s\n\n", ui.Dim, ui.Reset)
				break
			}
		}
	}

	if cleanDirs {
		ui.WarnBox("Destructive Action",
			"The --clean flag will permanently delete:",
			"  ~/.agents/rules/"+projectName+"/",
			"  ~/.agents/settings/"+projectName+"/",
			"  ~/.agents/mcp/"+projectName+"/",
			"  ~/.agents/hooks/"+projectName+"/",
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

	var cleanupFailures []string
	if _, err := os.Stat(projectPath); err == nil {
		config.SetWindowsMirrorContext(projectPath)
		var installed []platform.Platform
		for _, p := range platform.All() {
			if p.IsInstalled() {
				installed = append(installed, p)
			}
		}
		if err := platform.RemoveSharedTargetPlan(projectName, projectPath, installed); err != nil {
			ui.Bullet("warn", fmt.Sprintf("shared targets: %v", err))
			cleanupFailures = append(cleanupFailures, fmt.Sprintf("shared targets: %v", err))
		}
		for _, p := range platform.All() {
			if err := p.RemoveLinks(projectName, projectPath); err != nil {
				ui.Bullet("warn", fmt.Sprintf("%s: %v", p.DisplayName(), err))
				cleanupFailures = append(cleanupFailures, fmt.Sprintf("%s: %v", p.DisplayName(), err))
			} else {
				ui.Bullet("ok", p.DisplayName()+" links removed")
			}
		}
	} else {
		ui.Bullet("skip", "Skipped link removal (directory not found)")
	}

	// Managed cleanup failed: removing the registration now would orphan the
	// still-present managed outputs with no record to retry against. Preserve
	// the registration and fail so a re-run can finish the cleanup.
	if len(cleanupFailures) > 0 {
		return ErrorWithHints(
			fmt.Sprintf("remove incomplete for '%s': %s", projectName, strings.Join(cleanupFailures, "; ")),
			"The project registration was PRESERVED so cleanup can be retried. "+
				"Resolve the warnings above, then re-run `da remove "+projectName+"`.",
		)
	}

	// Canonical-dir cleanup runs BEFORE unregistering: a permission/locked-file
	// failure here must leave the project registered so a re-run still has a
	// handle to retry against. Unregistering first would orphan stale canonical
	// data with no recovery path while falsely reporting "removed completely".
	if cleanDirs {
		ui.Step("Cleaning project directories...")
		if err := removeProjectDirs(projectName); err != nil {
			return ErrorWithHints(
				fmt.Sprintf("remove incomplete for '%s': could not clean project directories: %v", projectName, err),
				"The project registration was PRESERVED so cleanup can be retried. "+
					"Resolve the errors above (permissions, locked files), then re-run "+
					"`da remove "+projectName+" --clean`.",
			)
		}
		ui.Bullet("ok", "Removed project directories")
	}

	cfg.RemoveProject(projectName)
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	ui.Bullet("ok", "Unregistered from config.json")

	if cleanDirs {
		ui.SuccessBox(fmt.Sprintf("Project '%s' removed completely!", projectName),
			"Verify removal: da status",
		)
	} else {
		ui.SuccessBox(fmt.Sprintf("Project '%s' unlinked successfully!", projectName),
			"Verify removal: da status",
			"To also remove project directories: da remove "+projectName+" --clean",
		)
	}
	return nil
}

// removeProjectDirs deletes the project's canonical directories under
// ~/.agents/. It aggregates and returns every removal failure (errors.Join)
// rather than discarding them: a swallowed permission/locked-file error left
// `da remove --clean` reporting complete removal while stale canonical data
// remained on disk. A not-exist error is the expected "nothing to clean"
// case and is the only error swallowed.
func removeProjectDirs(project string) error {
	agentsHome := config.AgentsHome()
	dirs := []string{
		agentsHome + "/rules/" + project,
		agentsHome + "/settings/" + project,
		agentsHome + "/mcp/" + project,
		// The hooks scope dir is resolved through the shared canonical
		// helper so `da remove --clean` and `da hooks remove` cannot
		// disagree about where ~/.agents/hooks/<scope> lives. Behavior is
		// unchanged: the whole scope subtree is still removed here.
		config.HooksScopeDir(project),
		agentsHome + "/skills/" + project,
		agentsHome + "/agents/" + project,
	}
	var errs []error
	for _, d := range dirs {
		if err := osRemoveAll(d); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("%s: %w", d, err))
		}
	}
	return errors.Join(errs...)
}
