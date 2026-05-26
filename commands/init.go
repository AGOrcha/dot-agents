package commands

import (
	"github.com/NikashPrakash/dot-agents/commands/lifecycle"
	"github.com/spf13/cobra"
)

// NewInitCmd is the root-level wrapper for the init command. The
// implementation lives in commands/lifecycle/init.go as of t05; this
// shim builds the cobra.Command literal itself (so the RunE closure's
// runtime symbol resolves under `commands.NewInitCmd.func1` for
// globalflagcov's static index) and repoints lifecycle's flag/usage
// seams at the parent commands package (commands.Flags,
// commands.UsageError) so the user-visible hint formatting is preserved
// without lifecycle importing commands (which would form an import
// cycle). The KG MCP scaffolder is no longer threaded through a shim
// seam — t02b lifted the helper into lifecycle itself, so the lifecycle
// default (lifecycle.EnsureGlobalKGMCPConfigs) is already correct.
// The shim is deleted in t13 once commands/root.go switches to
// lifecycle's constructors directly and globalflagcov's package list is
// extended to include commands/lifecycle.
func NewInitCmd() *cobra.Command {
	lifecycle.SetInitFlags(
		func() bool { return Flags.Force },
		func() bool { return Flags.DryRun },
		func() bool { return Flags.Yes },
	)
	lifecycle.InitUsageErrorFn = UsageError

	cmd := &cobra.Command{
		Use:     lifecycle.InitCmdUse,
		Short:   lifecycle.InitCmdShort,
		Long:    lifecycle.InitCmdLong,
		Example: lifecycle.InitCmdExample,
		Args:    lifecycle.InitNoArgs(lifecycle.InitCmdNoArgsHint),
		RunE: func(cmd *cobra.Command, args []string) error {
			return lifecycle.RunInit(cmd, args)
		},
	}
	return cmd
}
