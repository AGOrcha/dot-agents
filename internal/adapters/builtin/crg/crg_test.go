package crg

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/adapters/sdk"
	"github.com/AGOrcha/dot-agents/internal/graphstore"
	"github.com/AGOrcha/dot-agents/internal/kg/registry"
)

// Shared test literals (hoisted to consts to avoid S1192 duplication).
const (
	kindFn    = "Function"
	kindType  = "Type"
	kindCalls = "CALLS"
)

func TestSchemaLoadsAndValidates(t *testing.T) {
	s := New().Schema()
	if s.Name != Name {
		t.Fatalf("name = %q, want %q", s.Name, Name)
	}
	if len(s.NoteTypes) != 1 || s.NoteTypes[0].Name != "symbol" {
		t.Fatalf("expected one `symbol` note type, got %+v", s.NoteTypes)
	}
	if len(s.EdgeTypes) != 3 {
		t.Fatalf("expected 3 edge types (CALLS/TESTED_BY/IMPORTS), got %d", len(s.EdgeTypes))
	}
	if len(s.StalenessDrivers) != 1 || s.StalenessDrivers[0] != "source_mutation" {
		t.Fatalf("expected staleness_drivers [source_mutation] (O5), got %v", s.StalenessDrivers)
	}
	if s.MigrationOnly {
		t.Fatal("kg-native crg adapter must NOT be migration_only")
	}
}

func TestRegisterAndResolve(t *testing.T) {
	reg := registry.New()
	if err := Register(reg); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := reg.Resolve("dotagents-builtin:graph/crg@^1.0"); err != nil {
		t.Fatalf("resolve: %v", err)
	}
}

// TestBootstrapSnapshotReadsBackFromStore proves the snapshot is computed from
// the persisted namespace (readback), NOT the input corpus: a corpus with a
// dangling reference (dropped on write) must not be counted in EdgesByKind.
func TestBootstrapSnapshotReadsBackFromStore(t *testing.T) {
	c := Corpus{
		Commit: "deadbeef",
		Symbols: []Symbol{
			{QualifiedName: "pkg.A", Kind: kindFn, Language: "go", FilePath: "a.go", LineStart: 1, ContentHash: "h1"},
			{QualifiedName: "pkg.B", Kind: kindType, Language: "go", FilePath: "b.go", LineStart: 2, ContentHash: "h2"},
		},
		References: []Reference{
			{Kind: kindCalls, From: "pkg.A", To: "pkg.B"}, // real edge
			{Kind: kindCalls, From: "pkg.A", To: "ghost"}, // dangling — dropped on write
		},
	}
	store := sdk.NewMemStore()
	s := sdk.For(Name, store)
	snap, err := Bootstrap(s, store, c, nil)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if snap.Adapter != Name {
		t.Fatalf("snapshot adapter = %q, want %q", snap.Adapter, Name)
	}
	if snap.NodesTotal != 2 {
		t.Fatalf("nodes total = %d, want 2 (from readback)", snap.NodesTotal)
	}
	// Crucial: the dangling reference must NOT be counted — only 1 CALLS edge
	// actually reached storage.
	if snap.EdgesByKind[kindCalls] != 1 {
		t.Fatalf("EdgesByKind[CALLS] = %d, want 1 (dangling edge dropped on write, absent from readback)",
			snap.EdgesByKind[kindCalls])
	}
	ns := store.Namespaces()
	if len(ns) != 1 || ns[0] != Name {
		t.Fatalf("namespaces = %v, want exactly [%s]", ns, Name)
	}
}

