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
	combined   dsl.SchemaInfo
	consumerNS Namespace
	depNS      []Namespace
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
		combined:   combined,
		consumerNS: consumer,
		depNS:      append([]Namespace(nil), deps...),
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

// NoteReplacer is the storage capability Materialize needs beyond the
// append-only sdk.Store: it REBUILDS a view's derived rows in place rather than
// accumulating them. A materialized view is a function of current source state,
// so a refresh must converge the persisted derived notes to exactly the current
// result set — re-appending (the bare sdk.Store behavior) would leave duplicate
// and orphaned rows from prior refreshes. A production gcc-backed store provides
// this through upsert/delete; the in-memory RebuildStore in this package does
// too. Materialize requires it for the persist step and refuses to silently
// corrupt persisted state through an append-only store.
type NoteReplacer interface {
	// ReplaceNotes removes every note of noteType in ns (token-checked write)
	// and writes notes in their place, so the persisted set of that type equals
	// notes exactly.
	ReplaceNotes(token sdk.Token, ns, noteType string, notes []sdk.Note) error
}

// Materialize reads every authorized namespace through the token-checked Store,
// merges them into one in-memory view, evaluates the view query, and REBUILDS
// the view's derived notes (type = view name) in the CONSUMER namespace,
// returning the rows. Dependency reads and the derived-note write all carry the
// §8.2 ViewToken, so a write to a dependency is rejected at the Store layer (N8)
// and a read of an unauthorized namespace is rejected too (N9).
//
// Re-running Materialize recomputes the rows from current source state and
// replaces the prior derived set (§8.3 refresh): when a dependency note becomes
// stale (e.g. CRG fires source_mutation on a referenced symbol), the next
// Materialize surfaces the newly-affected rows and drops any that no longer
// hold — the persisted state converges to the current result set, never
// accumulates. The store MUST implement NoteReplacer; an append-only store is
// rejected rather than silently orphaning rows.
func (v *View) Materialize(store sdk.Store) ([]sdk.Row, error) {
	rebuilder, ok := store.(NoteReplacer)
	if !ok {
		return nil, fmt.Errorf("%s: view %q requires a rebuild-capable store (NoteReplacer); an append-only store would orphan prior derived rows", errPrefix, v.name)
	}
	merged, err := v.readMerged(store)
	if err != nil {
		return nil, err
	}
	rows, err := dsl.Eval(v.query, merged, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: evaluate view %q: %w", errPrefix, v.name, err)
	}
	if err := rebuilder.ReplaceNotes(v.Token(), v.consumer, v.name, rowsToNotes(v.name, rows)); err != nil {
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

// CheckCompat validates the view's DSL query against an updated set of
// dependency schemas — the mechanical cutover gate of §10.3. It returns
// (CompatOK, nil) only when the bump preserves both the EXISTENCE and the
// SIGNATURE of every note type, edge, and field the query reads; otherwise
// (CompatDSLUpdateRequired, <diagnostic>). The error on the
// dsl-update-required path is the reason, NOT a fatal error — the caller
// transitions the lockfile state on the returned Compat. There is no operator
// ack (O1).
//
// The gate is two-stage because re-parsing alone is insufficient (§10.3 requires
// SIGNATURE compatibility, not just resolvability):
//
//  1. Re-parse the query against the new schema. This rejects a removed note
//     type, a removed referenced field, or a changed edge direction — the parser
//     only accepts a field that still exists.
//  2. Compare the TYPE of every field the query references between the old and
//     new schema. The parser accepts a field once it exists regardless of type,
//     so a field whose type changed (e.g. symbol.qualified_name string→int)
//     passes step 1 but is a breaking change — step 2 catches it.
func (v *View) CheckCompat(updatedDeps []Namespace) (Compat, error) {
	updated, err := Compile(v.name, v.consumerNS, updatedDeps, v.crossEdges, v.querySrc)
	if err != nil {
		return CompatDSLUpdateRequired, err
	}
	if changed := signatureMismatch(v.query, v.combined, updated.combined); changed != "" {
		return CompatDSLUpdateRequired, fmt.Errorf("%s: view %q field %s changed signature in the dependency bump", errPrefix, v.name, changed)
	}
	return CompatOK, nil
}

// signatureMismatch returns the first referenced field whose type differs (or
// vanished) between the old and new combined schemas, or "" when every
// referenced field keeps its signature. It is the §10.3 signature-compatibility
// check the re-parse cannot perform (the parser accepts a field by existence,
// not by type).
func signatureMismatch(q *dsl.Query, oldInfo, newInfo dsl.SchemaInfo) string {
	aliasType := queryAliasTypes(q)
	for _, ref := range referencedFieldRefs(q) {
		key, oldSig, ok := terminalFieldType(ref, aliasType, oldInfo)
		if !ok {
			continue // intrinsic id, stale selector, or bare ref — no schema field
		}
		_, newSig, ok := terminalFieldType(ref, aliasType, newInfo)
		if !ok || newSig[1] != oldSig[1] {
			return key
		}
	}
	return ""
}

// queryAliasTypes maps each node alias to the note type fixed at its first typed
// MATCH binding (an untyped re-reference inherits that type).
func queryAliasTypes(q *dsl.Query) map[string]string {
	aliasType := map[string]string{}
	for _, m := range q.Matches {
		for _, n := range m.Nodes {
			if n.Type != "" {
				aliasType[n.Alias] = n.Type
			}
		}
	}
	return aliasType
}

// referencedFieldRefs collects every field reference the query reads — WHERE
// predicate left-hand sides and RETURN items (including nested function args) —
// so each can be checked for signature drift.
func referencedFieldRefs(q *dsl.Query) []dsl.FieldRef {
	var refs []dsl.FieldRef
	for _, pred := range q.Where {
		refs = append(refs, pred.Left)
	}
	for _, item := range q.Returns {
		refs = appendReturnRefs(refs, item)
	}
	return refs
}

// appendReturnRefs adds a RETURN item's field ref plus any refs nested in its
// function args (e.g. min(c.field)).
func appendReturnRefs(refs []dsl.FieldRef, item dsl.ReturnItem) []dsl.FieldRef {
	if item.Ref.Alias != "" {
		refs = append(refs, item.Ref)
	}
	for _, arg := range item.FuncArgs {
		if arg.Ref.Alias != "" {
			refs = append(refs, arg.Ref)
		}
	}
	return refs
}

// terminalFieldType resolves a field ref to its terminal (noteType, field) and
// that field's declared type in info, walking ref hops. It returns
// (key, [noteType.field, type], true) for a real schema field, or ok=false for a
// bare alias, an intrinsic id, a structured stale selector, or any path that
// does not resolve to a declared field (the parser handles those cases). The
// returned key is the stable "noteType.field" identity; sig[1] is the type.
func terminalFieldType(ref dsl.FieldRef, aliasType map[string]string, info dsl.SchemaInfo) (string, [2]string, bool) {
	cur, ok := aliasType[ref.Alias]
	if !ok || len(ref.Path) == 0 {
		return "", [2]string{}, false
	}
	for i, part := range ref.Path {
		if part == "stale" {
			return "", [2]string{}, false // structured stale selector, not a schema field
		}
		fields, ok := info.NoteFields[cur]
		if !ok {
			return "", [2]string{}, false
		}
		field, ok := fields[part]
		if !ok {
			return "", [2]string{}, false // intrinsic id terminal or unknown field
		}
		if i == len(ref.Path)-1 {
			key := cur + "." + part
			return key, [2]string{key, field.Type}, true
		}
		if field.RefType == "" {
			return "", [2]string{}, false
		}
		cur = field.RefType
	}
	return "", [2]string{}, false
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

// RebuildStore is an in-memory sdk.Store that also implements NoteReplacer, so
// a materialized view can REBUILD its derived rows on refresh instead of
// accumulating them (the bug the bare append-only sdk.MemStore has). It enforces
// the same §8.2 namespace-token contract as sdk.MemStore: every operation's
// (namespace, mode) must be authorized by the supplied token. The zero value is
// not usable; construct with NewRebuildStore. It is the cross-namespace view
// substrate for tests and the reference replace-on-refresh semantics a
// production gcc-backed store provides via upsert/delete.
type RebuildStore struct {
	notes map[string][]sdk.Note
	edges map[string][]sdk.Edge
}

// NewRebuildStore returns an empty rebuild-capable store.
func NewRebuildStore() *RebuildStore {
	return &RebuildStore{notes: map[string][]sdk.Note{}, edges: map[string][]sdk.Edge{}}
}

// authorizeStore reports whether tok grants mode on ns (§8.2), shared by every
// RebuildStore method so the rejection message is written once.
func authorizeStore(tok sdk.Token, ns string, mode sdk.Mode) error {
	for _, g := range tok.Authorized {
		if g.Namespace == ns && g.Mode == mode {
			return nil
		}
	}
	return fmt.Errorf("storage: token issued_for %q does not authorize %s of namespace %q", tok.IssuedFor, mode, ns)
}

// WriteNotes appends notes to ns after write authorization (§8.2).
func (s *RebuildStore) WriteNotes(tok sdk.Token, ns string, notes []sdk.Note) error {
	if err := authorizeStore(tok, ns, sdk.ModeWrite); err != nil {
		return err
	}
	s.notes[ns] = append(s.notes[ns], notes...)
	return nil
}

// WriteEdges appends edges to ns after write authorization (§8.2).
func (s *RebuildStore) WriteEdges(tok sdk.Token, ns string, edges []sdk.Edge) error {
	if err := authorizeStore(tok, ns, sdk.ModeWrite); err != nil {
		return err
	}
	s.edges[ns] = append(s.edges[ns], edges...)
	return nil
}

// Notes returns a copy of ns's notes after read authorization (§8.2).
func (s *RebuildStore) Notes(tok sdk.Token, ns string) ([]sdk.Note, error) {
	if err := authorizeStore(tok, ns, sdk.ModeRead); err != nil {
		return nil, err
	}
	out := make([]sdk.Note, len(s.notes[ns]))
	copy(out, s.notes[ns])
	return out, nil
}

// Edges returns a copy of ns's edges after read authorization (§8.2).
func (s *RebuildStore) Edges(tok sdk.Token, ns string) ([]sdk.Edge, error) {
	if err := authorizeStore(tok, ns, sdk.ModeRead); err != nil {
		return nil, err
	}
	out := make([]sdk.Edge, len(s.edges[ns]))
	copy(out, s.edges[ns])
	return out, nil
}

// ReplaceNotes rebuilds the noteType slice of ns: it drops every existing note
// of noteType and writes notes in their place (write-authorized), so the
// persisted set of that type converges to notes exactly (NoteReplacer).
func (s *RebuildStore) ReplaceNotes(tok sdk.Token, ns, noteType string, notes []sdk.Note) error {
	if err := authorizeStore(tok, ns, sdk.ModeWrite); err != nil {
		return err
	}
	kept := make([]sdk.Note, 0, len(s.notes[ns]))
	for _, n := range s.notes[ns] {
		if n.Type != noteType {
			kept = append(kept, n)
		}
	}
	s.notes[ns] = append(kept, notes...)
	return nil
}
