package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// NewRootCommand builds the root cobra command with persistent global flags and all
// subcommands. It mirrors cmd/dot-agents/main.go so tooling (e.g. global flag coverage)
// can inspect the live command tree without importing package main.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "dot-agents",
		Short: "Manage AI agent configurations across projects",
		Long: "dot-agents keeps your AI agent rules, settings, and skills in a single\n" +
			"~/.agents/ directory and links them into each project you work on.\n\n" +
			"It supports Cursor, Claude Code, Codex CLI, OpenCode, and GitHub Copilot.\n\n" +
			"Use it to bootstrap shared agent configuration, keep project links healthy,\n" +
			"capture workflow state, and generate reproducible .agentsrc.json manifests\n" +
			"that both humans and AI agents can follow.",
		Example: strings.Join([]string{
			"  dot-agents init",
			"  dot-agents add .",
			"  dot-agents status --audit",
			"  dot-agents workflow orient",
			"  dot-agents install --generate",
			"  dot-agents sync status",
		}, "\n"),
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().BoolVarP(&Flags.DryRun, "dry-run", "n", false, "Show what would be done without making changes")
	root.PersistentFlags().BoolVarP(&Flags.Force, "force", "f", false, "Overwrite existing configurations")
	root.PersistentFlags().BoolVarP(&Flags.Verbose, "verbose", "v", false, "Show detailed output")
	root.PersistentFlags().BoolVarP(&Flags.Yes, "yes", "y", false, "Auto-confirm prompts")
	root.PersistentFlags().BoolVar(&Flags.JSON, "json", false, "Output as JSON")

	root.AddCommand(NewInitCmd())
	root.AddCommand(NewAddCmd())
	root.AddCommand(NewRemoveCmd())
	root.AddCommand(NewRefreshCmd())
	root.AddCommand(NewImportCmd())
	root.AddCommand(NewStatusCmd())
	root.AddCommand(NewDoctorCmd())
	root.AddCommand(NewSkillsCmd())
	root.AddCommand(NewAgentsCmd())
	root.AddCommand(NewHooksCmd())
	root.AddCommand(NewWorkflowCmd())
	root.AddCommand(NewReviewCmd())
	root.AddCommand(NewSyncCmd())
	root.AddCommand(NewExplainCmd())
	root.AddCommand(NewInstallCmd())
	root.AddCommand(NewKGCmd())

	root.SetErr(os.Stderr)
	root.SetOut(os.Stdout)
	ConfigureRootCommandUX(root)

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return nil
	}
	cobra.EnableCommandSorting = false

	root.SetVersionTemplate(fmt.Sprintf("dot-agents version %s\n", Version))

	return root
}
