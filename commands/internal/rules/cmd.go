package rules

import (
	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/spf13/cobra"
)

// NewRulesCmd builds the `da rules` command tree from injected dependencies.
// Mirrors agents.NewAgentsCmd / skills.NewSkillsCmd: helpers come from Deps so
// the subpackage stays independent of the parent commands/ package. The
// cobra-tree assembly lives in cmdutil so mcp/settings/rules share one
// implementation; the per-resource description lives in a single
// CanonicalFileSpec built by canonicalSpec (see list.go).
func NewRulesCmd(deps Deps) *cobra.Command {
	return cmdutil.NewCanonicalResourceCmd(canonicalSpec(deps))
}

// NewListCmd / NewShowCmd / NewRemoveCmd build each subcommand standalone.
// Exported so the parent-package shim can wire them for cross-cutting
// RunE coverage tests.
func NewListCmd(deps Deps) *cobra.Command { return cmdutil.NewCanonicalListCmd(canonicalSpec(deps)) }
func NewShowCmd(deps Deps) *cobra.Command { return cmdutil.NewCanonicalShowCmd(canonicalSpec(deps)) }
func NewRemoveCmd(deps Deps) *cobra.Command {
	return cmdutil.NewCanonicalRemoveCmd(canonicalSpec(deps))
}
