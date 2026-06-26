// Package adapterkit holds the schema-info and query-compilation boilerplate
// every built-in graph-backend adapter shares, so each adapter (compliance-
// register, sdd-register, …) translates its registry.Schema and compiles its
// DSL queries through ONE source instead of copy-pasting the loop bodies. This
// collapses the structurally-identical translation/compile blocks the adapters
// would otherwise each carry.
package adapterkit

import (
	"fmt"

	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// BuildSchemaInfo compiles a dsl.SchemaInfo from a registry.Schema: it
// translates note/edge declarations into the DSL's typed-ref-aware form and
// carries the impact_radius max_depth as the variable-length bound. Every
// built-in adapter uses this rather than re-deriving the translation.
func BuildSchemaInfo(s registry.Schema) (dsl.SchemaInfo, error) {
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

// Compiled is the result of loading + compiling an adapter schema: the parsed
// schema, its DSL SchemaInfo, the impact query, and the named-query map. An
// adapter's newFromYAML wraps this with its own struct assembly.
type Compiled struct {
	Schema registry.Schema
	Info   dsl.SchemaInfo
	Impact *dsl.Query
	Named  map[string]*dsl.Query
}

// Load is the full load+compile pass every built-in adapter's newFromYAML runs:
// parse+validate the schema YAML, build the DSL SchemaInfo, and compile the
// impact + named queries against it. It returns the Compiled bundle or the
// first error, so each adapter's newFromYAML is just `Load(...)` plus its own
// struct assembly — the shared preamble lives here once.
func Load(yaml []byte, namedSrc map[string]string) (Compiled, error) {
	schema, err := registry.LoadSchema(yaml)
	if err != nil {
		return Compiled{}, fmt.Errorf("embedded schema invalid: %w", err)
	}
	info, err := BuildSchemaInfo(schema)
	if err != nil {
		return Compiled{}, fmt.Errorf("schema info: %w", err)
	}
	impact, named, err := CompileQueries(schema.ImpactRadius.Query, namedSrc, info)
	if err != nil {
		return Compiled{}, err
	}
	return Compiled{Schema: schema, Info: info, Impact: impact, Named: named}, nil
}

// CompileQueries parses an adapter's impact-radius query plus its named-query
// catalog against the compiled SchemaInfo, returning the impact query and a
// name→*Query map (or the first DSL compile error). It is the single
// implementation of the impact+named compile pass the adapters share.
func CompileQueries(impactSrc string, namedSrc map[string]string, info dsl.SchemaInfo) (*dsl.Query, map[string]*dsl.Query, error) {
	impact, err := dsl.ParseWithSchema(impactSrc, info)
	if err != nil {
		return nil, nil, fmt.Errorf("impact_radius query: %w", err)
	}
	named := make(map[string]*dsl.Query, len(namedSrc))
	for name, src := range namedSrc {
		q, err := dsl.ParseWithSchema(src, info)
		if err != nil {
			return nil, nil, fmt.Errorf("named query %q: %w", name, err)
		}
		named[name] = q
	}
	return impact, named, nil
}
