// Package crossnamespace executes a cross-adapter materialized view: a single
// v1 DSL query (internal/kg/dsl) that joins a consumer adapter's namespace with
// one or more reads_from dependency namespaces
// (graph-backend-adapter-contract §8.3).
//
// It is the executor tier (§2.7) that sits above the token-checked Store seam
// (internal/adapters/sdk): the consumer's own notes are read with an own-read
// token (the readback idiom), each declared dependency is read with the §8.2
// multi-namespace ViewToken, and the derived rows are written back to the
// consumer namespace under that same ViewToken. Because the ViewToken grants
// write on the consumer namespace and read-only on the dependencies, a view can
// read its declared dependencies but can never write to them — the Store
// rejects a cross-adapter write before any row is touched (N8), and a read of
// any namespace the token does not grant is rejected too (N9, defense in depth).
//
// The single declaration site of a cross-adapter dependency is the view (§8.3):
// the view names its reads_from dependencies and the cross-namespace edges that
// link them, the multi-namespace token derivation (§8.2) keys off the same set,
// and the mechanical cutover gate (§10.3, CheckCompat) re-validates the view's
// query against a bumped dependency schema — there is no operator-ack surface
// (O1). A query that names a note type from an undeclared namespace fails at
// compile time, not at runtime (N6): the combined schema only contains the
// declared namespaces, so the parser rejects the unknown type.
//
// crossnamespace stays a dsl-level leaf: it imports only dsl and sdk, never the
// registry. Callers translate an adapter's registry.Schema into a Namespace via
// adapterkit.BuildSchemaInfo (see the compliance-register/views package).
package crossnamespace

import (
	"fmt"
	"sort"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
)

// errPrefix is the shared error namespace for this package (declared once to
// avoid duplicate-literal drift across the construction and gate paths).
const errPrefix = "crossnamespace"

// Namespace is one adapter's contribution to a cross-namespace view: its
// namespace name and the compiled DSL schema slice (note fields + edges) the
// view may read from it. Build Info from the adapter's registry.Schema via
// adapterkit.BuildSchemaInfo so this package never imports the registry.
type Namespace struct {
	Name string
	Info dsl.SchemaInfo
}

// CrossEdge is an edge type that spans two namespaces. It appears in neither
// single adapter's schema, so the view declares it explicitly (§8.3: the view
// is the single declaration site for a cross-adapter link). Example:
// {Name: "references", From: "evidence" (consumer), To: "symbol" (crg)}.
type CrossEdge struct {
	Name       string
	From       string
	To         string
	Derivation bool
}

// View is a compiled, validated cross-namespace materialized view (§8.3). It is
// built once via Compile and reused: the DSL query is parsed against the merged
// schema at construction, and Materialize replays it against current storage.
type View struct {
	name       string
	consumer   string
	deps       []string
	query      *dsl.Query
	touched    []string
	consumerNS Namespace
	crossEdges []CrossEdge
	querySrc   string
}

// Compile builds and validates a cross-namespace view. It merges the consumer
// and dependency schema slices (plus the view's cross-namespace edges) into one
// combined SchemaInfo, compiles the DSL query against it (§5 validation), and
// records the set of namespaces the query touches (§8.2 compiler obligation).
//
// A query that references a note type from a namespace not present in deps fails
// here: the merged schema only contains the declared namespaces, so the parser
// rejects the unknown type. This is the compile-time half of the §8.2 namespace
// check (N6); the run-time half is the Store's per-namespace token enforcement
// in Materialize (N7/N9).
func Compile(name string, consumer Namespace, deps []Namespace, crossEdges []CrossEdge, query string) (*View, error) {
	combined, nsOf, err := combine(consumer, deps, crossEdges)
	if err != nil {
		return nil, err
	}
	q, err := dsl.ParseWithSchema(query, combined)
	if err != nil {
		return nil, fmt.Errorf("%s: view %q: %w", errPrefix, name, err)
	}
	depNames := make([]string, len(deps))
	for i, d := range deps {
		depNames[i] = d.Name
	}
	return &View{
		name:       name,
		consumer:   consumer.Name,
		deps:       depNames,
		query:      q,
		touched:    touchedNamespaces(q, nsOf),
		consumerNS: consumer,
		crossEdges: crossEdges,
		querySrc:   query,
	}, nil
}

// Name returns the view name (also the note type of its derived rows).
func (v *View) Name() string { return v.name }

// Consumer returns the consumer namespace the view writes into.
func (v *View) Consumer() string { return v.consumer }

