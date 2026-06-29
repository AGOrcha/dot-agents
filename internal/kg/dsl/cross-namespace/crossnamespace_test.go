package crossnamespace_test

import (
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/kg/dsl"
	crossnamespace "github.com/AGOrcha/dot-agents/internal/kg/dsl/cross-namespace"
)

const (
	nsConsumer = "compliance-register"
	nsDep      = "crg"
	viewName   = "controls_with_changed_function_evidence"
	reasonKey  = "reason"
	staleKey   = "stale"

	// Note/edge/field vocabulary and fixture ids hoisted to consts so the
	// schema builders, seeders, and assertions share one spelling (S1192).
	typeSymbol   = "symbol"
	typeEvidence = "evidence"
	typeControl  = "control"
	typeString   = "string"
	fQualified   = "qualified_name"
	fControlID   = "control_id"
	fCollectedAt = "collected_at"
	eCitedBy     = "cited_by"
	eReferences  = "references"
	eCalls       = "CALLS"
	rSource      = "source"
	idEvMfa      = "ev-mfa"
	idEvRetain   = "ev-retain"
	idEvRbac     = "ev-rbac"
	idCtlMfa     = "ctl-mfa"
	acMfa        = "AC-2-MFA"
	drRetention  = "DR-1-RETENTION"
	qnLogin      = "auth.Login"
	qnRetain     = "data.Retain"
)

// viewQuery joins the consumer evidence→control citation with the cross-edge
// evidence→symbol reference, keeping symbols that carry the source-stale tag.
const viewQuery = `
	MATCH (e:evidence)-[:cited_by]->(c:control)
	MATCH (e)-[:references]->(s:symbol)
	WHERE s.stale.reason = 'source'
	RETURN c.control_id AS control_id, e.id AS evidence_id, s.qualified_name AS function`

// consumerNS builds the compliance-side namespace contribution: evidence and
// control note types plus the within-namespace cited_by edge.
func consumerNS(t *testing.T) crossnamespace.Namespace {
	t.Helper()
	info, err := dsl.NewSchemaInfo(
		[]dsl.NoteTypeDecl{
			{Name: typeEvidence, Fields: []dsl.FieldDecl{{Name: fCollectedAt, Type: "date"}}},
			{Name: typeControl, Fields: []dsl.FieldDecl{{Name: fControlID, Type: typeString}}},
		},
		[]dsl.EdgeTypeDecl{{Name: eCitedBy, From: typeEvidence, To: typeControl}},
		2,
	)
	if err != nil {
		t.Fatalf("consumer schema: %v", err)
	}
	return crossnamespace.Namespace{Name: nsConsumer, Info: info}
}

// depNS builds the CRG-side namespace contribution: the symbol note type plus
// the within-namespace CALLS edge. maxDepth lets a test exercise the combined
// max-depth bump branch.
func depNS(t *testing.T, maxDepth int) crossnamespace.Namespace {
	t.Helper()
	info, err := dsl.NewSchemaInfo(
		[]dsl.NoteTypeDecl{{Name: typeSymbol, Fields: []dsl.FieldDecl{{Name: fQualified, Type: typeString}}}},
		[]dsl.EdgeTypeDecl{{Name: eCalls, From: typeSymbol, To: typeSymbol}},
		maxDepth,
	)
	if err != nil {
		t.Fatalf("dep schema: %v", err)
	}
	return crossnamespace.Namespace{Name: nsDep, Info: info}
}

func referencesEdge() crossnamespace.CrossEdge {
	return crossnamespace.CrossEdge{Name: eReferences, From: typeEvidence, To: typeSymbol}
}

// buildView compiles the canonical cross-namespace view used across the tests.
func buildView(t *testing.T) *crossnamespace.View {
	t.Helper()
	v, err := crossnamespace.Compile(viewName, consumerNS(t), []crossnamespace.Namespace{depNS(t, 3)},
		[]crossnamespace.CrossEdge{referencesEdge()}, viewQuery)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return v
}

func TestCompileMetadata(t *testing.T) {
	v := buildView(t)
	if v.Name() != viewName {
		t.Errorf("Name = %q, want %q", v.Name(), viewName)
	}
	if v.Consumer() != nsConsumer {
		t.Errorf("Consumer = %q, want %q", v.Consumer(), nsConsumer)
	}
	if got := v.Deps(); !reflect.DeepEqual(got, []string{nsDep}) {
		t.Errorf("Deps = %v, want [%q]", got, nsDep)
	}
	// §8.2 compiler obligation: the query touches both namespaces.
	if got := v.TouchedNamespaces(); !reflect.DeepEqual(got, []string{nsConsumer, nsDep}) {
		t.Errorf("TouchedNamespaces = %v, want [%q %q]", got, nsConsumer, nsDep)
	}
}

