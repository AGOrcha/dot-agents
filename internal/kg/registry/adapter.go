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

// Schema is the declarative adapter schema (spec §4). It captures only the
// fields the `none` adapter exercises end-to-end; richer fields (queries,
// materialized_views, env_predicates, planner hints) are added by later
// tasks in this plan.
type Schema struct {
	Name             string       `yaml:"name" json:"name"`
	Version          string       `yaml:"version" json:"version"`
	Description      string       `yaml:"description,omitempty" json:"description,omitempty"`
	NoteTypes        []NoteType   `yaml:"note_types" json:"note_types"`
	EdgeTypes        []EdgeType   `yaml:"edge_types" json:"edge_types"`
	ImpactRadius     ImpactRadius `yaml:"impact_radius" json:"impact_radius"`
	StalenessDrivers []string     `yaml:"staleness_drivers,omitempty" json:"staleness_drivers,omitempty"`
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
}

// New returns an empty Registry.
func New() *Registry {
	return &Registry{adapters: make(map[string]Adapter)}
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
