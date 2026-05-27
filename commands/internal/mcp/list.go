package mcp

import "github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"

// RunList prints canonical MCP files under ~/.agents/mcp/<scope>/.
// Does not invoke the Resolve callback so a zero-value Deps is
// sufficient for the spec construction.
func RunList(scope string) error {
	return cmdutil.RunCanonicalList(scope, canonicalSpec(Deps{}))
}
