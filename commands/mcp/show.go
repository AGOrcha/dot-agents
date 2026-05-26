package mcp

import "github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"

// RunShow prints metadata for one canonical MCP file. The resolve path
// goes through findMCPSpec; deps is threaded in so the parent commands
// shim can wire its commands.ErrorWithHints / commands.UsageError
// helpers and keep the user-facing CLIError shape unchanged.
//
// For ergonomic intra-package use the zero-value Deps is accepted
// (errors fall back to fmt.Errorf) — that path is exercised by
// mcp_test.go which only asserts on the wrapped message text.
func RunShow(deps Deps, scope, name string) error {
	return cmdutil.RunCanonicalShow(scope, name, canonicalSpec(deps))
}
