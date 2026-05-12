package commands

import (
	"github.com/NikashPrakash/dot-agents/commands/kg"
	"github.com/spf13/cobra"
)

func kgDeps() kg.Deps {
	return kg.Deps{
		Flags: kg.GlobalFlags{
			JSON:   Flags.JSON,
			DryRun: Flags.DryRun,
		},
		ExampleBlock: ExampleBlock,
	}
}

// NewKGCmd wires the kg subcommand tree with root-level flags and UX helpers.
func NewKGCmd() *cobra.Command {
	return kg.NewKGCmd(kgDeps())
}
