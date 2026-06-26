// Package sddregister implements the SDD-entity dogfood graph-backend adapter.
//
// It is the "KG-as-SOT dogfood": instead of a hand-authored corpus, its
// Ingest walks THIS repo's real .agents/workflow/ tree (specs/, plans/) and
// projects the specs, plans, and tasks into typed KG nodes + edges following
// the v4 SUBSET of sdd-entity-kg-schema-draft.md (node types spec/plan/task;
// edges contains_task, belongs_to_plan, depends_on, plan_for_spec,
// implements_spec). The notes/edges are written through the da-adapter-sdk SDK
// into an in-memory namespace, and the named trace queries (queries.go) run
// over that namespace via the v1 DSL evaluator (internal/kg/dsl).
//
// The adapter reuses the canonical PLAN.yaml/TASKS.yaml structs from
// commands/workflow (CanonicalPlan, CanonicalTaskFile, CanonicalTask) rather
// than re-deriving the YAML shape, and parses spec design.md frontmatter
// best-effort (the spec frontmatter is freeform markdown, not a schema'd
// artifact — see the GAPS the dogfood surfaces).
package sddregister

import (
	// blank import enables the //go:embed directive on schemaYAML.
	_ "embed"
	"fmt"

	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// Name is the adapter's short name and namespace.
const Name = "sdd-register"

//go:embed schema.yaml
var schemaYAML []byte

// Adapter is the built-in sdd-register dogfood adapter. Construct with New so
// the schema and DSL queries are compiled once.
type Adapter struct {
	schema registry.Schema
	info   dsl.SchemaInfo
	impact *dsl.Query
	named  map[string]*dsl.Query
}

// New constructs the adapter from the embedded schema, panicking on a malformed
// embed or query — a build-time-fixable bug that cannot occur for a shipped
// binary. The fallible logic lives in newFromYAML so its error paths are
// exercised by tests with deliberately-bad input.
func New() *Adapter { return mustFromYAML(schemaYAML) }

// mustFromYAML builds the adapter or panics. It is the panic seam New uses; a
// white-box test drives it with bad bytes so the panic branch is exercised
// (New itself can only ever be called with the valid embed).
func mustFromYAML(yaml []byte) *Adapter {
	a, err := newFromYAML(yaml)
	if err != nil {
		panic("sdd-register: " + err.Error())
	}
	return a
}

// newFromYAML builds the adapter from an adapter-schema YAML, returning an
// error (rather than panicking) for an invalid schema or a query that fails to
// compile against it. This is the testable core of New.
func newFromYAML(yaml []byte) (*Adapter, error) {
	schema, err := registry.LoadSchema(yaml)
	if err != nil {
		return nil, fmt.Errorf("embedded schema invalid: %w", err)
	}
	info, err := buildSchemaInfo(schema)
	if err != nil {
		return nil, fmt.Errorf("schema info: %w", err)
	}
	a := &Adapter{schema: schema, info: info, named: map[string]*dsl.Query{}}
	if err := a.compileQueries(); err != nil {
		return nil, err
	}
	return a, nil
}

// compileQueries parses the impact-radius and named trace queries against the
// adapter's SchemaInfo, returning the first DSL compile error.
func (a *Adapter) compileQueries() error {
	q, err := dsl.ParseWithSchema(a.schema.ImpactRadius.Query, a.info)
	if err != nil {
		return fmt.Errorf("impact_radius query: %w", err)
	}
	a.impact = q
	for name, src := range namedQuerySources() {
		nq, err := dsl.ParseWithSchema(src, a.info)
		if err != nil {
			return fmt.Errorf("named query %q: %w", name, err)
		}
		a.named[name] = nq
	}
	return nil
}

// Name returns the adapter name.
func (a *Adapter) Name() string { return a.schema.Name }

// Schema returns the adapter's declarative schema (§4).
func (a *Adapter) Schema() registry.Schema { return a.schema }

// SchemaInfo returns the compiled DSL schema info.
func (a *Adapter) SchemaInfo() dsl.SchemaInfo { return a.info }

// ImpactRadius satisfies registry.Adapter. The dogfood does not run the
// impact-radius traversal at the registry surface (the trace queries are the
// load-bearing path); it returns the changed ids unchanged, like the `none`
// adapter, so the adapter is registry-resolvable.
func (a *Adapter) ImpactRadius(req registry.ImpactRequest) (registry.ImpactResult, error) {
	ids := make([]string, len(req.ChangedIDs))
	copy(ids, req.ChangedIDs)
	return registry.ImpactResult{IDs: ids}, nil
}

// buildSchemaInfo compiles a dsl.SchemaInfo from the registry schema.
func buildSchemaInfo(s registry.Schema) (dsl.SchemaInfo, error) {
	notes := make([]dsl.NoteTypeDecl, 0, len(s.NoteTypes))
	for _, nt := range s.NoteTypes {
		fields := make([]dsl.FieldDecl, 0, len(nt.Fields))
		for _, f := range nt.Fields {
			fields = append(fields, dsl.FieldDecl{Name: f.Name, Type: f.Type, Derivation: f.Derivation})
		}
		notes = append(notes, dsl.NoteTypeDecl{Name: nt.Name, Fields: fields})
	}
	edges := make([]dsl.EdgeTypeDecl, 0, len(s.EdgeTypes))
	for _, et := range s.EdgeTypes {
		edges = append(edges, dsl.EdgeTypeDecl{Name: et.Name, From: et.From, To: et.To, Derivation: et.Derivation})
	}
	return dsl.NewSchemaInfo(notes, edges, s.ImpactRadius.MaxDepth)
}

// Register adds the sdd-register adapter to reg.
func Register(reg *registry.Registry) error {
	return reg.Register(New())
}
