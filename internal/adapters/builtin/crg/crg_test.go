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

func TestBootstrapWritesToOwnNamespaceAndSnapshots(t *testing.T) {
	c := smallCorpus()
	store := sdk.NewMemStore()
	s := sdk.For(Name, store)
	snap, err := Bootstrap(s, c, c.Commit)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if snap.Adapter != Name {
		t.Fatalf("snapshot adapter = %q, want %q", snap.Adapter, Name)
	}
	if snap.NodesTotal != len(c.Symbols) {
		t.Fatalf("nodes total = %d, want %d", snap.NodesTotal, len(c.Symbols))
	}
	// Wrote ONLY to kg-native namespace (Name), nothing else.
	ns := store.Namespaces()
	if len(ns) != 1 || ns[0] != Name {
		t.Fatalf("namespaces = %v, want exactly [%s]", ns, Name)
	}
	// O5: source_mutation fired once per symbol (content-hash event), not per
	// any-upsert.
	if got := len(s.FiredPredicates()); got != len(c.Symbols) {
		t.Fatalf("source_mutation fired %d times, want %d (one per symbol)", got, len(c.Symbols))
	}
	for _, fp := range s.FiredPredicates() {
		if fp.Predicate != "source_mutation" {
			t.Fatalf("unexpected predicate %q", fp.Predicate)
		}
		if _, ok := fp.Args["content_hash"]; !ok {
			t.Fatal("source_mutation event must carry content_hash (O5)")
		}
	}
}

func TestDiff_InsertUpdateDelete(t *testing.T) {
	prev := Corpus{Symbols: []Symbol{
		{QualifiedName: "a", Kind: kindFn, FilePath: "a.go", LineStart: 1, ContentHash: "h1"},
		{QualifiedName: "b", Kind: kindFn, FilePath: "b.go", LineStart: 2, ContentHash: "h2"},
	}}
	next := Corpus{Symbols: []Symbol{
		{QualifiedName: "a", Kind: kindFn, FilePath: "a.go", LineStart: 1, ContentHash: "h1-NEW"}, // update
		{QualifiedName: "c", Kind: kindType, FilePath: "c.go", LineStart: 3, ContentHash: "h3"},   // insert
		// b removed → delete
	}}
	got := Diff(prev, next)
	ops := map[graphstore.UpsertOp]int{}
	for _, u := range got {
		ops[u.Op]++
	}
	if ops[graphstore.OpInsert] != 1 || ops[graphstore.OpUpdate] != 1 || ops[graphstore.OpDelete] != 1 {
		t.Fatalf("expected 1 each of insert/update/delete, got %+v (%v)", ops, got)
	}
}

func TestDiff_NoChangeIsEmpty(t *testing.T) {
	c := smallCorpus()
	if got := Diff(c, c); len(got) != 0 {
		t.Fatalf("identical corpus diff should be empty, got %v", got)
	}
}

func TestImpactRadius_ExpandsAlongEdges(t *testing.T) {
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
	seed := SymbolID(c.Symbols[0]) // a
	rows := ImpactRadiusRows(c, []string{seed}, 3)
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

func TestImpactRadius_DepthBound(t *testing.T) {
	c := Corpus{
		Symbols: []Symbol{
			{QualifiedName: "a", Kind: kindFn}, {QualifiedName: "b", Kind: kindFn},
			{QualifiedName: "d", Kind: kindFn},
		},
		References: []Reference{
			{Kind: kindCalls, From: "a", To: "b"}, {Kind: kindCalls, From: "b", To: "d"},
		},
	}
	rows := ImpactRadiusRows(c, []string{SymbolID(c.Symbols[0])}, 1)
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
