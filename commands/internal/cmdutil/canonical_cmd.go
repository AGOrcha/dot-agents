package cmdutil

import "strings"

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
