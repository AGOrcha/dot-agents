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

// New constructs the adapter, compiling its schema, SchemaInfo, impact-radius
// query, and named queries. It panics on a malformed embed or query (a
// build-time-fixable bug that cannot occur for a shipped binary).
func New() *Adapter {
	schema, err := registry.LoadSchema(schemaYAML)
	if err != nil {
		panic("compliance-register: embedded schema invalid: " + err.Error())
	}
	info, err := buildSchemaInfo(schema)
	if err != nil {
		panic("compliance-register: schema info: " + err.Error())
	}
	a := &Adapter{schema: schema, info: info, envPred: envPredicates(), named: map[string]*dsl.Query{}}
	a.compileQueries()
	return a
}

// compileQueries parses the impact-radius and named queries against the
// adapter's SchemaInfo, panicking on any DSL error (build-time bug).
func (a *Adapter) compileQueries() {
	q, err := dsl.ParseWithSchema(a.schema.ImpactRadius.Query, a.info)
	if err != nil {
		panic("compliance-register: impact_radius query: " + err.Error())
	}
	a.impact = q
	for name, src := range namedQuerySources() {
		nq, err := dsl.ParseWithSchema(src, a.info)
		if err != nil {
			panic(fmt.Sprintf("compliance-register: named query %q: %v", name, err))
		}
		a.named[name] = nq
	}
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
