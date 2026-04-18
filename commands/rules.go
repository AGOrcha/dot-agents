package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/NikashPrakash/dot-agents/internal/ui"
	"github.com/spf13/cobra"
)

// rulesDeps carries UX helpers for the rules subcommand tree.
type rulesDeps struct {
	Flags              rulesGlobalFlags
	errorWithHints     func(message string, hints ...string) error
	usageError         func(message string, hints ...string) error
	maxArgsWithHints   func(n int, hints ...string) cobra.PositionalArgs
	exactArgsWithHints func(n int, hints ...string) cobra.PositionalArgs
}

type rulesGlobalFlags struct {
	DryRun bool
	Yes    bool
	Force  bool
}

func rulesCommandDeps() rulesDeps {
	return rulesDeps{
		Flags: rulesGlobalFlags{
			DryRun: Flags.DryRun,
			Yes:    Flags.Yes,
			Force:  Flags.Force,
		},
		errorWithHints:     ErrorWithHints,
		usageError:         UsageError,
		maxArgsWithHints:   MaximumNArgsWithHints,
		exactArgsWithHints: ExactArgsWithHints,
	}
}

func rulesExampleBlock(lines ...string) string {
	return strings.Join(lines, "\n")
}

// NewRulesCmd builds the `dot-agents rules` command tree.
func NewRulesCmd() *cobra.Command {
	deps := rulesCommandDeps()
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Inspect and manage canonical ~/.agents/rules files",
		Long: `Commands for rule files stored under ~/.agents/rules/<scope>/.

Scopes are either global (~/.agents/rules/global/) or a managed project name
(~/.agents/rules/<project>/), matching dot-agents status.

These files are what add, import, refresh, install, and remove wire into
Cursor, Claude Code, Codex, and Copilot projections. Prefer editing canonical
paths here, then run refresh or install for the project — do not hand-edit
platform copies unless you know they are unmanaged.`,
		Example: rulesExampleBlock(
			"  dot-agents rules list",
			"  dot-agents rules list my-app",
			"  dot-agents rules show global rules.mdc",
			"  dot-agents rules remove global old-rule.mdc",
		),
	}
	cmd.AddCommand(newRulesListCmd(deps))
	cmd.AddCommand(newRulesShowCmd(deps))
	cmd.AddCommand(newRulesRemoveCmd(deps))
	return cmd
}

func newRulesListCmd(deps rulesDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "list [scope]",
		Short: "List canonical rule files for a scope",
		Example: rulesExampleBlock(
			"  dot-agents rules list",
			"  dot-agents rules list billing-api",
		),
		Args: deps.maxArgsWithHints(1, "Optionally pass a project scope (or `global`) to inspect that rules tree."),
		RunE: func(cmd *cobra.Command, args []string) error {
			scope := "global"
			if len(args) > 0 {
				scope = args[0]
			}
			return runRulesList(scope)
		},
	}
}

