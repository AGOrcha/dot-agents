package rules

import (
	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/NikashPrakash/dot-agents/internal/platform"
)

// canonicalSpec wires platform's ListCanonicalRuleFiles / ResolveCanonicalRuleFile
// into cmdutil.RunCanonical{List,Show,Remove}. The deps parameter threads the
// errorWithHints / usageError hooks into findRuleSpec so existing tests can
// assert on hint-aware errors.
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
	}
}

// RunList lists canonical rule files for the given scope. Exported so the
// shim in commands/rules.go can delegate to it.
func RunList(deps Deps, scope string) error {
	return cmdutil.RunCanonicalList(scope, canonicalSpec(deps))
}