// Deps returns the reads_from dependency namespaces, in declared order.
func (v *View) Deps() []string { return append([]string(nil), v.deps...) }

// TouchedNamespaces returns the sorted set of namespaces the compiled query
// reads from (§8.2 compiler obligation: the compiler emits the namespaces a
// query touches as part of its metadata). It always includes only the consumer
// and the declared dependencies — a reference to anything else fails at Compile.
func (v *View) TouchedNamespaces() []string { return append([]string(nil), v.touched...) }

// Token derives the §8.2 multi-namespace ViewToken for this view: write on the
// consumer namespace plus read on each declared dependency (N3/N4).
func (v *View) Token() sdk.Token {
	return sdk.ViewToken(v.consumer, v.name, v.deps)
}

// Materialize reads every authorized namespace through the token-checked Store,
// merges them into one in-memory view, evaluates the view query, persists each
// result row as a derived note (type = view name) in the CONSUMER namespace,
// and returns the rows. Dependency reads and the derived-note write all carry
// the §8.2 ViewToken, so a write to a dependency is rejected at the Store layer
// (N8) and a read of an unauthorized namespace is rejected too (N9).
//
// Re-running Materialize recomputes the rows from current source state, which is
// the §8.3 refresh semantics: when a dependency note becomes stale (e.g. CRG
// fires source_mutation on a referenced symbol), the next Materialize surfaces
// the newly-affected rows.
func (v *View) Materialize(store sdk.Store) ([]sdk.Row, error) {
	merged, err := v.readMerged(store)
	if err != nil {
		return nil, err
	}
	rows, err := dsl.Eval(v.query, merged, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: evaluate view %q: %w", errPrefix, v.name, err)
	}
	if err := store.WriteNotes(v.Token(), v.consumer, rowsToNotes(v.name, rows)); err != nil {
		return nil, fmt.Errorf("%s: persist view %q: %w", errPrefix, v.name, err)
	}
	return rows, nil
}

// Compat is the outcome of the mechanical cutover gate (§10.3).
type Compat string

const (
	// CompatOK means the view's query still validates against the updated
	// dependency schema; the lockfile proceeds pending-rebuild → ready.
	CompatOK Compat = "compatible"
	// CompatDSLUpdateRequired means the bump removed or retyped something the
	// query reads; the dependent view blocks dependee activation until an
	// updated query ships (§10.3, O1 — no accepts_breaking_changes opt-out).
	CompatDSLUpdateRequired Compat = "dsl-update-required"
)

// CheckCompat re-validates the view's DSL query against an updated set of
// dependency schemas — the mechanical cutover gate of §10.3. A dependee schema
// bump that preserves every note/edge the query reads yields (CompatOK, nil). A
// bump that removes or retypes something the query reads yields
// (CompatDSLUpdateRequired, <the compile failure>): the error is the diagnostic
// reason, NOT a fatal error — the caller transitions the lockfile state on the
// returned Compat. The check is purely mechanical: the same ParseWithSchema the
// view compiled under, re-run against the new schema; there is no operator ack.
func (v *View) CheckCompat(updatedDeps []Namespace) (Compat, error) {
	if _, err := Compile(v.name, v.consumerNS, updatedDeps, v.crossEdges, v.querySrc); err != nil {
		return CompatDSLUpdateRequired, err
	}
	return CompatOK, nil
}

// readMerged reads the consumer namespace (own-read token) and each declared
// dependency (ViewToken) through the Store and unions their notes and edges into
// one in-memory view the DSL evaluator runs over.
func (v *View) readMerged(store sdk.Store) (sdk.NamespaceView, error) {
	var merged sdk.NamespaceView
	if err := appendNamespace(store, sdk.OwnReadToken(v.consumer, v.name), v.consumer, &merged); err != nil {
		return merged, err
	}
	tok := v.Token()
	for _, d := range v.deps {
		if err := appendNamespace(store, tok, d, &merged); err != nil {
			return merged, err
		}
	}
	return merged, nil
}

// appendNamespace reads ns's notes and edges with tok and appends them to merged.
func appendNamespace(store sdk.Store, tok sdk.Token, ns string, merged *sdk.NamespaceView) error {
	notes, err := store.Notes(tok, ns)
	if err != nil {
		return fmt.Errorf("%s: read notes from %q: %w", errPrefix, ns, err)
	}
	edges, err := store.Edges(tok, ns)
	if err != nil {
		return fmt.Errorf("%s: read edges from %q: %w", errPrefix, ns, err)
	}
	merged.Notes = append(merged.Notes, notes...)
	merged.Edges = append(merged.Edges, edges...)
	return nil
}

