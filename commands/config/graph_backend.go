package config

import (
	crgbridge "github.com/AGOrcha/dot-agents/internal/adapters/builtin/crg-bridge"
	"github.com/AGOrcha/dot-agents/internal/adapters/builtin/none"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// registerBuiltinGraphBackends registers every graph-backend adapter that ships
// inside `da`: the null `none` backend plus the CRG family. The family is
// registered through crgbridge.RegisterCRGFamily rather than the two Register
// calls, because that entry point also runs registry.EnforceReadsFrom — without
// it the §11.2 migration_only gate is registered but inert, so an adapter
// declaring reads_from the migration-only mirror would load instead of being
// rejected. It is a package var so a test can substitute a registry whose
// registration fails (a path a fresh registry never hits in production),
// mirroring the seam in commands/kg.
var registerBuiltinGraphBackends = func(reg *registry.Registry) error {
	if err := none.Register(reg); err != nil {
		return err
	}
	return crgbridge.RegisterCRGFamily(reg)
}

// builtinGraphRegistry returns a registry pre-populated with the built-in
// graph-backend adapters. It is the single construction point the config
// command's graph_backend resolution uses, so the set of built-ins resolvable
// from a profile's `graph_backend` ref stays in one place.
func builtinGraphRegistry() (*registry.Registry, error) {
	reg := registry.New()
	if err := registerBuiltinGraphBackends(reg); err != nil {
		return nil, err
	}
	return reg, nil
}

// resolveGraphBackend resolves a profile's graph_backend adapter-ref against the
// built-in registry's ref resolver (graph-backend-adapter-contract §8 / the t1
// registry). This is the config-side selection path: a profile declaring
// `graph_backend: dotagents-builtin:graph/none@^1.0` resolves to the registered
// `none` adapter end-to-end, the same resolver dispatch uses.
func resolveGraphBackend(ref string) (registry.Adapter, error) {
	reg, err := builtinGraphRegistry()
	if err != nil {
		return nil, err
	}
	return reg.Resolve(ref)
}
