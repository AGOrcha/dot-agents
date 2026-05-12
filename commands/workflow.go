package commands

import (
	wf "github.com/NikashPrakash/dot-agents/commands/workflow"
	"github.com/spf13/cobra"
)

func workflowBridgeDeps() wf.Deps {
	return wf.Deps{
		ErrNoProject: errNoWorkflowProject,
		Flags: wf.GlobalFlags{
			JSON:   func() bool { return Flags.JSON },
			Yes:    func() bool { return Flags.Yes },
			DryRun: func() bool { return Flags.DryRun },
		},
		ErrorWithHints:        ErrorWithHints,
		UsageError:            UsageError,
		NoArgsWithHints:       NoArgsWithHints,
		ExactArgsWithHints:    ExactArgsWithHints,
		MaximumNArgsWithHints: MaximumNArgsWithHints,
		ExampleBlock:          ExampleBlock,
	}
}

// WorkflowBridgeDeps wires workflow CLI handlers to commands UX helpers and global flags (also used by workflow package tests).
func WorkflowBridgeDeps() wf.Deps {
	return workflowBridgeDeps()
}

// NewWorkflowCmd builds the `da workflow` command tree.
func NewWorkflowCmd() *cobra.Command {
	return wf.NewCmd(workflowBridgeDeps())
}
