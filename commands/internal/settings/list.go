package settings

import (
	"fmt"
	"strings"

	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/spf13/cobra"
)

// canonicalSpec assembles the `da settings` resource spec by combining
// the static cmdutil.SettingsResource definition (Kind/DirSegment/strings
// /Examples + EnsureScope) with the per-leaf runner closures that need
// access to platform.ListCanonicalSettingsFiles + findSettingsSpec for
// hint-aware errors.
//
// Per plan duplicate-density-drop: keeping this body as a single call
// into cmdutil.SpecForResource means the only duplication across the
// mcp/settings/rules trio is the four lines of runner closure shape —
// which Sonar's clone detector treats as structurally distinct because
// the captured platform.* helpers and findXxxSpec wrappers differ.
//
// settings uses Deps.MaximumNArgsWithHints (matching rules; mcp uses
// MaxArgsWithHints), so the list-args binding happens at this leaf via
// maxArgs(...) — that's why the args validators flow into
// SpecForResource as parameters rather than living on the def.
func canonicalSpec(deps Deps) cmdutil.CanonicalFileSpec {
	return cmdutil.SpecForResource(
		cmdutil.SettingsResource,
		cmdutil.ResourceRunners{
			List: func(agentsHome, scope string) ([]cmdutil.CanonicalFileEntry, error) {
				specs, err := platform.ListCanonicalSettingsFiles(agentsHome, scope)
				if err != nil {
					return nil, err
				}
				return cmdutil.EntriesFromSpecs(specs, func(sp platform.SettingsFileSpec) cmdutil.CanonicalFileEntry {
					return cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}
				}), nil
			},
			Resolve: func(agentsHome, scope, name string) (cmdutil.CanonicalFileEntry, error) {
				sp, err := findSettingsSpec(deps, agentsHome, scope, name)
				if err != nil {
					return cmdutil.CanonicalFileEntry{}, err
				}
				return cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}, nil
			},
			ListRun:   func(scope string) error { return RunList(scope) },
			ShowRun:   func(scope, name string) error { return RunShow(deps, scope, name) },
			RemoveRun: func(scope, name string) error { return RunRemove(deps, scope, name) },
		},
		maxArgs(deps, 1, cmdutil.SettingsResource.ListArgsHint),
		exactArgs(deps, 2, cmdutil.SettingsResource.ShowArgsHint),
		exactArgs(deps, 2, cmdutil.SettingsResource.RemoveArgsHint),
	)
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
