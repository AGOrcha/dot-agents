// Package complianceregister implements the built-in compliance-register
// graph-backend adapter (graph-backend-adapter-contract §13.2).
//
// It is the first adapter that exercises the full v1 DSL engine
// (internal/kg/dsl): the impact-radius query and the named queries are real
// DSL strings compiled at adapter load and evaluated against a corpus loaded
// through the SDK (internal/adapters/sdk). The adapter is read-only at this
// task: a stub bootstrap loads a hand-authored GRC corpus (controls, risks,
// findings, evidence, regulations, policies, owners), and the named queries
// (impact_radius on a control status change; policy_review_due_impact after a
// policy webhook fires) return the §13.2 expected results.
//
// env_predicates (§7) are wired through the DSL's ApplyEnvTrigger: the evidence
// time_after predicate and the policy.review_due webhook predicate tag matching
// notes stale, and policy_review_due_impact reads those stale tags rather than
// recomputing date filtering at query time.
package complianceregister

import (
	// blank import enables the //go:embed directive on schemaYAML.
	_ "embed"
	"fmt"

	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// Name is the adapter's short name and namespace.
const Name = "compliance-register"

//go:embed schema.yaml
var schemaYAML []byte

// Adapter is the built-in compliance-register adapter. The zero value is
// usable; construct with New so the schema and DSL queries are compiled once.
type Adapter struct {
	schema  registry.Schema
	info    dsl.SchemaInfo
	impact  *dsl.Query
	named   map[string]*dsl.Query
	envPred []dsl.EnvPredicate
}

// New constructs the adapter from the embedded schema, panicking on a malformed
// embed or query — a build-time-fixable bug that cannot occur for a shipped
// binary. The fallible construction logic lives in newFromYAML so its error
// paths are exercised by tests with deliberately-bad input; New is the thin
// can't-fail-in-production entry point.
func New() *Adapter {
	a, err := newFromYAML(schemaYAML)
	if err != nil {
		panic("compliance-register: " + err.Error())
	}
	return a
}

// newFromYAML builds the adapter from an adapter-schema YAML, returning an error
// (rather than panicking) for an invalid schema or a query that fails to
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
	a := &Adapter{schema: schema, info: info, envPred: envPredicates(), named: map[string]*dsl.Query{}}
	if err := a.compileQueries(); err != nil {
		return nil, err
	}
	return a, nil
}

// compileQueries parses the impact-radius and named queries against the
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

// SchemaInfo returns the compiled DSL schema info (ref metadata + max depth),
// exposed so the SDK or a cross-adapter view can compile queries against the
// same shape the adapter validated.
func (a *Adapter) SchemaInfo() dsl.SchemaInfo { return a.info }

// EnvPredicates returns the declared environmental predicates (§7).
func (a *Adapter) EnvPredicates() []dsl.EnvPredicate { return a.envPred }

// buildSchemaInfo compiles a dsl.SchemaInfo from the registry schema: it
// translates note/edge declarations into the DSL's typed-ref-aware form and
// carries the impact_radius max_depth as the variable-length bound.
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

// envPredicates is the §13.2 env-predicate declaration set: evidence expiry via
// time_after, and policy review-due via a webhook endpoint.
func envPredicates() []dsl.EnvPredicate {
	return []dsl.EnvPredicate{
		{NoteType: "evidence", Kind: dsl.KindTimeAfter, Field: "expires_at"},
		{NoteType: "policy", Kind: dsl.KindWebhook, Endpoint: "policy.review_due"},
	}
}

// Register adds the compliance-register adapter to reg.
func Register(reg *registry.Registry) error {
	return reg.Register(New())
}
