package rules

import (
	"fmt"
	"strings"

	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/AGOrcha/dot-agents/internal/platform"
)

// RunRemove deletes a canonical rule file from ~/.agents/rules/. Exported so
// the shim in commands/rules.go can delegate to it.
func RunRemove(deps Deps, scope, name string) error {
	return cmdutil.RunCanonicalRemove(cmdutil.RemoveDeps{
		DryRun: deps.Flags.DryRun, Yes: deps.Flags.Yes, Force: deps.Flags.Force,
	}, scope, name, canonicalSpec(deps))
}

// FindRuleSpec resolves <scope>/<name> to a platform.RuleFileSpec, emitting
// hint-aware errors via deps.UsageError / deps.ErrorWithHints for the empty
// and not-found cases. Exported so the parent-package shim and its tests can
// reach it.
func FindRuleSpec(deps Deps, agentsHome, scope, name string) (*platform.RuleFileSpec, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, deps.UsageError("rule name is empty", "Pass the file name or stem shown by `da rules list`.")
	}
	spec, err := deps.io().ResolveCanonicalRuleFile(agentsHome, scope, name)
	if err != nil {
		return nil, deps.ErrorWithHints(
			fmt.Sprintf("rule not found: %s / %s", scope, name),
			"Run `da rules list "+scope+"` to see available files.",
		)
	}
	return spec, nil
}
