package commands

import (
	"github.com/NikashPrakash/dot-agents/commands/skills"
	"github.com/spf13/cobra"
)

// skillsDeps builds the Deps struct passed to skills.NewSkillsCmd. Mirrors
// agentsDeps so the two extracted subcommand subpackages share the same wiring
// pattern.
func skillsDeps() skills.Deps {
	return skills.Deps{
		Flags: skills.GlobalFlags{
			Yes: Flags.Yes,
		},
		ErrorWithHints:        ErrorWithHints,
		UsageError:            UsageError,
		MaximumNArgsWithHints: MaximumNArgsWithHints,
		RangeArgsWithHints:    RangeArgsWithHints,
		ExactArgsWithHints:    ExactArgsWithHints,
	}
}

// NewSkillsCmd wires the skills subcommand tree. Thin shim preserved for
// source-compat with root.go and external callers.
func NewSkillsCmd() *cobra.Command {
	return skills.NewSkillsCmd(skillsDeps())
}

// createSkill is used by agentsrc mutation tests in this package. Mirrors the
// agents.createAgent shim.
func createSkill(name, scope string) error {
	return skills.CreateSkill(name, scope)
}
