package hooks

import "github.com/NikashPrakash/dot-agents/internal/platform"

// hookSpecResolver is the dependency seam for the hook-bundle removal
// pipeline: the three deeper operations runHooksRemove sequences before
// it touches the filesystem. It is one cohesive interface (a hook-spec
// resolver) rather than several tiny ones because the operations form a
// single resolve-then-validate flow whose intermediate error returns are
// the behavior under test. Production composes the real implementation
// via Deps; tests inject a fake that drives an error into a specific
// guard. This matches the package's existing Deps-struct DI idiom (no
// package-global seams) and is forward-compatible with the
// di-refactor-rollout contract-bound Deps shape (a typed handle field).
type hookSpecResolver interface {
	// ResolveSpec locates the hook spec for scope/name under agentsHome.
	ResolveSpec(deps Deps, agentsHome, scope, name string) (*platform.HookSpec, error)
	// RemovalTarget maps a resolved spec to the path that would be removed.
	RemovalTarget(spec *platform.HookSpec) (string, error)
	// EnsureUnderScopeTree refuses targets that escape the scope subtree.
	EnsureUnderScopeTree(agentsHome, scope, target string) error
}

// defaultHookSpecResolver is the production implementation: it delegates
// to the real package functions, so the happy path is behavior-identical.
type defaultHookSpecResolver struct{}

func (defaultHookSpecResolver) ResolveSpec(deps Deps, agentsHome, scope, name string) (*platform.HookSpec, error) {
	return findHookSpec(deps, agentsHome, scope, name)
}

func (defaultHookSpecResolver) RemovalTarget(spec *platform.HookSpec) (string, error) {
	return hookRemovalTarget(spec)
}

func (defaultHookSpecResolver) EnsureUnderScopeTree(agentsHome, scope, target string) error {
	return ensureUnderHooksScopeTree(agentsHome, scope, target)
}

// hookSpecResolver returns the composed resolver, defaulting to the real
// implementation when none is injected. Keeping the default here (rather
// than forcing every Deps construction site to set it) preserves the
// existing happy-path wiring in commands/hooks.go and testDeps() without
// edits, while still allowing tests to inject a fake.
func (d Deps) hookSpecResolver() hookSpecResolver {
	if d.Resolver != nil {
		return d.Resolver
	}
	return defaultHookSpecResolver{}
}