// TestSourceMutationFiresOnContentHashChange proves O5: the driver fires for new
// and content-changed symbols, and does NOT fire for unchanged ones.
func TestSourceMutationFiresOnContentHashChange(t *testing.T) {
	prev := []sdk.Note{
		{ID: "pkg.A@a.go", Type: noteTypeSymbol, Fields: map[string]any{fieldContentSum: "h1"}},
		{ID: "pkg.B@b.go", Type: noteTypeSymbol, Fields: map[string]any{fieldContentSum: "h2"}},
	}
	cur := Corpus{
		Commit: "c2",
		Symbols: []Symbol{
			{QualifiedName: "pkg.A", Kind: kindFn, Language: "go", FilePath: "a.go", LineStart: 1, ContentHash: "h1"},     // unchanged
			{QualifiedName: "pkg.B", Kind: kindFn, Language: "go", FilePath: "b.go", LineStart: 2, ContentHash: "h2-NEW"}, // changed
			{QualifiedName: "pkg.C", Kind: kindFn, Language: "go", FilePath: "c.go", LineStart: 3, ContentHash: "h3"},     // new
		},
	}
	store := sdk.NewMemStore()
	s := sdk.For(Name, store)
	if _, err := Bootstrap(s, store, cur, prev); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	fired := s.FiredPredicates()
	if len(fired) != 2 {
		t.Fatalf("source_mutation fired %d times, want 2 (changed + new only); got %v", len(fired), fired)
	}
	firedIDs := map[string]bool{}
	for _, fp := range fired {
		if fp.Predicate != driverSourceMutation {
			t.Fatalf("unexpected predicate %q", fp.Predicate)
		}
		firedIDs[fp.Args["id"].(string)] = true
	}
	if firedIDs["pkg.A@a.go"] {
		t.Fatal("unchanged symbol pkg.A must NOT fire source_mutation (O5)")
	}
	if !firedIDs["pkg.B@b.go"] || !firedIDs["pkg.C@c.go"] {
		t.Fatalf("changed+new symbols must fire; fired ids = %v", firedIDs)
	}
}

func TestSourceMutation_FirstBootstrapFiresAll(t *testing.T) {
	c := smallCorpus()
	store := sdk.NewMemStore()
	s := sdk.For(Name, store)
	if _, err := Bootstrap(s, store, c, nil); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if got := len(s.FiredPredicates()); got != len(c.Symbols) {
		t.Fatalf("first bootstrap fired %d, want %d (all symbols new)", got, len(c.Symbols))
	}
}

func TestDiffFromStore_InsertUpdateDelete(t *testing.T) {
	prev := []sdk.Note{
		note("a@a.go", "a", kindFn, "a.go", "h1"),
		note("b@b.go", "b", kindFn, "b.go", "h2"),
	}
	store := sdk.NewMemStore()
	s := sdk.For(Name, store)
	cur := Corpus{Symbols: []Symbol{
		{QualifiedName: "a", Kind: kindFn, FilePath: "a.go", LineStart: 1, ContentHash: "h1-NEW"}, // update
		{QualifiedName: "c", Kind: kindType, FilePath: "c.go", LineStart: 3, ContentHash: "h3"},   // insert
		// b removed → delete
	}}
	notes, edges := cur.ToGraph()
	if err := s.WriteNotes(notes); err != nil {
		t.Fatalf("seed notes: %v", err)
	}
	if err := s.WriteEdges(edges); err != nil {
		t.Fatalf("seed edges: %v", err)
	}
	got, err := DiffFromStore(prev, store, Name)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	ops := map[graphstore.UpsertOp]int{}
	for _, u := range got {
		ops[u.Op]++
	}
	if ops[graphstore.OpInsert] != 1 || ops[graphstore.OpUpdate] != 1 || ops[graphstore.OpDelete] != 1 {
		t.Fatalf("expected 1 each insert/update/delete, got %+v (%v)", ops, got)
	}
}

