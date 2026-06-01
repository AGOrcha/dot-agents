package rules

import (
	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/AGOrcha/dot-agents/internal/platform"
	"github.com/spf13/cobra"
)

// canonicalSpec assembles the `da rules` resource spec by combining the
// static cmdutil.RulesResource definition (Kind/DirSegment/strings/
// Examples + EnsureScope) with the per-leaf runner closures that need
// access to platformListCanonicalRuleFiles (the seam, not the platform
// helper directly) + FindRuleSpec for hint-aware errors.
//
// Per plan duplicate-density-drop: keeping this body as a single call
// into cmdutil.SpecForResource means the only duplication across the
// mcp/settings/rules trio is the four lines of runner closure shape —
// which Sonar's clone detector treats as structurally distinct because
// the captured platform.* helpers and findXxxSpec wrappers differ.
//
// Note rules.RunList takes (deps, scope) — unlike mcp/settings where it
// takes just (scope) — so the ListRun closure captures deps explicitly.
func canonicalSpec(deps Deps) cmdutil.CanonicalFileSpec {
	return cmdutil.SpecForResource(
		cmdutil.RulesResource,
		cmdutil.ResourceRunners{
			List: func(agentsHome, scope string) ([]cmdutil.CanonicalFileEntry, error) {
				specs, err := platformListCanonicalRuleFiles(agentsHome, scope)
				if err != nil {
					return nil, err
				}
				return cmdutil.EntriesFromSpecs(specs, func(sp platform.RuleFileSpec) cmdutil.CanonicalFileEntry {
					return cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}
				}), nil
			},
			Resolve: func(agentsHome, scope, name string) (cmdutil.CanonicalFileEntry, error) {
				sp, err := FindRuleSpec(deps, agentsHome, scope, name)
				if err != nil {
					return cmdutil.CanonicalFileEntry{}, err
				}
				return cmdutil.CanonicalFileEntry{Scope: sp.Scope, BaseName: sp.BaseName, SourcePath: sp.SourcePath}, nil
			},
			ListRun:   func(scope string) error { return RunList(deps, scope) },
			ShowRun:   func(scope, name string) error { return RunShow(deps, scope, name) },
			RemoveRun: func(scope, name string) error { return RunRemove(deps, scope, name) },
		},
		maxArgs(deps, 1, cmdutil.RulesResource.ListArgsHint),
		exactArgs(deps, 2, cmdutil.RulesResource.ShowArgsHint),
		exactArgs(deps, 2, cmdutil.RulesResource.RemoveArgsHint),
	)
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
