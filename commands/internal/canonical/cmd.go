// Package canonical owns the shared cobra-tree assembly used by the
// canonical resource subpackages (mcp, settings, rules). All three expose
// the same `da <kind> {list|show|remove}` shape, differing only in the
// strings on each command and the closure each subcommand's RunE invokes.
//
// NewCanonicalResourceCmd takes a ResourceCmdSpec — a description of those
// per-resource strings plus pre-bound Args/RunE for each subcommand — and
// returns the assembled cobra.Command tree. Pre-binding Args at the call
// site lets the leaves keep their own Deps shape (mcp.Deps uses
// MaxArgsWithHints, settings.Deps and rules.Deps use MaximumNArgsWithHints)
// without leaking that naming mismatch into this package.
//
// The only dependency beyond stdlib is github.com/spf13/cobra. Coverage
// gate: the package must hold >=95% line coverage; see cmd_test.go.
package canonical

import "github.com/spf13/cobra"

// SubCmdSpec describes one leaf subcommand under a canonical resource
// tree. Use/Short are required. Long and Example are optional — when
// empty they pass through to cobra unchanged. Args and RunE are bound
// by the caller so this package stays free of Deps coupling.
type SubCmdSpec struct {
	Use     string
	Short   string
	Long    string
	Example string
	Args    cobra.PositionalArgs
	RunE    func(cmd *cobra.Command, args []string) error
}

// ResourceCmdSpec describes a canonical `da <kind>` parent command plus
// its list/show/remove triplet. Mirrors the shape mcp/settings/rules
// previously hand-rolled in each cmd.go.
type ResourceCmdSpec struct {
	Use     string
	Short   string
	Long    string
	Example string
	List    SubCmdSpec
	Show    SubCmdSpec
	Remove  SubCmdSpec
}

// NewCanonicalResourceCmd assembles a canonical resource command tree
// from spec. The returned *cobra.Command has list/show/remove already
// attached as children.
func NewCanonicalResourceCmd(spec ResourceCmdSpec) *cobra.Command {
	cmd := &cobra.Command{
		Use:     spec.Use,
		Short:   spec.Short,
		Long:    spec.Long,
		Example: spec.Example,
	}
	cmd.AddCommand(NewSubCmd(spec.List))
	cmd.AddCommand(NewSubCmd(spec.Show))
	cmd.AddCommand(NewSubCmd(spec.Remove))
	return cmd
}

// NewSubCmd builds a single leaf subcommand from sub. Exported so the
// leaf packages can expose per-verb constructors (NewListCmd, NewShowCmd,
// NewRemoveCmd) as one-line delegations instead of re-implementing the
// cobra.Command{...} literal.
func NewSubCmd(sub SubCmdSpec) *cobra.Command {
	return &cobra.Command{
		Use:     sub.Use,
		Short:   sub.Short,
		Long:    sub.Long,
		Example: sub.Example,
		Args:    sub.Args,
		RunE:    sub.RunE,
	}
}
