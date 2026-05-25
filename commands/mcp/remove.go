package mcp

import "github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"

// RunRemove removes an MCP file from ~/.agents/mcp/<scope>/, with the
// dry-run + confirm gates handled inside cmdutil.RunCanonicalRemove.
// deps.Flags carries the resolved global DryRun/Yes/Force; deps is also
// threaded into canonicalSpec so the Resolve callback's findMCPSpec
// errors can flow through the parent ErrorWithHints/UsageError helpers.
func RunRemove(deps Deps, scope, name string) error {
	return cmdutil.RunCanonicalRemove(cmdutil.RemoveDeps{
		DryRun: deps.Flags.DryRun,
		Yes:    deps.Flags.Yes,
		Force:  deps.Flags.Force,
	}, scope, name, canonicalSpec(deps))
}
