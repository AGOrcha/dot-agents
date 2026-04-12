package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/NikashPrakash/dot-agents/commands"
	"github.com/spf13/cobra"
)

func main() {
	root := buildRoot()
	if err := root.Execute(); err != nil {
		commands.RenderCommandError(os.Stderr, root, os.Args[1:], err)
		os.Exit(1)
	}
}

func buildRoot() *cobra.Command {
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
		Version:       commands.Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// Global flags
	root.PersistentFlags().BoolVarP(&commands.Flags.DryRun, "dry-run", "n", false, "Show what would be done without making changes")
	root.PersistentFlags().BoolVarP(&commands.Flags.Force, "force", "f", false, "Overwrite existing configurations")
	root.PersistentFlags().BoolVarP(&commands.Flags.Verbose, "verbose", "v", false, "Show detailed output")
	root.PersistentFlags().BoolVarP(&commands.Flags.Yes, "yes", "y", false, "Auto-confirm prompts")
	root.PersistentFlags().BoolVar(&commands.Flags.JSON, "json", false, "Output as JSON")

	// Register commands
	root.AddCommand(commands.NewInitCmd())
	root.AddCommand(commands.NewAddCmd())
	root.AddCommand(commands.NewRemoveCmd())
	root.AddCommand(commands.NewRefreshCmd())
	root.AddCommand(commands.NewImportCmd())
	root.AddCommand(commands.NewStatusCmd())
	root.AddCommand(commands.NewDoctorCmd())
	root.AddCommand(commands.NewSkillsCmd())
	root.AddCommand(commands.NewAgentsCmd())
	root.AddCommand(commands.NewHooksCmd())
	root.AddCommand(commands.NewWorkflowCmd())
	root.AddCommand(commands.NewReviewCmd())
	root.AddCommand(commands.NewSyncCmd())
	root.AddCommand(commands.NewExplainCmd())
	root.AddCommand(commands.NewInstallCmd())
	root.AddCommand(commands.NewKGCmd())

	// Override Execute error handling for better UX
	root.SetErr(os.Stderr)
	root.SetOut(os.Stdout)
	commands.ConfigureRootCommandUX(root)

	// Custom error display
	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return nil
	}
	cobra.EnableCommandSorting = false

	// Custom usage template to show version
	root.SetVersionTemplate(fmt.Sprintf("dot-agents version %s\n", commands.Version))

	return root
}
