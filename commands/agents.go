package commands

import (
	"github.com/NikashPrakash/dot-agents/commands/agents"
	"github.com/spf13/cobra"
)

func agentsDeps() agents.Deps {
	return agents.Deps{
		Flags: agents.GlobalFlags{
			Yes: Flags.Yes,
		},
		ErrorWithHints:        ErrorWithHints,
		UsageError:            UsageError,
		MaximumNArgsWithHints: MaximumNArgsWithHints,
		RangeArgsWithHints:    RangeArgsWithHints,
		ExactArgsWithHints:    ExactArgsWithHints,
	}
}

// NewAgentsCmd wires the agents subcommand tree.
func NewAgentsCmd() *cobra.Command {
	return agents.NewAgentsCmd(agentsDeps())
}

// createAgent is used by agentsrc mutation tests in this package.
func createAgent(name, scope string) error {
	return agents.CreateAgent(name, scope)
}