// TestViewTokenDerivation covers N3 (single dep) and N4 (two deps): the view
// token grants write on the consumer plus read on each declared dependency.
func TestViewTokenDerivation(t *testing.T) {
	t.Run("N3_single_dep", func(t *testing.T) {
		tok := buildView(t).Token()
		assertGrants(t, tok, []sdk.Grant{
			{Namespace: nsConsumer, Mode: sdk.ModeWrite},
			{Namespace: nsDep, Mode: sdk.ModeRead},
		})
	})
	t.Run("N4_two_deps", func(t *testing.T) {
		second := crossnamespace.Namespace{Name: "citation", Info: mustInfo(t,
			[]dsl.NoteTypeDecl{{Name: "claim", Fields: []dsl.FieldDecl{{Name: "text", Type: typeString}}}}, nil, 1)}
		v, err := crossnamespace.Compile(viewName, consumerNS(t),
			[]crossnamespace.Namespace{depNS(t, 3), second},
			[]crossnamespace.CrossEdge{referencesEdge()}, viewQuery)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		assertGrants(t, v.Token(), []sdk.Grant{
			{Namespace: nsConsumer, Mode: sdk.ModeWrite},
			{Namespace: nsDep, Mode: sdk.ModeRead},
			{Namespace: "citation", Mode: sdk.ModeRead},
		})
	})
}

func TestCompileErrors(t *testing.T) {
	tests := []struct {
		name    string
		deps    []crossnamespace.Namespace
		cross   []crossnamespace.CrossEdge
		query   string
		wantSub string
	}{
		{
			name:    "N6_unknown_note_type_undeclared_dep",
			deps:    nil,
			cross:   nil,
			query:   "MATCH (s:symbol) RETURN s.qualified_name",
			wantSub: "unknown note type",
		},
		{
			name:    "N6_cross_edge_to_undeclared_namespace",
			deps:    nil, // crg not declared, so its symbol type is unknown
			cross:   []crossnamespace.CrossEdge{referencesEdge()},
			query:   viewQuery,
			wantSub: `cross edge "references" to unknown note type "symbol"`,
		},
		{
			name:    "cross_edge_from_unknown",
			deps:    []crossnamespace.Namespace{depNS(t, 3)},
			cross:   []crossnamespace.CrossEdge{{Name: eReferences, From: "nope", To: typeSymbol}},
			query:   viewQuery,
			wantSub: `from unknown note type "nope"`,
		},
		{
			name:    "cross_edge_name_collides_with_declared_edge",
			deps:    []crossnamespace.Namespace{depNS(t, 3)},
			cross:   []crossnamespace.CrossEdge{{Name: eCitedBy, From: typeEvidence, To: typeSymbol}},
			query:   viewQuery,
			wantSub: "collides with a declared edge",
		},
		{
			name:    "note_type_declared_by_two_namespaces",
			deps:    []crossnamespace.Namespace{collidingDep(t)},
			cross:   []crossnamespace.CrossEdge{referencesEdge()},
			query:   viewQuery,
			wantSub: "declared by both",
		},
		{
			name:    "edge_declared_by_two_namespaces",
			deps:    []crossnamespace.Namespace{collidingEdgeDep(t)},
			cross:   []crossnamespace.CrossEdge{referencesEdge()},
			query:   viewQuery,
			wantSub: "declared by more than one namespace",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := crossnamespace.Compile(viewName, consumerNS(t), tt.deps, tt.cross, tt.query)
			if err == nil {
				t.Fatalf("Compile: expected error containing %q, got nil", tt.wantSub)
			}
			if !contains(err.Error(), tt.wantSub) {
				t.Fatalf("Compile error = %q, want substring %q", err, tt.wantSub)
			}
		})
	}
}

