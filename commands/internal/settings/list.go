package settings

import (
	"fmt"
	"strings"

	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/spf13/cobra"
)

// canonicalSpec is the single source of truth for the `da settings`
// resource family. It populates both the data-layer fields
// cmdutil.RunCanonical{List,Show,Remove} consume and the CLI-surface
// fields cmdutil.NewCanonicalResourceCmd consumes. One struct literal
// per resource — there is no parallel ResourceCmdSpec to keep in sync.
//
// Returning a fresh spec per call keeps the closure-captured Deps
// explicit at each call site so list (which carries no Deps) and
// show/remove (which do) share the exact same projection of platform
// helpers.
func canonicalSpec(deps Deps) cmdutil.CanonicalFileSpec {
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
			sp, err := findSettingsSpec(deps, agentsHome, scope, name)
			if err != nil {
				return cmdutil.CanonicalFileEntry{}, err
			}
			return cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}, nil
		},
		EnsureScope: platform.EnsureUnderSettingsScopeTree,

		Use:   "settings",
		Short: "Inspect and manage canonical ~/.agents/settings files",
		Long: `Commands for platform settings files stored under ~/.agents/settings/<scope>/.

Scopes are either global (~/.agents/settings/global/) or a managed project name
(~/.agents/settings/<project>/), matching da status.

Files include JSON/TOML/YAML configs (e.g. cursor.json, claude-code.json) and
cursorignore. These are wired by add, import, refresh, install, and remove.
Prefer editing canonical paths here, then run refresh or install.`,
		Example: cmdutil.CanonicalCmdExampleBlock(
			"  da settings list",
			"  da settings list my-app",
			"  da settings show global cursor.json",
			"  da settings remove proj cursorignore",
		),
		ListSub: cmdutil.SubCmdStrings{
			Use:   "list [scope]",
			Short: "List canonical settings files for a scope",
			Example: cmdutil.CanonicalCmdExampleBlock(
				"  da settings list",
				"  da settings list billing-api",
			),
		},
		ListArgs: maxArgs(deps, 1, "Optionally pass a project scope (or `global`) to inspect that settings tree."),
		ListRun:  func(scope string) error { return RunList(scope) },
		ShowSub: cmdutil.SubCmdStrings{
			Use:   "show <scope> <name>",
			Short: "Show metadata for one settings file under ~/.agents/settings/",
		},
		ShowArgs: exactArgs(deps, 2, "`scope` is `global` or a managed project name; `name` is the file (e.g. cursor.json) or stem."),
		ShowRun:  func(scope, name string) error { return RunShow(deps, scope, name) },
		RemoveSub: cmdutil.SubCmdStrings{
			Use:   "remove <scope> <name>",
			Short: "Remove a settings file from ~/.agents/settings/ (canonical storage only)",
			Long: `Deletes the file from managed settings storage only (not repo links). After removal,
run da refresh or install for the relevant project so platform settings
links stay consistent.`,
		},
		RemoveArgs: exactArgs(deps, 2, "`scope` is `global` or a managed project name; `name` matches list/show."),
		RemoveRun:  func(scope, name string) error { return RunRemove(deps, scope, name) },
	}
}

// maxArgs / exactArgs guard against the zero-value Deps used by the
// data-layer RunList path. The CLI wiring in NewCmd always supplies real
// helpers via Deps; the data path only needs the data-layer spec fields
// and never invokes Args, so nil-returning fallbacks are safe.
func maxArgs(deps Deps, n int, hints ...string) cobra.PositionalArgs {
	if deps.MaximumNArgsWithHints == nil {
		return nil
	}
	return deps.MaximumNArgsWithHints(n, hints...)
}

func exactArgs(deps Deps, n int, hints ...string) cobra.PositionalArgs {
	if deps.ExactArgsWithHints == nil {
		return nil
	}
	return deps.ExactArgsWithHints(n, hints...)
}

// RunList prints canonical settings entries for a scope. No Deps needed because
// no hint-driven errors fire in the list path — list surfaces only info
// messages via cmdutil.
func RunList(scope string) error {
	return cmdutil.RunCanonicalList(scope, canonicalSpec(Deps{}))
}

// findSettingsSpec looks up a settings file by basename or stem and wraps
// not-found errors with a hint pointing at `settings list`. Kept package-
// private because TestFindSettingsSpec_* (moved here from the root settings
// tests in t5 PR #49) call it directly.
func findSettingsSpec(deps Deps, agentsHome, scope, name string) (*platform.SettingsFileSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, settingsUsageError(deps, "settings file name is empty", "Pass the file name or stem shown by `da settings list`.")
	}
	spec, err := platform.ResolveCanonicalSettingsFile(agentsHome, scope, name)
	if err != nil {
		return nil, settingsErrorWithHints(
			deps,
			fmt.Sprintf("settings file not found: %s / %s", scope, name),
			"Run `da settings list "+scope+"` to see available files.",
		)
	}
	return spec, nil
}

// settingsErrorWithHints routes through deps.ErrorWithHints when wired and
// otherwise falls back to a plain fmt.Errorf. Mirrors agents.agentUserError
// so test call sites that build Deps{} bare still see a sensible message.
func settingsErrorWithHints(deps Deps, message string, hints ...string) error {
	if deps.ErrorWithHints != nil {
		return deps.ErrorWithHints(message, hints...)
	}
	if len(hints) == 0 {
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("%s: %s", message, hints[0])
}

// settingsUsageError mirrors settingsErrorWithHints but routes through the
// usage-error helper so the wired CLIError has its UsageError flag set when
// available.
func settingsUsageError(deps Deps, message string, hints ...string) error {
	if deps.UsageError != nil {
		return deps.UsageError(message, hints...)
	}
	if len(hints) == 0 {
		return fmt.Errorf("%s", message)
	}
	return fmt.Errorf("%s: %s", message, hints[0])
}
