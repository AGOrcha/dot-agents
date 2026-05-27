package mcp

import (
	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/spf13/cobra"
)

// NewCmd builds the `da mcp` command tree from injected dependencies.
// Mirrors agents.NewAgentsCmd / skills.NewSkillsCmd: helpers come from
// Deps so the subpackage stays independent of the parent commands/
// package. The cobra-tree assembly lives in cmdutil so mcp/settings/rules
// share one implementation; the per-resource description lives in a
// single CanonicalFileSpec built by canonicalSpec (see seams.go).
func NewCmd(deps Deps) *cobra.Command { return cmdutil.NewCanonicalResourceCmd(canonicalSpec(deps)) }

// NewListCmd / NewShowCmd / NewRemoveCmd build each subcommand
// standalone — retained for parity with the historical surface and for
// the parent shim's cross-cutting RunE coverage tests.
func NewListCmd(deps Deps) *cobra.Command { return cmdutil.NewCanonicalListCmd(canonicalSpec(deps)) }
func NewShowCmd(deps Deps) *cobra.Command { return cmdutil.NewCanonicalShowCmd(canonicalSpec(deps)) }
func NewRemoveCmd(deps Deps) *cobra.Command {
	return cmdutil.NewCanonicalRemoveCmd(canonicalSpec(deps))
}
