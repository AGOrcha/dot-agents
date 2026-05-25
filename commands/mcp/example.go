package mcp

import "github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"

// exampleBlock is a package-local alias for cmdutil.CanonicalCmdExampleBlock.
// Mirrors agents.exampleBlock / skills.exampleBlock so each resource
// subpackage uses the same identifier for its cobra Example blocks; the
// underlying join lives in cmdutil because the three canonical resource
// command trees (mcp/settings/rules) share it.
func exampleBlock(lines ...string) string {
	return cmdutil.CanonicalCmdExampleBlock(lines...)
}