// TestMaterialize is the §8.3 hard test: the view surfaces exactly the controls
// whose cited evidence references a source-stale CRG symbol, and re-running it
// after a new stale symbol arrives refreshes the result (§8.3 refresh).
func TestMaterialize(t *testing.T) {
	store := sdk.NewMemStore()
	seedConsumer(t, store)
	seedDep(t, store)
	v := buildView(t)

	rows, err := v.Materialize(store)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	assertRows(t, rows, [][3]string{
		{acMfa, idEvMfa, qnLogin},
		{drRetention, idEvRetain, qnRetain},
	})

	// The rows persist as derived notes in the CONSUMER namespace.
	persisted, err := store.Notes(sdk.OwnReadToken(nsConsumer, viewName), nsConsumer)
	if err != nil {
		t.Fatalf("read persisted: %v", err)
	}
	if got := countType(persisted, viewName); got != len(rows) {
		t.Errorf("persisted derived notes = %d, want %d", got, len(rows))
	}

	// Refresh: a newly source-stale symbol cited by a control surfaces on the
	// next Materialize.
	addStaleSymbol(t, store, "sym:net-call", "net.Call")
	addCitation(t, store, "ev-net", idCtlMfa, "sym:net-call")
	rows2, err := v.Materialize(store)
	if err != nil {
		t.Fatalf("Materialize refresh: %v", err)
	}
	assertRows(t, rows2, [][3]string{
		{acMfa, idEvMfa, qnLogin},
		{acMfa, "ev-net", "net.Call"},
		{drRetention, idEvRetain, qnRetain},
	})
}

// TestMaterializeStoreErrors covers the read and write error branches via a
// store stub that fails a chosen operation.
func TestMaterializeStoreErrors(t *testing.T) {
	t.Run("read_failure", func(t *testing.T) {
		store := &failStore{base: seededStore(t), failNotesNS: nsDep}
		if _, err := buildView(t).Materialize(store); err == nil || !contains(err.Error(), "read notes from") {
			t.Fatalf("Materialize: want read-notes error, got %v", err)
		}
	})
	t.Run("read_edges_failure", func(t *testing.T) {
		store := &failStore{base: seededStore(t), failEdgesNS: nsConsumer}
		if _, err := buildView(t).Materialize(store); err == nil || !contains(err.Error(), "read edges from") {
			t.Fatalf("Materialize: want read-edges error, got %v", err)
		}
	})
	t.Run("write_failure", func(t *testing.T) {
		store := &failStore{base: seededStore(t), failWrite: true}
		if _, err := buildView(t).Materialize(store); err == nil || !contains(err.Error(), "persist view") {
			t.Fatalf("Materialize: want persist error, got %v", err)
		}
	})
}

// TestCrossAdapterWriteRejected is N8: a cross-adapter write attempted with the
// view token is rejected at the storage layer — the token grants only read on
// the dependency.
func TestCrossAdapterWriteRejected(t *testing.T) {
	store := sdk.NewMemStore()
	tok := buildView(t).Token()
	err := store.WriteNotes(tok, nsDep, []sdk.Note{{ID: "x", Type: typeSymbol}})
	if err == nil || !contains(err.Error(), "does not authorize write") {
		t.Fatalf("cross-adapter write: want storage rejection, got %v", err)
	}
}

// TestDefenseInDepthReadRejected is N9: the view token does not grant a
// namespace outside its reads_from set, and the store rejects a read of it.
func TestDefenseInDepthReadRejected(t *testing.T) {
	store := sdk.NewMemStore()
	tok := buildView(t).Token()
	if _, err := store.Notes(tok, "unrelated"); err == nil || !contains(err.Error(), "does not authorize read") {
		t.Fatalf("defense-in-depth read: want storage rejection, got %v", err)
	}
}

// TestCheckCompat is the §10.3 mechanical cutover gate: a backward-compatible
// CRG bump keeps the view valid (CompatOK); an incompatible bump (the symbol
// note type renamed) blocks with CompatDSLUpdateRequired.
func TestCheckCompat(t *testing.T) {
	v := buildView(t)

	t.Run("compatible_bump", func(t *testing.T) {
		// Adds a field to symbol — the query's reads still resolve.
		bumped, err := dsl.NewSchemaInfo(
			[]dsl.NoteTypeDecl{{Name: typeSymbol, Fields: []dsl.FieldDecl{
				{Name: fQualified, Type: typeString}, {Name: "visibility", Type: typeString}}}},
			[]dsl.EdgeTypeDecl{{Name: eCalls, From: typeSymbol, To: typeSymbol}}, 3)
		if err != nil {
			t.Fatalf("bumped schema: %v", err)
		}
		state, cerr := v.CheckCompat([]crossnamespace.Namespace{{Name: nsDep, Info: bumped}})
		if state != crossnamespace.CompatOK || cerr != nil {
			t.Fatalf("CheckCompat = (%s, %v), want (compatible, nil)", state, cerr)
		}
	})

	t.Run("incompatible_bump", func(t *testing.T) {
		// Renames symbol → node: the cross edge endpoint and the MATCH no longer
		// resolve, so the view query fails to validate.
		renamed, err := dsl.NewSchemaInfo(
			[]dsl.NoteTypeDecl{{Name: "node", Fields: []dsl.FieldDecl{{Name: fQualified, Type: typeString}}}},
			[]dsl.EdgeTypeDecl{{Name: eCalls, From: "node", To: "node"}}, 3)
		if err != nil {
			t.Fatalf("renamed schema: %v", err)
		}
		state, cerr := v.CheckCompat([]crossnamespace.Namespace{{Name: nsDep, Info: renamed}})
		if state != crossnamespace.CompatDSLUpdateRequired {
			t.Fatalf("CheckCompat state = %s, want dsl-update-required", state)
		}
		if cerr == nil {
			t.Fatal("CheckCompat: expected a diagnostic compile error on dsl-update-required")
		}
	})
}

