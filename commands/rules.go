package commands

import (
	"fmt"
	"os"
	"strings"

	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
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

// rulesCanonicalSpec wires rules.go's runRules* helpers into
// cmdutil.RunCanonical{List,Show,Remove}. The deps parameter threads
// the rulesDeps error/usage hooks into findRuleSpec, which is how
// existing tests assert on hint-aware errors.
func rulesCanonicalSpec(deps rulesDeps) cmdutil.CanonicalFileSpec {
	return cmdutil.CanonicalFileSpec{
		Kind:        "Rule",
		DirSegment:  "rules",
		SingularRem: "rule file",
		EmptyHint: func(scope string) string {
			return "No rule files (.mdc/.md/.txt) under ~/.agents/rules/" + scope + "/"
		},
		MissingDirHint: func(scope string) string {
			return "No ~/.agents/rules/" + scope + "/ directory yet (no canonical rule files for this scope)."
		},
		List: func(agentsHome, scope string) ([]cmdutil.CanonicalFileEntry, error) {
			specs, err := platform.ListCanonicalRuleFiles(agentsHome, scope)
			if err != nil {
				return nil, err
			}
			out := make([]cmdutil.CanonicalFileEntry, len(specs))
			for i, sp := range specs {
				out[i] = cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}
			}
			return out, nil
		},
		Resolve: func(agentsHome, scope, name string) (cmdutil.CanonicalFileEntry, error) {
			sp, err := findRuleSpec(deps, agentsHome, scope, name)
			if err != nil {
				return cmdutil.CanonicalFileEntry{}, err
			}
			return cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}, nil
		},
		EnsureScope: platform.EnsureUnderRulesScopeTree,
	}
}

func runRulesList(scope string) error {
	// rulesList path doesn't need rulesDeps — error hints only fire on
	// resolve, not on list/missing-dir paths. Use a zero-value deps.
	return cmdutil.RunCanonicalList(scope, rulesCanonicalSpec(rulesCommandDeps()))
}

// rulesShowFrontmatterExtra appends a `description:` line to the show
// output when the rule file has a non-empty frontmatter description.
func rulesShowFrontmatterExtra(srcPath string) {
	if desc := extractRuleFrontmatterDescription(srcPath); desc != "" {
		fmt.Fprintf(os.Stdout, "  %sdescription:%s %s\n", ui.Dim, ui.Reset, desc)
	}
}

func runRulesShow(deps rulesDeps, scope, name string) error {
	return cmdutil.RunCanonicalShow(scope, name, rulesCanonicalSpec(deps), rulesShowFrontmatterExtra)
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
	return cmdutil.RunCanonicalRemove(cmdutil.RemoveDeps{
		DryRun: deps.Flags.DryRun, Yes: deps.Flags.Yes, Force: deps.Flags.Force,
	}, scope, name, rulesCanonicalSpec(deps))
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
