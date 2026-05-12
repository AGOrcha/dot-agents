package commands

import (
	"fmt"
	"strings"

	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/NikashPrakash/dot-agents/internal/platform"
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

// NewSettingsCmd builds the `da settings` command tree.
func NewSettingsCmd() *cobra.Command {
	deps := settingsCommandDeps()
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Inspect and manage canonical ~/.agents/settings files",
		Long: `Commands for platform settings files stored under ~/.agents/settings/<scope>/.

Scopes are either global (~/.agents/settings/global/) or a managed project name
(~/.agents/settings/<project>/), matching da status.

Files include JSON/TOML/YAML configs (e.g. cursor.json, claude-code.json) and
cursorignore. These are wired by add, import, refresh, install, and remove.
Prefer editing canonical paths here, then run refresh or install.`,
		Example: rulesExampleBlock(
			"  da settings list",
			"  da settings list my-app",
			"  da settings show global cursor.json",
			"  da settings remove proj cursorignore",
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
			"  da settings list",
			"  da settings list billing-api",
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
run da refresh or install for the relevant project so platform settings
links stay consistent.`,
		Args: deps.exactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` matches list/show."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSettingsRemove(deps, args[0], args[1])
		},
	}
}

// settingsCanonicalSpec wires settings.go's runSettings* helpers into
// cmdutil.RunCanonical{List,Show,Remove}.
func settingsCanonicalSpec() cmdutil.CanonicalFileSpec {
	return cmdutil.CanonicalFileSpec{
		Kind:        "Settings",
		DirSegment:  "settings",
		SingularRem: "settings file",
		EmptyHint: func(scope string) string {
			return "No settings files under ~/.agents/settings/" + scope + "/"
		},
		List: func(agentsHome, scope string) ([]cmdutil.CanonicalFileEntry, error) {
			specs, err := platform.ListCanonicalSettingsFiles(agentsHome, scope)
			if err != nil {
				return nil, err
			}
			out := make([]cmdutil.CanonicalFileEntry, len(specs))
			for i, sp := range specs {
				out[i] = cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}
			}
			return out, nil
		},
		Resolve: func(agentsHome, scope, name string) (cmdutil.CanonicalFileEntry, error) {
			sp, err := findSettingsSpec(agentsHome, scope, name)
			if err != nil {
				return cmdutil.CanonicalFileEntry{}, err
			}
			return cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}, nil
		},
		EnsureScope: platform.EnsureUnderSettingsScopeTree,
	}
}

func runSettingsList(scope string) error {
	return cmdutil.RunCanonicalList(scope, settingsCanonicalSpec())
}

func runSettingsShow(scope, name string) error {
	return cmdutil.RunCanonicalShow(scope, name, settingsCanonicalSpec())
}

func runSettingsRemove(deps settingsDeps, scope, name string) error {
	return cmdutil.RunCanonicalRemove(cmdutil.RemoveDeps{
		DryRun: deps.Flags.DryRun, Yes: deps.Flags.Yes, Force: deps.Flags.Force,
	}, scope, name, settingsCanonicalSpec())
}

// findSettingsSpec looks up a settings file by basename or stem and
// wraps not-found errors with a hint pointing at `settings list`. Kept
// as a package-private helper because TestFindSettingsSpecNotFound
// calls it directly.
func findSettingsSpec(agentsHome, scope, name string) (*platform.SettingsFileSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, UsageError("settings file name is empty", "Pass the file name or stem shown by `da settings list`.")
	}
	spec, err := platform.ResolveCanonicalSettingsFile(agentsHome, scope, name)
	if err != nil {
		return nil, ErrorWithHints(
			fmt.Sprintf("settings file not found: %s / %s", scope, name),
			"Run `da settings list "+scope+"` to see available files.",
		)
	}
	return spec, nil
}
