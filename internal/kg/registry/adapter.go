// Package registry defines the graph-backend adapter contract and a
// register/resolve surface keyed by adapter name.
//
// It implements the smallest slice of
// graph-backend-adapter-contract §4 (adapter schema) and §8.2 (namespace
// tokens) needed to drive the built-in `none` adapter end-to-end: an
// Adapter interface, the declarative Schema value types, semantic-version
// ref parsing for refs of the form
// `dotagents-builtin:graph/<name>@<constraint>`, and a process-wide
// registry that resolves a ref to a registered adapter.
package registry

import (
	"fmt"
	"sort"
	"sync"
)

// FieldType enumerates the note/edge field types from spec §4.
type FieldType string

// FieldSpec describes a single field on a note type (spec §4).
type FieldSpec struct {
	Name       string   `yaml:"name" json:"name"`
	Type       string   `yaml:"type" json:"type"`
	Required   bool     `yaml:"required,omitempty" json:"required,omitempty"`
	Values     []string `yaml:"values,omitempty" json:"values,omitempty"`
	Derivation bool     `yaml:"derivation,omitempty" json:"derivation,omitempty"`
}

// NoteType is a declared note type in an adapter schema (spec §4).
type NoteType struct {
	Name   string      `yaml:"name" json:"name"`
	Fields []FieldSpec `yaml:"fields,omitempty" json:"fields,omitempty"`
}

// EdgeType is a declared edge type in an adapter schema (spec §4).
type EdgeType struct {
	Name        string `yaml:"name" json:"name"`
	From        string `yaml:"from" json:"from"`
	To          string `yaml:"to" json:"to"`
	Cardinality string `yaml:"cardinality,omitempty" json:"cardinality,omitempty"`
	Signed      bool   `yaml:"signed,omitempty" json:"signed,omitempty"`
	WeightField string `yaml:"weight_field,omitempty" json:"weight_field,omitempty"`
	Derivation  bool   `yaml:"derivation,omitempty" json:"derivation,omitempty"`
}

// ImpactRadius is the required impact-radius declaration (spec §4).
type ImpactRadius struct {
	Query         string `yaml:"query" json:"query"`
	MaxDepth      int    `yaml:"max_depth" json:"max_depth"`
	AlgorithmHint string `yaml:"algorithm_hint,omitempty" json:"algorithm_hint,omitempty"`
}

// MaterializedView is a declared materialized view (spec §8.3). Cross-adapter
// reads are expressed ONLY through a view's reads_from list — a named query
// cannot read another namespace. The §11.2 loader rule gates these reads_from
// declarations against migration_only dependencies.
type MaterializedView struct {
	Name      string   `yaml:"name" json:"name"`
	ReadsFrom []string `yaml:"reads_from,omitempty" json:"reads_from,omitempty"`
}

