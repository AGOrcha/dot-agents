// Package mcp owns the `da mcp` subcommand tree. Resources live under
// ~/.agents/mcp/<scope>/ and feed the list/show/remove triplet shared by
// the rules and settings resource families via cmdutil.RunCanonical*.
//
// The package mirrors the file convention used by commands/agents and
// commands/skills: cmd.go assembles the cobra tree, list.go/show.go/
// remove.go hold each handler, seams.go owns the cmdutil.CanonicalFileSpec
// wiring + findMCPSpec lookup. Tests are split across mcp_test.go (the
// testutil-consuming flow tests, transplanted verbatim from
// commands/mcp_test.go), seams_test.go (findMCPSpec coverage), and
// coverage_test.go (RunE wiring coverage).
package mcp

import (
	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/spf13/cobra"
)

// Deps carries UX helpers from the parent commands/ package without an
// import cycle. Mirrors agents.Deps / skills.Deps but uses
// cmdutil.CanonicalCmdFlags directly because the three canonical resource
// subpackages (mcp, settings, rules) share that flag struct.
//
// MaxArgsWithHints and ExactArgsWithHints are exported so the parent
// shim in commands/mcp.go can populate them from
// commands.MaximumNArgsWithHints / commands.ExactArgsWithHints without
// reaching into unexported fields.
type Deps struct {
	Flags              cmdutil.CanonicalCmdFlags
	MaxArgsWithHints   func(n int, hints ...string) cobra.PositionalArgs
	ExactArgsWithHints func(n int, hints ...string) cobra.PositionalArgs

	// ErrorWithHints / UsageError let findMCPSpec produce the same
	// user-facing errors the parent commands.* helpers emit. Both
	// fields may be nil; findMCPSpec falls back to fmt.Errorf-style
	// errors in that case.
	ErrorWithHints func(message string, hints ...string) error
	UsageError     func(message string, hints ...string) error
}
