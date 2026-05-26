package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/NikashPrakash/dot-agents/commands/mcp"
	"github.com/NikashPrakash/dot-agents/commands/rules"
	"github.com/NikashPrakash/dot-agents/commands/settings"
	"github.com/spf13/cobra"
)

// rootMCPDeps builds the mcp.Deps passed to mcp.NewCmd. Inlined here after
// t13a deleted commands/mcp.go's mcpCommandDeps helper.
func rootMCPDeps() mcp.Deps {
	return mcp.Deps{
		Flags: cmdutil.CanonicalCmdFlags{
			DryRun: Flags.DryRun,
			Yes:    Flags.Yes,
			Force:  Flags.Force,
		},
		MaxArgsWithHints:   MaximumNArgsWithHints,
		ExactArgsWithHints: ExactArgsWithHints,
		ErrorWithHints:     ErrorWithHints,
		UsageError:         UsageError,
	}
}

// rootSettingsDeps builds the settings.Deps passed to settings.NewCmd.
// Inlined here after t13a deleted commands/settings.go's
// settingsCommandDeps + toSettingsSubpackageDeps helpers.
func rootSettingsDeps() settings.Deps {
	return settings.Deps{
		Flags: settings.GlobalFlags{
			DryRun: Flags.DryRun,
			Yes:    Flags.Yes,
			Force:  Flags.Force,
		},
		ErrorWithHints:        ErrorWithHints,
		UsageError:            UsageError,
		MaximumNArgsWithHints: MaximumNArgsWithHints,
		ExactArgsWithHints:    ExactArgsWithHints,
	}
}

// rootRulesDeps builds the rules.Deps passed to rules.NewRulesCmd. Inlined
// here after t13a deleted commands/rules.go's rulesCommandDeps helper.
func rootRulesDeps() rules.Deps {
	return rules.Deps{
		Flags: rules.GlobalFlags{
			DryRun: Flags.DryRun,
			Yes:    Flags.Yes,
			Force:  Flags.Force,
		},
		ErrorWithHints:        ErrorWithHints,
		UsageError:            UsageError,
		MaximumNArgsWithHints: MaximumNArgsWithHints,
		ExactArgsWithHints:    ExactArgsWithHints,
	}
}

// NewRootCommand builds the root cobra command with persistent global flags and all
// subcommands. It mirrors cmd/dot-agents/main.go so tooling (e.g. global flag coverage)
// can inspect the live command tree without importing package main.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "da",
		Short: "Manage AI agent configurations across projects",
		Long: "da keeps your AI agent rules, settings, and skills in a single\n" +
			"~/.agents/ directory and links them into each project you work on.\n\n" +
			"It supports Cursor, Claude Code, Codex CLI, OpenCode, and GitHub Copilot.\n\n" +
			"Use it to bootstrap shared agent configuration, keep project links healthy,\n" +
			"capture workflow state, and generate reproducible .agentsrc.json manifests\n" +
			"that both humans and AI agents can follow.\n\n" +
			"Managed hook/rules/MCP/settings command boundaries are documented in\n" +
			"docs/RESOURCE_COMMAND_CONTRACT.md (resource-command-parity plan).",
		Example: strings.Join([]string{
			"  da init",
			"  da add .",
			"  da status --audit",
			"  da install --generate",
			"  da sync status",
			"  da workflow orient",
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
	root.AddCommand(rules.NewRulesCmd(rootRulesDeps()))
	root.AddCommand(mcp.NewCmd(rootMCPDeps()))
	root.AddCommand(settings.NewCmd(rootSettingsDeps()))
	root.AddCommand(NewReviewCmd())
	root.AddCommand(NewSyncCmd())
	root.AddCommand(NewExplainCmd())
	root.AddCommand(NewInstallCmd())
	root.AddCommand(NewSessionCmd())
	root.AddCommand(NewWorkflowCmd())
	root.AddCommand(NewKGCmd())
	root.AddCommand(NewScoreCmd())

	root.SetErr(os.Stderr)
	root.SetOut(os.Stdout)
	ConfigureRootCommandUX(root)

	root.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		return nil
	}
	cobra.EnableCommandSorting = false

	root.SetVersionTemplate(fmt.Sprintf("da version %s\n", Version))

	return root
}