// Schema is the declarative adapter schema (spec §4). It captures only the
// fields the built-in adapters exercise end-to-end; richer fields (queries,
// env_predicates, planner hints) are added by later tasks in this plan.
type Schema struct {
	Name        string `yaml:"name" json:"name"`
	Version     string `yaml:"version" json:"version"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// MigrationOnly marks an adapter that exists solely as a temporary
	// migration surface (spec §11.2, added in v4.2). Long-term adapters must
	// not declare reads_from against a migration_only adapter; the loader
	// rejects such dependencies at registration time.
	MigrationOnly bool       `yaml:"migration_only,omitempty" json:"migration_only,omitempty"`
	NoteTypes     []NoteType `yaml:"note_types" json:"note_types"`
	EdgeTypes     []EdgeType `yaml:"edge_types" json:"edge_types"`
	// MaterializedViews declares the adapter's cross-adapter views (§8.3); each
	// view's reads_from is gated by the §11.2 rule at load.
	MaterializedViews []MaterializedView `yaml:"materialized_views,omitempty" json:"materialized_views,omitempty"`
	ImpactRadius      ImpactRadius       `yaml:"impact_radius" json:"impact_radius"`
	StalenessDrivers  []string           `yaml:"staleness_drivers,omitempty" json:"staleness_drivers,omitempty"`
}

// ReadsFrom returns the de-duplicated union of every materialized view's
// reads_from targets — the full set of adapters this schema declares a
// cross-adapter dependency on. It is the input the §11.2 load gate validates.
func (s Schema) ReadsFrom() []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range s.MaterializedViews {
		for _, dep := range v.ReadsFrom {
			if !seen[dep] {
				seen[dep] = true
				out = append(out, dep)
			}
		}
	}
	return out
}

// ImpactRequest is the input to an adapter's ImpactRadius operation.
type ImpactRequest struct {
	// ChangedIDs are the node ids whose blast radius is requested. The DSL
	// binds these to the $changed_ids parameter.
	ChangedIDs []string
}

// ImpactResult is the output of an adapter's ImpactRadius operation. The
// `none` adapter returns ChangedIDs unchanged (no expansion).
type ImpactResult struct {
	// IDs are the impacted node ids. For the `none` adapter this is exactly
	// the input ChangedIDs (max_depth 0, no neighborhood expansion).
	IDs []string
}

// Adapter is the contract every graph backend satisfies. The `none` adapter
// is the minimal conforming implementation: it declares an empty schema and
// a no-op impact radius.
type Adapter interface {
	// Name is the adapter's short name (e.g. "none"). It must match the
	// name in Schema().
	Name() string
	// Schema returns the adapter's declarative schema (spec §4).
	Schema() Schema
	// ImpactRadius runs the adapter's impact-radius operation. The `none`
	// adapter returns the changed ids unchanged.
	ImpactRadius(req ImpactRequest) (ImpactResult, error)
}

// Registry is a process-wide map of adapter name to Adapter. The zero value
// is not usable; construct with New.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
	// readsFrom records each adapter's declared cross-adapter dependencies
	// (DeclareReadsFrom), so the §11.2 migration_only gate can be enforced
	// transitively.
	readsFrom map[string][]string
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{adapters: make(map[string]Adapter), readsFrom: make(map[string][]string)}
}

// Register adds a in the registry keyed by its Name. It returns an error if
// the adapter is nil, has an empty name, or a name collides with an
// already-registered adapter.
func (r *Registry) Register(a Adapter) error {
	if a == nil {
		return fmt.Errorf("registry: cannot register nil adapter")
	}
	name := a.Name()
	if name == "" {
		return fmt.Errorf("registry: adapter has empty name")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.adapters[name]; ok {
		return fmt.Errorf("registry: adapter %q already registered", name)
	}
	r.adapters[name] = a
	return nil
}

// Resolve looks up an adapter by a backend ref string of the form
// `dotagents-builtin:graph/<name>@<constraint>` (or the bare `<name>`).
// It returns an error if the ref cannot be parsed, the named adapter is not
// registered, or the registered adapter's version does not satisfy the
// ref's constraint.
func (r *Registry) Resolve(ref string) (Adapter, error) {
	parsed, err := ParseRef(ref)
	if err != nil {
		return nil, err
	}
	r.mu.RLock()
	a, ok := r.adapters[parsed.Name]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("registry: no adapter named %q registered", parsed.Name)
	}
	if parsed.Constraint != nil {
		v, err := ParseVersion(a.Schema().Version)
		if err != nil {
			return nil, fmt.Errorf("registry: adapter %q has invalid version %q: %w", parsed.Name, a.Schema().Version, err)
		}
		if !parsed.Constraint.Satisfies(v) {
			return nil, fmt.Errorf("registry: adapter %q version %s does not satisfy %s", parsed.Name, a.Schema().Version, ref)
		}
	}
	return a, nil
}

// DeclareReadsFrom is the loader entry point that records dependent's
// cross-adapter reads_from declaration and GATES it against the §11.2 rule. The
// loader calls this when an adapter (or one of its materialized views) declares
// reads_from; a returned error means the declaration is rejected and the
// adapter must not activate. On success the declaration is recorded so later
// transitive checks see it. Both the dependent and every named dependency must
// already be registered (missing names are rejected — a reads_from against an
// unregistered adapter cannot be validated and so cannot be allowed).
func (r *Registry) DeclareReadsFrom(dependent string, readsFrom []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkReadsFromLocked(dependent, readsFrom); err != nil {
		return err
	}
	r.readsFrom[dependent] = append([]string(nil), readsFrom...)
	return nil
}

// ValidateReadsFrom checks the §11.2 rule for a candidate reads_from
// declaration WITHOUT recording it (the read-only counterpart of
// DeclareReadsFrom). It is the gate a loader runs before activation.
func (r *Registry) ValidateReadsFrom(dependent string, readsFrom []string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.checkReadsFromLocked(dependent, readsFrom)
}

// EnforceReadsFrom is the load gate that validates EVERY registered adapter's
// schema-declared reads_from (the union across its materialized views) against
// the §11.2 rule, and records the dependency edges so the check is transitive
// and order-independent. The built-in registration path calls this once after
// all adapters are registered; a returned error means an adapter declares a
// forbidden (direct or transitive) dependency on a migration_only adapter and
// the load is rejected. Without this call the gate is inert — declaring
// reads_from [crg-bridge] from a long-term adapter would otherwise load.
func (r *Registry) EnforceReadsFrom() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	sort.Strings(names) // deterministic error ordering
	// Pass 1: structural checks (self-ref, unknown dep) + record every edge so
	// pass 2's transitive walk sees the full graph regardless of sweep order.
	for _, name := range names {
		deps := r.adapters[name].Schema().ReadsFrom()
		if err := r.checkReadsFromEdgesLocked(name, deps); err != nil {
			return err
		}
		if len(deps) > 0 {
			r.readsFrom[name] = append([]string(nil), deps...)
		}
	}
	// Pass 2: transitive migration_only reachability over the recorded graph.
	for _, name := range names {
		if r.isMigrationOnlyLocked(name) {
			continue // mirrors may read mirrors
		}
		if hit := r.reachesMigrationOnlyLocked(r.readsFrom[name]); hit != "" {
			return fmt.Errorf("registry: adapter %q must not reads_from migration_only adapter %q (spec §11.2)", name, hit)
		}
	}
	return nil
}

// checkReadsFromLocked enforces the §11.2 rule. The caller holds r.mu.
//
//   - A migration_only dependent MAY read migration_only deps (mirrors are not
//     long-term consumers) — short-circuit allow.
//   - A self-reference is rejected (an adapter cannot reads_from itself).
//   - Every named dependency must be registered; an unknown dep is rejected.
//   - A non-migration dependent must not reach ANY migration_only adapter, even
//     transitively through other adapters' recorded reads_from edges.
func (r *Registry) checkReadsFromLocked(dependent string, readsFrom []string) error {
	if err := r.checkReadsFromEdgesLocked(dependent, readsFrom); err != nil {
		return err
	}
	if r.isMigrationOnlyLocked(dependent) {
		return nil
	}
	if hit := r.reachesMigrationOnlyLocked(readsFrom); hit != "" {
		return fmt.Errorf("registry: adapter %q must not reads_from migration_only adapter %q (spec §11.2)", dependent, hit)
	}
	return nil
}

// checkReadsFromEdgesLocked runs the per-edge structural checks shared by the
// single-declaration and whole-registry gates: no self-reference, and every
// named dependency must be registered. The caller holds r.mu.
func (r *Registry) checkReadsFromEdgesLocked(dependent string, readsFrom []string) error {
	for _, name := range readsFrom {
		if name == dependent {
			return fmt.Errorf("registry: adapter %q must not reads_from itself", dependent)
		}
		if _, ok := r.adapters[name]; !ok {
			return fmt.Errorf("registry: adapter %q reads_from unregistered adapter %q", dependent, name)
		}
	}
	return nil
}

// isMigrationOnlyLocked reports whether a registered adapter is migration_only.
func (r *Registry) isMigrationOnlyLocked(name string) bool {
	a, ok := r.adapters[name]
	return ok && a.Schema().MigrationOnly
}

// reachesMigrationOnlyLocked returns the first migration_only adapter reachable
// from any of seeds, following recorded reads_from edges transitively. Empty
// string means none is reachable.
func (r *Registry) reachesMigrationOnlyLocked(seeds []string) string {
	seen := make(map[string]bool, len(seeds))
	stack := append([]string(nil), seeds...)
	for len(stack) > 0 {
		name := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[name] {
			continue
		}
		seen[name] = true
		if r.isMigrationOnlyLocked(name) {
			return name
		}
		stack = append(stack, r.readsFrom[name]...)
	}
	return ""
}

// Names returns the registered adapter names in sorted order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.adapters))
	for n := range r.adapters {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