func newRulesShowCmd(deps rulesDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "show <scope> <name>",
		Short: "Show metadata for one rule file under ~/.agents/rules/",
		Args:  deps.exactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` is the file (e.g. rules.mdc) or stem (rules)."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRulesShow(deps, args[0], args[1])
		},
	}
}

func newRulesRemoveCmd(deps rulesDeps) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <scope> <name>",
		Short: "Remove a rule file from ~/.agents/rules/ (canonical storage only)",
		Long: `Deletes the file from managed rule storage only (not repo links). After removal,
run dot-agents refresh or install for the relevant project so platform rule
links stay consistent.`,
		Args: deps.exactArgsWithHints(2, "`scope` is `global` or a managed project name; `name` matches list/show."),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRulesRemove(deps, args[0], args[1])
		},
	}
}

func runRulesList(scope string) error {
	agentsHome := config.AgentsHome()
	specs, err := platform.ListCanonicalRuleFiles(agentsHome, scope)
	if err != nil {
		if os.IsNotExist(err) {
			ui.Info("No ~/.agents/rules/" + scope + "/ directory yet (no canonical rule files for this scope).")
			return nil
		}
		return err
	}
	if len(specs) == 0 {
		ui.Info("No rule files (.mdc/.md/.txt) under ~/.agents/rules/" + scope + "/")
		return nil
	}
	ui.Header("Rules (" + scope + ")")
	for _, spec := range specs {
		fmt.Fprintf(os.Stdout, "\n  %s%s%s\n", ui.Cyan, spec.BaseName, ui.Reset)
		fmt.Fprintf(os.Stdout, "    %spath:%s %s\n", ui.Dim, ui.Reset, config.DisplayPath(spec.SourcePath))
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func runRulesShow(deps rulesDeps, scope, name string) error {
	agentsHome := config.AgentsHome()
	spec, err := findRuleSpec(deps, agentsHome, scope, name)
	if err != nil {
		return err
	}
	info, statErr := os.Stat(spec.SourcePath)
	ui.Header("Rule " + spec.BaseName + " (" + scope + ")")
	fmt.Fprintf(os.Stdout, "  %spath:%s %s\n", ui.Dim, ui.Reset, config.DisplayPath(spec.SourcePath))
	if statErr == nil {
		fmt.Fprintf(os.Stdout, "  %ssize:%s %d bytes\n", ui.Dim, ui.Reset, info.Size())
	}
	if desc := extractRuleFrontmatterDescription(spec.SourcePath); desc != "" {
		fmt.Fprintf(os.Stdout, "  %sdescription:%s %s\n", ui.Dim, ui.Reset, desc)
	}
	fmt.Fprintln(os.Stdout)
	return nil
}

func extractRuleFrontmatterDescription(path string) string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	s := string(data)
	rest := s
	switch {
	case strings.HasPrefix(s, "---\n"):
		rest = strings.TrimPrefix(s, "---\n")
	case strings.HasPrefix(s, "---\r\n"):
		rest = strings.TrimPrefix(s, "---\r\n")
	default:
		return ""
	}
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ""
	}
	fm := rest[:end]
	for _, line := range strings.Split(fm, "\n") {
		line = strings.TrimSpace(line)
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(key), "description") {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

func runRulesRemove(deps rulesDeps, scope, name string) error {
	agentsHome := config.AgentsHome()
	spec, err := findRuleSpec(deps, agentsHome, scope, name)
	if err != nil {
		return err
	}
	if err := platform.EnsureUnderRulesScopeTree(agentsHome, scope, spec.SourcePath); err != nil {
		return err
	}

	ui.Header("dot-agents rules remove")
	fmt.Fprintf(os.Stdout, "Remove rule %q from scope %s\n", name, ui.BoldText(scope))
	fmt.Fprintf(os.Stdout, "  %s\n", config.DisplayPath(spec.SourcePath))

	if deps.Flags.DryRun {
		fmt.Fprintln(os.Stdout, "\nDRY RUN - no changes made")
		return nil
	}
	if !deps.Flags.Yes && !deps.Flags.Force {
		if !ui.Confirm("Remove this file from ~/.agents/rules/?", false) {
			ui.Info("Cancelled.")
			return nil
		}
	}

	if err := os.Remove(spec.SourcePath); err != nil {
		return fmt.Errorf("removing rule file: %w", err)
	}
	ui.Success(fmt.Sprintf("Removed rule file %q from scope %s.", spec.BaseName, scope))
	return nil
}

func findRuleSpec(deps rulesDeps, agentsHome, scope, name string) (*platform.RuleFileSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, deps.usageError("rule name is empty", "Pass the file name or stem shown by `dot-agents rules list`.")
	}
	spec, err := platform.ResolveCanonicalRuleFile(agentsHome, scope, name)
	if err != nil {
		return nil, deps.errorWithHints(
			fmt.Sprintf("rule not found: %s / %s", scope, name),
			"Run `dot-agents rules list "+scope+"` to see available files.",
		)
	}
	return spec, nil
}