// combine merges the consumer and dependency schema slices plus the view's
// cross-namespace edges into one SchemaInfo, returning it alongside the
// note-type → owning-namespace map. It rejects a note type declared by more
// than one namespace (ambiguous resolution), a cross edge that collides with a
// declared edge, and a cross edge whose endpoints are not known note types.
func combine(consumer Namespace, deps []Namespace, crossEdges []CrossEdge) (dsl.SchemaInfo, map[string]string, error) {
	combined := dsl.SchemaInfo{
		NoteFields: map[string]map[string]dsl.FieldInfo{},
		Edges:      map[string]dsl.EdgeInfo{},
		MaxDepth:   consumer.Info.MaxDepth,
	}
	nsOf := map[string]string{}
	all := append([]Namespace{consumer}, deps...)
	for _, ns := range all {
		if err := mergeNamespace(&combined, nsOf, ns); err != nil {
			return dsl.SchemaInfo{}, nil, err
		}
	}
	if err := mergeCrossEdges(&combined, nsOf, crossEdges); err != nil {
		return dsl.SchemaInfo{}, nil, err
	}
	return combined, nsOf, nil
}

// mergeNamespace folds one namespace's note fields and edges into the combined
// schema, recording each note type's owning namespace and rejecting collisions.
func mergeNamespace(combined *dsl.SchemaInfo, nsOf map[string]string, ns Namespace) error {
	if ns.Info.MaxDepth > combined.MaxDepth {
		combined.MaxDepth = ns.Info.MaxDepth
	}
	for nt, fields := range ns.Info.NoteFields {
		if owner, dup := nsOf[nt]; dup {
			return fmt.Errorf("%s: note type %q declared by both %q and %q", errPrefix, nt, owner, ns.Name)
		}
		nsOf[nt] = ns.Name
		combined.NoteFields[nt] = fields
	}
	for et, ei := range ns.Info.Edges {
		if _, dup := combined.Edges[et]; dup {
			return fmt.Errorf("%s: edge type %q declared by more than one namespace", errPrefix, et)
		}
		combined.Edges[et] = ei
	}
	return nil
}

// mergeCrossEdges adds the view's cross-namespace edges to the combined schema,
// validating each one's name and endpoints against the merged note types.
func mergeCrossEdges(combined *dsl.SchemaInfo, nsOf map[string]string, crossEdges []CrossEdge) error {
	for _, ce := range crossEdges {
		if _, dup := combined.Edges[ce.Name]; dup {
			return fmt.Errorf("%s: cross edge %q collides with a declared edge", errPrefix, ce.Name)
		}
		if _, ok := nsOf[ce.From]; !ok {
			return fmt.Errorf("%s: cross edge %q from unknown note type %q", errPrefix, ce.Name, ce.From)
		}
		if _, ok := nsOf[ce.To]; !ok {
			return fmt.Errorf("%s: cross edge %q to unknown note type %q", errPrefix, ce.Name, ce.To)
		}
		combined.Edges[ce.Name] = dsl.EdgeInfo{From: ce.From, To: ce.To, Derivation: ce.Derivation}
	}
	return nil
}

// touchedNamespaces returns the sorted set of namespaces the query's MATCH
// clauses reference, resolved through nsOf. An untyped re-referenced alias
// (e.g. the second clause's `(e)`) resolves via the type fixed at its first
// typed binding.
func touchedNamespaces(q *dsl.Query, nsOf map[string]string) []string {
	aliasType := map[string]string{}
	for _, m := range q.Matches {
		for _, n := range m.Nodes {
			if n.Type != "" {
				aliasType[n.Alias] = n.Type
			}
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, typ := range aliasType {
		if ns, ok := nsOf[typ]; ok && !seen[ns] {
			seen[ns] = true
			out = append(out, ns)
		}
	}
	sort.Strings(out)
	return out
}

// rowsToNotes lowers the view's result rows into derived notes for persistence:
// one note per row, typed by the view name, fields copied from the row columns.
func rowsToNotes(view string, rows []sdk.Row) []sdk.Note {
	out := make([]sdk.Note, 0, len(rows))
	for i, r := range rows {
		fields := make(map[string]any, len(r))
		for k, val := range r {
			fields[k] = val
		}
		out = append(out, sdk.Note{ID: fmt.Sprintf("%s#%d", view, i), Type: view, Fields: fields})
	}
	return out
}