// --- helpers ------------------------------------------------------------------

func mustInfo(t *testing.T, notes []dsl.NoteTypeDecl, edges []dsl.EdgeTypeDecl, maxDepth int) dsl.SchemaInfo {
	t.Helper()
	info, err := dsl.NewSchemaInfo(notes, edges, maxDepth)
	if err != nil {
		t.Fatalf("schema info: %v", err)
	}
	return info
}

// collidingDep is a CRG-side namespace that also declares the consumer's
// `control` note type, triggering the note-type collision check.
func collidingDep(t *testing.T) crossnamespace.Namespace {
	t.Helper()
	return crossnamespace.Namespace{Name: nsDep, Info: mustInfo(t,
		[]dsl.NoteTypeDecl{
			{Name: typeSymbol, Fields: []dsl.FieldDecl{{Name: fQualified, Type: typeString}}},
			{Name: typeControl, Fields: []dsl.FieldDecl{{Name: fControlID, Type: typeString}}},
		},
		[]dsl.EdgeTypeDecl{{Name: eCalls, From: typeSymbol, To: typeSymbol}}, 3)}
}

// collidingEdgeDep declares the consumer's `cited_by` edge, triggering the
// edge-type collision check.
func collidingEdgeDep(t *testing.T) crossnamespace.Namespace {
	t.Helper()
	return crossnamespace.Namespace{Name: nsDep, Info: mustInfo(t,
		[]dsl.NoteTypeDecl{{Name: typeSymbol, Fields: []dsl.FieldDecl{{Name: fQualified, Type: typeString}}}},
		[]dsl.EdgeTypeDecl{
			{Name: eCalls, From: typeSymbol, To: typeSymbol},
			{Name: eCitedBy, From: typeSymbol, To: typeSymbol},
		}, 3)}
}

func seededStore(t *testing.T) *sdk.MemStore {
	t.Helper()
	store := sdk.NewMemStore()
	seedConsumer(t, store)
	seedDep(t, store)
	return store
}

// seedConsumer writes the compliance corpus (controls, evidence, cited_by, and
// the cross-namespace references edges) into the consumer namespace.
func seedConsumer(t *testing.T, store sdk.Store) {
	t.Helper()
	s := sdk.For(nsConsumer, store)
	must(t, s.WriteNotes([]sdk.Note{
		{ID: idCtlMfa, Type: typeControl, Fields: map[string]any{fControlID: acMfa}},
		{ID: "ctl-rbac", Type: typeControl, Fields: map[string]any{fControlID: "AC-3-RBAC"}},
		{ID: "ctl-retention", Type: typeControl, Fields: map[string]any{fControlID: drRetention}},
		{ID: idEvMfa, Type: typeEvidence, Fields: map[string]any{fCollectedAt: "2025-02-01"}},
		{ID: idEvRbac, Type: typeEvidence, Fields: map[string]any{fCollectedAt: "2025-03-01"}},
		{ID: idEvRetain, Type: typeEvidence, Fields: map[string]any{fCollectedAt: "2025-04-01"}},
	}))
	must(t, s.WriteEdges([]sdk.Edge{
		{Type: eCitedBy, From: idEvMfa, To: idCtlMfa},
		{Type: eCitedBy, From: idEvRbac, To: "ctl-rbac"},
		{Type: eCitedBy, From: idEvRetain, To: "ctl-retention"},
		{Type: eReferences, From: idEvMfa, To: "sym:auth-login"},
		{Type: eReferences, From: idEvRbac, To: "sym:auth-logout"},
		{Type: eReferences, From: idEvRetain, To: "sym:data-retain"},
	}))
}

