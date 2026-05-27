package cmdutil

import (
	"strings"

	"github.com/spf13/cobra"
)

// CanonicalCmdFlags captures the global flags relevant to canonical
// `da <kind>` subcommands (rules, mcp, settings, …). Lifted from
// commands/rules.go in plan root-command-decomposition t10pre so the
// three resource subpackages (rules, mcp, settings) can share a single
// definition once they split out of package commands.
type CanonicalCmdFlags struct {
	DryRun bool
	Yes    bool
	Force  bool
}

// CanonicalCmdExampleBlock joins example lines for canonical subcommand
// `Example:` fields. Shared across rules/mcp/settings command trees.
func CanonicalCmdExampleBlock(lines ...string) string {
	return strings.Join(lines, "\n")
}

// NewCanonicalResourceCmd assembles the parent `da <kind>` cobra tree
// from a CanonicalFileSpec — parent command plus its list/show/remove
// children — using the spec's CLI-surface fields (Use/Short/Long/
// Example and the per-verb SubCmdStrings + Args + Run). The returned
// command is ready to attach to the root command.
//
// This used to live in commands/internal/canonical; folding it back into
// cmdutil collapses the two-package split that had every resource family
// carrying a parallel ResourceCmdSpec literal alongside its existing
// CanonicalFileSpec. Now there is exactly one struct literal per
// resource (mcp/settings/rules).
func NewCanonicalResourceCmd(spec CanonicalFileSpec) *cobra.Command {
	cmd := &cobra.Command{
		Use:     spec.Use,
		Short:   spec.Short,
		Long:    spec.Long,
		Example: spec.Example,
	}
	cmd.AddCommand(NewCanonicalListCmd(spec))
	cmd.AddCommand(NewCanonicalShowCmd(spec))
	cmd.AddCommand(NewCanonicalRemoveCmd(spec))
	return cmd
}

// NewCanonicalListCmd builds the list leaf from spec. Exported so leaf
// packages can keep a one-liner standalone constructor (mcp.NewListCmd
// etc.) for cross-cutting coverage tests in the parent shim.
//
// Scope defaulting: when args is empty the runner receives "global",
// matching the behavior the three leaves previously open-coded.
func NewCanonicalListCmd(spec CanonicalFileSpec) *cobra.Command {
	return &cobra.Command{
		Use:     spec.ListSub.Use,
		Short:   spec.ListSub.Short,
		Long:    spec.ListSub.Long,
		Example: spec.ListSub.Example,
		Args:    spec.ListArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return spec.ListRun(scope)
		},
	}
}

// NewCanonicalShowCmd builds the show leaf from spec. Args validator
// must enforce exactly two positional arguments before this RunE fires.
func NewCanonicalShowCmd(spec CanonicalFileSpec) *cobra.Command {
	return &cobra.Command{
		Use:     spec.ShowSub.Use,
		Short:   spec.ShowSub.Short,
		Long:    spec.ShowSub.Long,
		Example: spec.ShowSub.Example,
		Args:    spec.ShowArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			return spec.ShowRun(args[0], args[1])
		},
	}
}

// NewCanonicalRemoveCmd builds the remove leaf from spec. Args
// validator must enforce exactly two positional arguments before this
// RunE fires.
func NewCanonicalRemoveCmd(spec CanonicalFileSpec) *cobra.Command {
	return &cobra.Command{
		Use:     spec.RemoveSub.Use,
		Short:   spec.RemoveSub.Short,
		Long:    spec.RemoveSub.Long,
		Example: spec.RemoveSub.Example,
		Args:    spec.RemoveArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			return spec.RemoveRun(args[0], args[1])
		},
	}
}
