package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

type settingsDeps struct {
	Flags              rulesGlobalFlags
	maxArgsWithHints   func(n int, hints ...string) cobra.PositionalArgs
	exactArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
}

func settingsCommandDeps() settingsDeps {
	return settingsDeps{
		Flags: rulesGlobalFlags{
			DryRun: Flags.DryRun,
			Yes:    Flags.Yes,
			Force:  Flags.Force,
		},
		maxArgsWithHints:   MaximumNArgsWithHints,
		exactArgsWithHints: ExactArgsWithHints,
	}
}

// NewSettingsCmd builds the `dot-agents settings` command tree.
func NewSettingsCmd() *cobra.Command {
	deps := settingsCommandDeps()
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Inspect and manage canonical ~/.agents/settings files",
		Long: `Commands for platform settings files stored under ~/.agents/settings/<scope>/.

Scopes are either global (~/.agents/settings/global/) or a managed project name
(~/.agents/settings/<project>/), matching dot-agents status.

Files include JSON/TOML/YAML configs (e.g. cursor.json, claude-code.json) and
cursorignore. These are wired by add, import, refresh, install, and remove.
Prefer editing canonical paths here, then run refresh or install.`,
		Example: rulesExampleBlock(
			"  dot-agents settings list",
			"  dot-agents settings list my-app",
			"  dot-agents settings show global cursor.json",
			"  dot-agents settings remove proj cursorignore",
		),
	}
	cmd.AddCommand(newSettingsListCmd(deps))
	cmd.AddCommand(newSettingsShowCmd(deps))
	cmd.AddCommand(newSettingsRemoveCmd(deps))
	return cmd
}

func newSettingsListCmd(deps settingsDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "list [scope]",
		Short: "List canonical settings files for a scope",
		Example: rulesExampleBlock(
			"  dot-agents settings list",
			"  dot-agents settings list billing-api",
		),
		Args: deps.maxArgsWithHints(1, "Optionally pass a project scope (or `global`) to inspect that settings tree."),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return runSettingsList(scope)
		},
	}
}

func newSettingsShowCmd(deps settingsDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <scope> <name>",
		Short: "Show metadata for one settings file under ~/.agents/settings/",
		Args:  deps.exactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` is the file (e.g. cursor.json) or stem."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSettingsShow(args[0], args[1])
		},
	}
}

func newSettingsRemoveCmd(deps settingsDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <scope> <name>",
		Short: "Remove a settings file from ~/.agents/settings/ (canonical storage only)",
		Long: `Deletes the file from managed settings storage only (not repo links). After removal,
run dot-agents refresh or install for the relevant project so platform settings
links stay consistent.`,
		Args: deps.exactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` matches list/show."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSettingsRemove(deps, args[0], args[1])
		},
	}
}

func runSettingsList(scope string) error {
	agentsHome := config.AgentsHome()
	specs, err := platform.ListCanonicalSettingsFiles(agentsHome, scope)
	if err != nil {
		if os.IsNotExist(err) {
			ui.Info("No ~/.agents/settings/" + scope + "/ directory yet (no canonical settings files for this scope).")
			return nil
		}
		return err
	}
	if len(specs) == 0 {
		ui.Info("No settings files under ~/.agents/settings/" + scope + "/")
		return nil
	}
	ui.Header("Settings (" + scope + ")")
	for _, spec := range specs {
		fmt.Fprintf(os.Stdout, "\n  %s%s%s\n", ui.Cyan, spec.BaseName, ui.Reset)
		fmt.Fprintf(os.Stdout, "    %spath:%s %s\n", ui.Dim, ui.Reset, config.DisplayPath(spec.SourcePath))
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runSettingsShow(scope, name string) error {
	agentsHome := config.AgentsHome()
	spec, err := findSettingsSpec(agentsHome, scope, name)
	if err != nil {
		return err
	}
	info, statErr := os.Stat(spec.SourcePath)
	ui.Header("Settings " + spec.BaseName + " (" + scope + ")")
	fmt.Fprintf(os.Stdout, "  %spath:%s %s\n", ui.Dim, ui.Reset, config.DisplayPath(spec.SourcePath))
	if statErr == nil {
		fmt.Fprintf(os.Stdout, "  %ssize:%s %d bytes\n", ui.Dim, ui.Reset, info.Size())
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runSettingsRemove(deps settingsDeps, scope, name string) error {
	agentsHome := config.AgentsHome()
	spec, err := findSettingsSpec(agentsHome, scope, name)
	if err != nil {
		return err
	}
	if err := platform.EnsureUnderSettingsScopeTree(agentsHome, scope, spec.SourcePath); err != nil {
		return err
	}

	ui.Header("dot-agents settings remove")
	fmt.Fprintf(os.Stdout, "Remove settings file %q from scope %s\n", name, ui.BoldText(scope))
	fmt.Fprintf(os.Stdout, "  %s\n", config.DisplayPath(spec.SourcePath))

	if deps.Flags.DryRun {
		fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
		return nil
	}
	if !deps.Flags.Yes && !deps.Flags.Force {
		if !ui.Confirm("Remove this file from ~/.agents/settings/?", false) {
			ui.Info("Cancelled.")
			return nil
		}
	}

	if err := os.Remove(spec.SourcePath); err != nil {
		return fmt.Errorf("removing settings file: %w", err)
	}
	ui.Success(fmt.Sprintf("Removed settings file %q from scope %s.", spec.BaseName, scope))
	return nil
}

func findSettingsSpec(agentsHome, scope, name string) (*platform.SettingsFileSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, UsageError("settings file name is empty", "Pass the file name or stem shown by `dot-agents settings list`.")
	}
	spec, err := platform.ResolveCanonicalSettingsFile(agentsHome, scope, name)
	if err != nil {
		return nil, ErrorWithHints(
			fmt.Sprintf("settings file not found: %s / %s", scope, name),
			"Run `dot-agents settings list "+scope+"` to see available files.",
		)
	}
	return spec, nil
}
