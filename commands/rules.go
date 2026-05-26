package commands

import (
	"github.com/NikashPrakash/dot-agents/commands/rules"
	"github.com/spf13/cobra"
)

// rulesDeps is a parent-package alias preserved so the cross-cutting tests in
// commands/ (resource_parity_test.go, coverage_test.go) and the local
// rules_test.go can keep referring to the legacy shape with minimal churn.
// The actual command tree lives in commands/rules/.
type rulesDeps = rules.Deps

// rulesCommandDeps builds the Deps struct passed to rules.NewRulesCmd. Mirrors
// agentsDeps / skillsDeps so the extracted subcommand subpackages share the
// same wiring pattern.
func rulesCommandDeps() rulesDeps {
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

// NewRulesCmd wires the rules subcommand tree. Thin shim preserved for
// source-compat with root.go and external callers.
func NewRulesCmd() *cobra.Command {
	return rules.NewRulesCmd(rulesCommandDeps())
}

// ── Test-only shims ──────────────────────────────────────────────────────────
// The cross-cutting commands/ tests (resource_parity_test.go, coverage_test.go)
// and rules_test.go reach into the per-subcommand constructors and run* helpers
// via these unexported wrappers. t12 re-homes those tests; until then the
// wrappers below keep the test surface stable.

func newRulesListCmd(deps rulesDeps) *cobra.Command   { return rules.NewListCmd(deps) }
func newRulesShowCmd(deps rulesDeps) *cobra.Command   { return rules.NewShowCmd(deps) }
func newRulesRemoveCmd(deps rulesDeps) *cobra.Command { return rules.NewRemoveCmd(deps) }

func runRulesList(scope string) error {
	return rules.RunList(rulesCommandDeps(), scope)
}

func runRulesShow(deps rulesDeps, scope, name string) error {
	return rules.RunShow(deps, scope, name)
}

func runRulesRemove(deps rulesDeps, scope, name string) error {
	return rules.RunRemove(deps, scope, name)
}

func makeRulesDeps(dryRun, yes, force bool) rulesDeps {
	return rules.Deps{
		Flags:                 rules.GlobalFlags{DryRun: dryRun, Yes: yes, Force: force},
		ErrorWithHints:        ErrorWithHints,
		UsageError:            UsageError,
		MaximumNArgsWithHints: MaximumNArgsWithHints,
		ExactArgsWithHints:    ExactArgsWithHints,
	}
}
