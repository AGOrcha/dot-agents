package rules

import (
	"os"

	"github.com/AGOrcha/dot-agents/internal/platform"
)

// ruleIO is the narrow filesystem + platform-lookup collaborator the rules
// subcommands need to drive their error-return branches. A writable tmp dir
// always reads back cleanly and platform.{List,Resolve}CanonicalRuleFile do
// not fail on well-formed fixtures, so these branches are otherwise
// unreachable; interface-DI lets a test inject a fake that returns a sentinel.
//
// Scope: exactly the three operations the rules list/show/remove flows call
// through (ReadFile for frontmatter parsing, ListCanonicalRuleFiles for the
// list runner, ResolveCanonicalRuleFile for FindRuleSpec). Per docs/TEST_SEAMS.md
// the whole package shares one role-named collaborator rather than per-file
// func-var swaps — the interface-DI shape that replaced the legacy
// `var osReadFile = os.ReadFile` package vars (plan
// seam-interface-di-migration / root-command-decomposition t15).
type ruleIO interface {
	ReadFile(name string) ([]byte, error)
	ListCanonicalRuleFiles(agentsHome, scope string) ([]platform.RuleFileSpec, error)
	ResolveCanonicalRuleFile(agentsHome, scope, name string) (*platform.RuleFileSpec, error)
}

// stdRuleIO is the production ruleIO backed by os + platform. The Deps.io
// accessor returns it whenever Deps.IO is nil, so production call sites and
// the zero-value Deps used by the data layer never need to wire it.
type stdRuleIO struct{}

func (stdRuleIO) ReadFile(name string) ([]byte, error) { return os.ReadFile(name) }

func (stdRuleIO) ListCanonicalRuleFiles(agentsHome, scope string) ([]platform.RuleFileSpec, error) {
	return platform.ListCanonicalRuleFiles(agentsHome, scope)
}

func (stdRuleIO) ResolveCanonicalRuleFile(agentsHome, scope, name string) (*platform.RuleFileSpec, error) {
	return platform.ResolveCanonicalRuleFile(agentsHome, scope, name)
}
