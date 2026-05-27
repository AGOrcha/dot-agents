package rules

import (
	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/NikashPrakash/dot-agents/internal/platform"
	"github.com/spf13/cobra"
)

// canonicalSpec is the single source of truth for the `da rules`
// resource family. It populates both the data-layer fields
// cmdutil.RunCanonical{List,Show,Remove} consume (via FindRuleSpec for
// hint-aware resolution) and the CLI-surface fields
// cmdutil.NewCanonicalResourceCmd consumes. One struct literal per
// resource — there is no parallel ResourceCmdSpec to keep in sync.
//
// Note rules.RunList takes (deps, scope) — unlike mcp/settings where it
// takes just (scope) — so the ListRun closure captures deps explicitly.
func canonicalSpec(deps Deps) cmdutil.CanonicalFileSpec {
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
			specs, err := platformListCanonicalRuleFiles(agentsHome, scope)
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
			sp, err := FindRuleSpec(deps, agentsHome, scope, name)
			if err != nil {
				return cmdutil.CanonicalFileEntry{}, err
			}
			return cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}, nil
		},
		EnsureScope: platform.EnsureUnderRulesScopeTree,

		Use:   "rules",
		Short: "Inspect and manage canonical ~/.agents/rules files",
		Long: `Commands for rule files stored under ~/.agents/rules/<scope>/.

Scopes are either global (~/.agents/rules/global/) or a managed project name
(~/.agents/rules/<project>/), matching da status.

These files are what add, import, refresh, install, and remove wire into
Cursor, Claude Code, Codex, and Copilot projections. Prefer editing canonical
paths here, then run refresh or install for the project — do not hand-edit
platform copies unless you know they are unmanaged.`,
		Example: cmdutil.CanonicalCmdExampleBlock(
			"  da rules list",
			"  da rules list my-app",
			"  da rules show global rules.mdc",
			"  da rules remove global old-rule.mdc",
		),
		ListSub: cmdutil.SubCmdStrings{
			Use:   "list [scope]",
			Short: "List canonical rule files for a scope",
			Example: cmdutil.CanonicalCmdExampleBlock(
				"  da rules list",
				"  da rules list billing-api",
			),
		},
		ListArgs: maxArgs(deps, 1, "Optionally pass a project scope (or `global`) to inspect that rules tree."),
		ListRun:  func(scope string) error { return RunList(deps, scope) },
		ShowSub: cmdutil.SubCmdStrings{
			Use:   "show <scope> <name>",
			Short: "Show metadata for one rule file under ~/.agents/rules/",
		},
		ShowArgs: exactArgs(deps, 2, "`scope` is `global` or a managed project name; `name` is the file (e.g. rules.mdc) or stem (rules)."),
		ShowRun:  func(scope, name string) error { return RunShow(deps, scope, name) },
		RemoveSub: cmdutil.SubCmdStrings{
			Use:   "remove <scope> <name>",
			Short: "Remove a rule file from ~/.agents/rules/ (canonical storage only)",
			Long: `Deletes the file from managed rule storage only (not repo links). After removal,
run da refresh or install for the relevant project so platform rule
links stay consistent.`,
		},
		RemoveArgs: exactArgs(deps, 2, "`scope` is `global` or a managed project name; `name` matches list/show."),
		RemoveRun:  func(scope, name string) error { return RunRemove(deps, scope, name) },
	}
}

// maxArgs / exactArgs guard against the zero-value Deps used by the
// data-layer RunList path. The CLI wiring in NewRulesCmd always supplies
// real helpers via Deps; the data path only needs the data-layer spec
// fields and never invokes Args, so nil-returning fallbacks are safe.
func maxArgs(deps Deps, n int, hints ...string) cobra.PositionalArgs {
	if deps.MaximumNArgsWithHints == nil {
		return nil
	}
	return deps.MaximumNArgsWithHints(n, hints...)
}

func exactArgs(deps Deps, n int, hints ...string) cobra.PositionalArgs {
	if deps.ExactArgsWithHints == nil {
		return nil
	}
	return deps.ExactArgsWithHints(n, hints...)
}

// RunList lists canonical rule files for the given scope. Exported so the
// shim in commands/rules.go can delegate to it.
func RunList(deps Deps, scope string) error {
	return cmdutil.RunCanonicalList(scope, canonicalSpec(deps))
}
