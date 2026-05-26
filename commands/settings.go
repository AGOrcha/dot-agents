package commands

import (
	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/NikashPrakash/dot-agents/commands/settings"
	"github.com/spf13/cobra"
)

// settingsDeps preserves the legacy package-private struct shape so the
// cross-cutting tests (coverage_test.go, resource_parity_test.go) keep
// compiling through t10b. Mirrors settings.Deps with the parent-package
// flag handling. t12 re-homes those tests; t13 deletes this shim.
type settingsDeps struct {
	Flags              cmdutil.CanonicalCmdFlags
	maxArgsWithHints   func(n int, hints ...string) cobra.PositionalArgs
	exactArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
}

func settingsCommandDeps() settingsDeps {
	return settingsDeps{
		Flags: cmdutil.CanonicalCmdFlags{
			DryRun: Flags.DryRun,
			Yes:    Flags.Yes,
			Force:  Flags.Force,
		},
		maxArgsWithHints:   MaximumNArgsWithHints,
		exactArgsWithHints: ExactArgsWithHints,
	}
}

// toSettingsSubpackageDeps adapts the legacy settingsDeps shim into the
// subpackage Deps struct expected by settings.New{List,Show,Remove}Cmd /
// settings.RunRemove. Bridges the parent-package commands.ErrorWithHints /
// UsageError into the subpackage's deps fields so error wrapping matches
// the pre-extraction behavior.
func toSettingsSubpackageDeps(d settingsDeps) settings.Deps {
	return settings.Deps{
		Flags: settings.GlobalFlags{
			DryRun: d.Flags.DryRun,
			Yes:    d.Flags.Yes,
			Force:  d.Flags.Force,
		},
		ErrorWithHints:        ErrorWithHints,
		UsageError:            UsageError,
		MaximumNArgsWithHints: d.maxArgsWithHints,
		ExactArgsWithHints:    d.exactArgsWithHints,
	}
}

// NewSettingsCmd wires the settings subcommand tree from the parent commands
// package. Thin shim preserved for source-compat with root.go and external
// callers; t13 deletes it once root.go switches to settings.NewCmd directly.
func NewSettingsCmd() *cobra.Command {
	return settings.NewCmd(toSettingsSubpackageDeps(settingsCommandDeps()))
}

// Legacy shims for tests that exercise individual settings subcommands by
// constructing them from the parent package. These delegate straight into
// the settings subpackage; t12 moves the tests that call them.

func newSettingsListCmd(deps settingsDeps) *cobra.Command {
	return settings.NewListCmd(toSettingsSubpackageDeps(deps))
}

func newSettingsShowCmd(deps settingsDeps) *cobra.Command {
	return settings.NewShowCmd(toSettingsSubpackageDeps(deps))
}

func newSettingsRemoveCmd(deps settingsDeps) *cobra.Command {
	return settings.NewRemoveCmd(toSettingsSubpackageDeps(deps))
}

func runSettingsList(scope string) error {
	return settings.RunList(scope)
}

func runSettingsShow(scope, name string) error {
	return settings.RunShow(toSettingsSubpackageDeps(settingsCommandDeps()), scope, name)
}

func runSettingsRemove(deps settingsDeps, scope, name string) error {
	return settings.RunRemove(toSettingsSubpackageDeps(deps), scope, name)
}
