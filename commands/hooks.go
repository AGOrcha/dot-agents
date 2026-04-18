package commands

import (
	"github.com/NikashPrakash/dot-agents/commands/hooks"
	"github.com/spf13/cobra"
)

func hooksDeps() hooks.Deps {
	return hooks.Deps{
		Flags: hooks.GlobalFlags{
			DryRun: Flags.DryRun,
			Yes:    Flags.Yes,
			Force:  Flags.Force,
		},
		ErrorWithHints:     ErrorWithHints,
		UsageError:         UsageError,
		MaxArgsWithHints:   MaximumNArgsWithHints,
		ExactArgsWithHints: ExactArgsWithHints,
	}
}

func NewHooksCmd() *cobra.Command {
	return hooks.NewHooksCmd(hooksDeps())
}
