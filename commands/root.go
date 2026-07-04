package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/AGOrcha/dot-agents/commands/config"
	"github.com/AGOrcha/dot-agents/commands/eval"
	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/AGOrcha/dot-agents/commands/internal/lifecycle"
	"github.com/AGOrcha/dot-agents/commands/internal/mcp"
	"github.com/AGOrcha/dot-agents/commands/internal/rules"
	"github.com/AGOrcha/dot-agents/commands/internal/settings"
	"github.com/spf13/cobra"
)

// rootConfigDeps builds the config.Deps passed to config.NewConfigCmd. The
// config subtree threads the hint-emitting UX helpers plus the resolved global
// --json/--dry-run getters from root. The mutating `da config sync` consumes
// DryRun — it short-circuits the force re-resolve + lock rewrite in preview
// mode; the read-only siblings (explain/verify/lint) ignore it.
func rootConfigDeps() config.Deps {
	return config.Deps{
		ErrorWithHints:        ErrorWithHints,
		UsageError:            UsageError,
		MaximumNArgsWithHints: MaximumNArgsWithHints,
		ExactArgsWithHints:    ExactArgsWithHints,
		JSON:                  func() bool { return Flags.JSON },
		DryRun:                func() bool { return Flags.DryRun },
	}
}

// rootEvalCmd builds the `da eval` group, wiring the gen/run/ls RunE handlers
// here in package commands. The handlers are defined as named functions (below)
// rather than injected closures so internal/globalflagcov can statically trace
// their global-flag reads: that analyzer indexes a fixed set of command
// packages that excludes commands/eval, so a RunE defined there resolves to an
// "unresolved closure". Reading Flags.JSON directly in runEvalRun/runEvalLs
// keeps the --json coverage analysable.
func rootEvalCmd() *cobra.Command {
	return eval.NewCmd(runEvalGen, runEvalRun, runEvalLs)
}

// runEvalGen is the `da eval gen` RunE handler; it threads the global --json
// flag so gen emits the TaskSpec as structured JSON (YAML otherwise). The read
// happens here so internal/globalflagcov can statically trace it (see
// rootEvalCmd).
func runEvalGen(cmd *cobra.Command, _ []string) error {
	return eval.RunGen(cmd, Flags.JSON)
}

// runEvalRun is the `da eval run` RunE handler; it threads the global --json and
// -n/--dry-run flags into the run pipeline. --dry-run makes the run resolve and
// preview the task without invoking the agent, provisioning a sandbox, or
// writing a run dir. Both reads happen here so internal/globalflagcov can
// statically trace them (see rootEvalCmd).
func runEvalRun(cmd *cobra.Command, _ []string) error {
	return eval.RunEval(cmd, Flags.JSON, Flags.DryRun)
}

// runEvalLs is the `da eval ls` RunE handler; it threads the global --json flag
// into the run listing.
func runEvalLs(cmd *cobra.Command, _ []string) error {
	return eval.RunLs(cmd, Flags.JSON)
}

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

// buildLifecycleDeps builds the lifecycle.Deps passed to lifecycle.NewInstallCmd /
// NewDoctorCmd / NewInitCmd / NewStatusCmd. Inlined here after t13b deleted the
// per-command shim files (install.go/doctor.go/init.go/status.go) along with
// their lifecycleDeps / lifecycleDoctorDeps / lifecycleStatusDeps helpers.
//
// FlagsFn is a closure (not a value snapshot) so applyDepsToGlobals — invoked
// by NewInstallCmd / NewDoctorCmd / NewInitCmd's RunE wrapper — observes
// commands.Flags at RunE time (cobra parses persistent flags AFTER the
// constructor returns, so a value snapshot taken at construction would be
// stale). NewStatusCmd does not call applyDepsToGlobals — its moved helpers
// do not read the lifecycle package globals — but receiving the same Deps
// shape keeps the per-constructor wiring uniform.
//
// Version/Commit/Describe carry build-info into the install pipeline's
// finalizeInstall helper. RunRefresh is intentionally nil here: only
// lifecycle.NewRefreshCmd reads it, and that constructor is wired separately
// through commands/refresh.go's shim until t07 collapses the refresh body
// into lifecycle.
func buildLifecycleDeps() lifecycle.Deps {
	return lifecycle.Deps{
		Flags: lifecycle.GlobalFlags{
			DryRun:  Flags.DryRun,
			Force:   Flags.Force,
			Verbose: Flags.Verbose,
			Yes:     Flags.Yes,
		},
		FlagsFn: func() lifecycle.GlobalFlags {
			return lifecycle.GlobalFlags{
				DryRun:  Flags.DryRun,
				Force:   Flags.Force,
				Verbose: Flags.Verbose,
				Yes:     Flags.Yes,
			}
		},
		ErrorWithHints:        ErrorWithHints,
		UsageError:            UsageError,
		MaximumNArgsWithHints: MaximumNArgsWithHints,
		RangeArgsWithHints:    RangeArgsWithHints,
		ExactArgsWithHints:    ExactArgsWithHints,
		NoArgsWithHints:       NoArgsWithHints,
		ExampleBlock:          ExampleBlock,
		Version:               Version,
		Commit:                Commit,
		Describe:              Describe,
	}
}

// NewRootCommand builds the root cobra command with persistent global flags and all
// subcommands. It mirrors cmd/da/main.go so tooling (e.g. global flag coverage)
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

	root.AddCommand(lifecycle.NewInitCmd(buildLifecycleDeps()))
	root.AddCommand(NewAddCmd())
	root.AddCommand(NewRemoveCmd())
	root.AddCommand(NewRefreshCmd())
	root.AddCommand(NewImportCmd())
	root.AddCommand(lifecycle.NewStatusCmd(buildLifecycleDeps(), func() bool { return Flags.JSON }))
	root.AddCommand(lifecycle.NewDoctorCmd(buildLifecycleDeps()))
	root.AddCommand(NewSkillsCmd())
	root.AddCommand(NewAgentsCmd())
	root.AddCommand(NewHooksCmd())
	root.AddCommand(rules.NewRulesCmd(rootRulesDeps()))
	root.AddCommand(mcp.NewCmd(rootMCPDeps()))
	root.AddCommand(settings.NewCmd(rootSettingsDeps()))
	root.AddCommand(config.NewConfigCmd(rootConfigDeps()))
	root.AddCommand(withReviewAdmin(NewReviewCmd()))
	root.AddCommand(NewSyncCmd())
	root.AddCommand(NewExplainCmd())
	root.AddCommand(lifecycle.NewInstallCmd(buildLifecycleDeps()))
	root.AddCommand(NewSessionCmd())
	root.AddCommand(NewWorkflowCmd())
	root.AddCommand(NewKGCmd())
	root.AddCommand(NewScoreCmd())
	root.AddCommand(rootEvalCmd())
	root.AddCommand(NewRunCmd())

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
