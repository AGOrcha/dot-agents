package commands

import (
	wf "github.com/NikashPrakash/dot-agents/commands/workflow"
	"github.com/spf13/cobra"
)

// errNoWorkflowProject is returned when workflow commands cannot resolve a project context.
// Kept in package commands so ux enrichment and tests can use errors.Is with a stable sentinel.
var errNoWorkflowProject = &CLIError{
	Message: "workflow commands must run inside a project directory",
	Hints: []string{
		"Run workflow commands from a repository that already contains `.agents/` or `.agentsrc.json`.",
		"If this repo is not managed yet, start with `dot-agents add .` or `dot-agents install --generate`.",
	},
}

func workflowBridgeDeps() wf.Deps {
	return wf.Deps{
		ErrNoProject: errNoWorkflowProject,
		Flags: wf.GlobalFlags{
			JSON: func() bool { return Flags.JSON },
			Yes:  func() bool { return Flags.Yes },
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

// NewWorkflowCmd builds the `dot-agents workflow` command tree.
func NewWorkflowCmd() *cobra.Command {
	return wf.NewCmd(workflowBridgeDeps())
}