// seedDep writes CRG symbols into the dep namespace: two source-stale, one fresh.
func seedDep(t *testing.T, store sdk.Store) {
	t.Helper()
	s := sdk.For(nsDep, store)
	must(t, s.WriteNotes([]sdk.Note{
		staleSymbol("sym:auth-login", qnLogin),
		{ID: "sym:auth-logout", Type: typeSymbol, Fields: map[string]any{fQualified: "auth.Logout"}},
		staleSymbol("sym:data-retain", qnRetain),
	}))
}

func staleSymbol(id, qn string) sdk.Note {
	return sdk.Note{ID: id, Type: typeSymbol, Fields: map[string]any{
		fQualified: qn,
		staleKey:   map[string]any{reasonKey: rSource},
	}}
}

func addStaleSymbol(t *testing.T, store sdk.Store, id, qn string) {
	t.Helper()
	must(t, sdk.For(nsDep, store).WriteNotes([]sdk.Note{staleSymbol(id, qn)}))
}

func addCitation(t *testing.T, store sdk.Store, ev, ctl, sym string) {
	t.Helper()
	s := sdk.For(nsConsumer, store)
	must(t, s.WriteNotes([]sdk.Note{{ID: ev, Type: typeEvidence, Fields: map[string]any{}}}))
	must(t, s.WriteEdges([]sdk.Edge{
		{Type: eCitedBy, From: ev, To: ctl},
		{Type: eReferences, From: ev, To: sym},
	}))
}

func assertGrants(t *testing.T, tok sdk.Token, want []sdk.Grant) {
	t.Helper()
	got := append([]sdk.Grant(nil), tok.Authorized...)
	sortGrants(got)
	w := append([]sdk.Grant(nil), want...)
	sortGrants(w)
	if !reflect.DeepEqual(got, w) {
		t.Fatalf("token grants = %v, want %v", got, w)
	}
}

func sortGrants(g []sdk.Grant) {
	sort.Slice(g, func(i, j int) bool {
		if g[i].Namespace != g[j].Namespace {
			return g[i].Namespace < g[j].Namespace
		}
		return g[i].Mode < g[j].Mode
	})
}

// assertRows checks the result rows equal want as (control_id, evidence_id,
// function) triples, order-insensitively.
func assertRows(t *testing.T, rows []sdk.Row, want [][3]string) {
	t.Helper()
	got := make([][3]string, 0, len(rows))
	for _, r := range rows {
		got = append(got, [3]string{str(r[fControlID]), str(r["evidence_id"]), str(r["function"])})
	}
	sortTriples(got)
	w := append([][3]string(nil), want...)
	sortTriples(w)
	if !reflect.DeepEqual(got, w) {
		t.Fatalf("rows = %v, want %v", got, w)
	}
}

func sortTriples(s [][3]string) {
	sort.Slice(s, func(i, j int) bool {
		for k := 0; k < 3; k++ {
			if s[i][k] != s[j][k] {
				return s[i][k] < s[j][k]
			}
		}
		return false
	})
}

func countType(notes []sdk.Note, typ string) int {
	n := 0
	for _, note := range notes {
		if note.Type == typ {
			n++
		}
	}
	return n
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
}

// failStore wraps a MemStore and fails a chosen operation, to exercise
// Materialize's error branches.
type failStore struct {
	base        *sdk.MemStore
	failNotesNS string
	failEdgesNS string
	failWrite   bool
}

var errInjected = errors.New("injected store failure")

func (f *failStore) WriteNotes(tok sdk.Token, ns string, notes []sdk.Note) error {
	if f.failWrite {
		return errInjected
	}
	return f.base.WriteNotes(tok, ns, notes)
}

func (f *failStore) WriteEdges(tok sdk.Token, ns string, edges []sdk.Edge) error {
	return f.base.WriteEdges(tok, ns, edges)
}

func (f *failStore) Notes(tok sdk.Token, ns string) ([]sdk.Note, error) {
	if ns == f.failNotesNS {
		return nil, errInjected
	}
	return f.base.Notes(tok, ns)
}

func (f *failStore) Edges(tok sdk.Token, ns string) ([]sdk.Edge, error) {
	if ns == f.failEdgesNS {
		return nil, errInjected
	}
	return f.base.Edges(tok, ns)
}
