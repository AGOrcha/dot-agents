package settings

import (
	"fmt"
	"strings"

	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/NikashPrakash/dot-agents/internal/platform"
)

// canonicalSpec wires the settings runners into cmdutil.RunCanonical{List,Show,Remove}.
// Returning a fresh spec per call keeps the closure-captured Deps explicit at
// each call site so list (which carries no Deps) and show/remove (which do)
// share the exact same projection of platform helpers.
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
	}
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