func TestImpactRadiusFromStore_ExpandsAlongEdges(t *testing.T) {
	c := Corpus{
		Symbols: []Symbol{
			{QualifiedName: "a", Kind: kindFn, FilePath: "a.go"},
			{QualifiedName: "b", Kind: kindFn, FilePath: "b.go"},
			{QualifiedName: "d", Kind: kindFn, FilePath: "d.go"},
			{QualifiedName: "iso", Kind: kindType, FilePath: "iso.go"},
		},
		References: []Reference{
			{Kind: kindCalls, From: "a", To: "b"},
			{Kind: kindCalls, From: "b", To: "d"},
		},
	}
	store := sdk.NewMemStore()
	s := sdk.For(Name, store)
	if _, err := Bootstrap(s, store, c, nil); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	seed := SymbolID(c.Symbols[0]) // a
	rows, err := ImpactRadiusFromStore(store, Name, []string{seed}, 3)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	ids := map[string]bool{}
	for _, r := range rows {
		ids[r.QualifiedName] = true
	}
	if !ids["b"] || !ids["d"] {
		t.Fatalf("impact radius from a should reach b and d; got %v", ids)
	}
	if ids["a"] {
		t.Fatal("impact radius must exclude the seed itself")
	}
	if ids["iso"] {
		t.Fatal("isolated node must not appear in impact radius")
	}
}

func TestImpactRadiusFromStore_DepthBound(t *testing.T) {
	c := Corpus{
		Symbols: []Symbol{
			{QualifiedName: "a", Kind: kindFn, FilePath: "a.go"},
			{QualifiedName: "b", Kind: kindFn, FilePath: "b.go"},
			{QualifiedName: "d", Kind: kindFn, FilePath: "d.go"},
		},
		References: []Reference{
			{Kind: kindCalls, From: "a", To: "b"}, {Kind: kindCalls, From: "b", To: "d"},
		},
	}
	store := sdk.NewMemStore()
	s := sdk.For(Name, store)
	if _, err := Bootstrap(s, store, c, nil); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	rows, err := ImpactRadiusFromStore(store, Name, []string{SymbolID(c.Symbols[0])}, 1)
	if err != nil {
		t.Fatalf("impact: %v", err)
	}
	if len(rows) != 1 || rows[0].QualifiedName != "b" {
		t.Fatalf("depth-1 from a should reach only b, got %v", rows)
	}
}

func TestLoadCorpusAndPinnedCommits(t *testing.T) {
	dir := parityDir(t)
	commits, err := PinnedCommits(filepath.Join(dir, "commits.txt"))
	if err != nil {
		t.Fatalf("pinned commits: %v", err)
	}
	if len(commits) != 10 {
		t.Fatalf("want 10 pinned commits (§11.6), got %d", len(commits))
	}
	files, err := SortedCorpusFiles(filepath.Join(dir, "corpus"))
	if err != nil {
		t.Fatalf("corpus files: %v", err)
	}
	if len(files) != 10 {
		t.Fatalf("want 10 corpus files, got %d", len(files))
	}
	c, err := LoadCorpus(files[0])
	if err != nil {
		t.Fatalf("load corpus: %v", err)
	}
	if len(c.Symbols) != 100 {
		t.Fatalf("want 100-symbol corpus (§11.6), got %d", len(c.Symbols))
	}
}

func smallCorpus() Corpus {
	return Corpus{
		Commit: "deadbeef",
		Symbols: []Symbol{
			{QualifiedName: "pkg.A", Kind: kindFn, Language: "go", FilePath: "a.go", LineStart: 1, ContentHash: "h1"},
			{QualifiedName: "pkg.B", Kind: kindType, Language: "go", FilePath: "b.go", LineStart: 2, ContentHash: "h2"},
			{QualifiedName: "pkg.C", Kind: kindFn, Language: "ts", FilePath: "c.ts", LineStart: 3, ContentHash: "h3"},
		},
		References: []Reference{{Kind: kindCalls, From: "pkg.A", To: "pkg.C"}},
	}
}

// note builds a persisted symbol note for diff tests.
func note(id, qn, kind, file, hash string) sdk.Note {
	return sdk.Note{ID: id, Type: noteTypeSymbol, Fields: map[string]any{
		fieldQualified: qn, fieldKind: kind, fieldFilePath: file, fieldContentSum: hash, fieldLineStart: 1,
	}}
}

// parityDir resolves testdata/crg-parity relative to this test file.
func parityDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test file path")
	}
	// internal/adapters/builtin/crg/crg_test.go → repo root is 4 dirs up.
	root := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..")
	return filepath.Join(root, "testdata", "crg-parity")
}
