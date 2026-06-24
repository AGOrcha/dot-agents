package rules

import (
	"github.com/spf13/cobra"
)

// GlobalFlags mirrors the subset of commands.Flags used by rules subcommands.
// Kept as a parallel type to commands.GlobalFlags so the rules subpackage has
// no import on the parent commands/ package. Mirrors agents.GlobalFlags /
// skills.GlobalFlags.
type GlobalFlags struct {
	DryRun bool
	Yes    bool
	Force  bool
}

// Deps carries UX helpers from the commands package without an import cycle.
// Mirrors agents.Deps / skills.Deps so the extracted subpackages share the
// same shape. Only fields actually consumed by rules subcommands are present.
type Deps struct {
	Flags                 GlobalFlags
	ErrorWithHints        func(message string, hints ...string) error
	UsageError            func(message string, hints ...string) error
	MaximumNArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
	ExactArgsWithHints    func(n int, hints ...string) cobra.PositionalArgs

	// IO is the filesystem + platform-lookup collaborator (seams.go). It is
	// the interface-DI seam that replaced the legacy osReadFile /
	// platform*CanonicalRuleFile package func-vars. Leave it nil in
	// production and the zero-value data path; the io() accessor falls back
	// to stdRuleIO{}. Tests inject a fake to fault the error branches.
	IO ruleIO
}

// io returns the injected ruleIO collaborator, defaulting to the real
// os/platform-backed stdRuleIO when Deps.IO is nil so production and
// zero-value Deps callers never have to wire it.
func (d Deps) io() ruleIO {
	if d.IO != nil {
		return d.IO
	}
	return stdRuleIO{}
}
