// Package storetest provides shared backend-agnostic test bodies for
// graphstore.Store implementations. Each Run* function executes one
// canonical assertion sequence against a Store handle produced by the
// caller's openStore closure.
//
// The package lives under internal/graphstore/internal/ to ensure it
// is only consumable by the graphstore package itself — these are
// fixtures for graphstore tests, not a public API.
//
// Naming note: design.md (.agents/workflow/specs/go-test-fixture-extraction)
// proposed three runners as RunNode / RunEdge / RunStats. The actual
// Sonar-flagged duplication blocks at sqlite_test.go:135/505/559 ↔
// postgres_test.go:134/369/423 are node round-trip + KGNote round-trip
// + KGNote search (not edge or stats). The runners below are named
// against what the duplications actually are.
package storetest

import (
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// OpenStore returns a fresh, empty graphstore.Store for one test
// invocation. Implementations should register a t.Cleanup hook to close
// the store at test end.
type OpenStore func(t *testing.T) graphstore.Store

// RunNodeRoundTrip exercises UpsertNode → GetNode for a function-kind
// node fixture and asserts the read-back node carries the same Name,
// Language, and LineStart. Replaces the duplicated body at
// sqlite_test.go:135 ↔ postgres_test.go:134.
func RunNodeRoundTrip(t *testing.T, open OpenStore) {
	t.Helper()
	s := open(t)
	node := graphstore.NodeInfo{
		Kind:      graphstore.NodeKindFunction,
		Name:      "run",
		FilePath:  "cmd/main.go",
		Language:  "go",
		LineStart: 5,
		LineEnd:   20,
	}
	if _, err := s.UpsertNode(node, "abc123"); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	got, err := s.GetNode("cmd/main.go::run")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got == nil {
		t.Fatal("GetNode returned nil")
	}
	if got.Name != "run" || got.Language != "go" || got.LineStart != 5 {
		t.Errorf("unexpected node: %+v", got)
	}
}

// RunKGNoteRoundTrip exercises UpsertKGNote → GetKGNote for a decision
// fixture and asserts the read-back note carries the same Title,
// NoteType, and Version. Replaces the duplicated body at
// sqlite_test.go:505 ↔ postgres_test.go:369.
func RunKGNoteRoundTrip(t *testing.T, open OpenStore) {
	t.Helper()
	s := open(t)
	note := graphstore.KGNote{
		ID:       "decision-001",
		Title:    "Backend choice",
		NoteType: "decision",
		Status:   "active",
		Summary:  "Round-trip fixture; backend implementation under test.",
		FilePath: "/kg/notes/decision-001.md",
		Version:  1,
	}
	if err := s.UpsertKGNote(note); err != nil {
		t.Fatalf("UpsertKGNote: %v", err)
	}
	got, err := s.GetKGNote("decision-001")
	if err != nil {
		t.Fatalf("GetKGNote: %v", err)
	}
	if got == nil {
		t.Fatal("GetKGNote returned nil")
	}
	if got.Title != note.Title || got.NoteType != note.NoteType || got.Version != 1 {
		t.Errorf("unexpected note: %+v", got)
	}
}

// RunKGNoteSearch seeds three notes whose Titles or Summaries share a
// term ("graph") and asserts SearchKGNotes returns at least the two
// expected matches. Replaces the duplicated body at
// sqlite_test.go:559 ↔ postgres_test.go:423.
func RunKGNoteSearch(t *testing.T, open OpenStore) {
	t.Helper()
	s := open(t)
	notes := []graphstore.KGNote{
		{ID: "n1", Title: "graph architecture", NoteType: "decision", Status: "active", FilePath: "a.md"},
		{ID: "n2", Title: "Theory", Summary: "concepts for the graph layer", NoteType: "concept", Status: "active", FilePath: "b.md"},
		{ID: "n3", Title: "Unrelated", NoteType: "entity", Status: "active", FilePath: "c.md"},
	}
	for _, n := range notes {
		if err := s.UpsertKGNote(n); err != nil {
			t.Fatalf("UpsertKGNote %s: %v", n.ID, err)
		}
	}
	results, err := s.SearchKGNotes("graph", 10)
	if err != nil {
		t.Fatalf("SearchKGNotes: %v", err)
	}
	if len(results) < 2 {
		t.Errorf("want at least 2 'graph' notes, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// Prefix-parameterized runners
//
// The runners below take a string prefix that is composed into every row
// key the runner writes. This isolation contract lets each consumer
// (sqlite_test.go, postgres_test.go) pass a backend-unique prefix so that
// the shared Postgres testcontainer — which is re-used across all
// PG-tagged tests in one process — never sees row collisions between
// the SQLite-side and PG-side invocations of the same runner, nor
// between two PG-tagged tests that happen to invoke the same runner.
//
// Callers should pick prefixes that include both a backend tag and a
// per-runner discriminator, e.g. "sqlite-meta-rt-", "pg-meta-ov-". The
// runners do not generate the prefix themselves; the caller owns
// uniqueness so the prefix can be greppable in failure traces.
// ---------------------------------------------------------------------------

// RunMetadataRoundTrip exercises SetMetadata → GetMetadata across three
// scenarios: a basic round trip, a missing key (must return ""), and an
// overwrite (later Set wins). All metadata keys are namespaced by
// keyPrefix so two concurrent invocations against the same backend do
// not interfere. Replaces the mirrored bodies at
// sqlite_test.go:64/78/89 ↔ postgres_test.go:57/71/82.
func RunMetadataRoundTrip(t *testing.T, store graphstore.Store, keyPrefix string) {
	t.Helper()

	// Round trip.
	rtKey := keyPrefix + "rt"
	if err := store.SetMetadata(rtKey, "v-rt"); err != nil {
		t.Fatalf("SetMetadata round-trip: %v", err)
	}
	got, err := store.GetMetadata(rtKey)
	if err != nil {
		t.Fatalf("GetMetadata round-trip: %v", err)
	}
	if got != "v-rt" {
		t.Errorf("round-trip: want %q, got %q", "v-rt", got)
	}

	// Missing key — must return empty string, no error.
	missing, err := store.GetMetadata(keyPrefix + "missing-xyz-not-set")
	if err != nil {
		t.Fatalf("GetMetadata missing: %v", err)
	}
	if missing != "" {
		t.Errorf("missing key: expected empty string, got %q", missing)
	}

	// Overwrite — second Set wins.
	ovKey := keyPrefix + "ov"
	if err := store.SetMetadata(ovKey, "v1"); err != nil {
		t.Fatalf("SetMetadata overwrite v1: %v", err)
	}
	if err := store.SetMetadata(ovKey, "v2"); err != nil {
		t.Fatalf("SetMetadata overwrite v2: %v", err)
	}
	ov, err := store.GetMetadata(ovKey)
	if err != nil {
		t.Fatalf("GetMetadata overwrite: %v", err)
	}
	if ov != "v2" {
		t.Errorf("overwrite: want v2, got %q", ov)
	}
}

// RunEdgeUpsertCreate exercises UpsertEdge on a fresh edge and asserts a
// non-zero ID is returned. Source/Target are prefixed by idPrefix so
// distinct invocations cannot collide on the (source, target, kind)
// uniqueness key. Replaces the mirrored body at
// sqlite_test.go:183 ↔ postgres_test.go:160.
func RunEdgeUpsertCreate(t *testing.T, store graphstore.Store, idPrefix string) {
	t.Helper()
	edge := graphstore.EdgeInfo{
		Kind:     graphstore.EdgeKindCalls,
		Source:   idPrefix + "A",
		Target:   idPrefix + "B",
		FilePath: idPrefix + "edge.go",
		Line:     1,
	}
	id, err := store.UpsertEdge(edge)
	if err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero edge ID")
	}
}

// RunEdgeUpsertUpdate exercises UpsertEdge twice with the same
// (source, target, kind) tuple and asserts the returned ID is stable —
// i.e. the second call updates rather than inserts. Source/Target are
// prefixed by idPrefix to keep two invocations of this runner against
// the same backend independent. Replaces the mirrored body at
// sqlite_test.go:194 ↔ postgres_test.go:171.
func RunEdgeUpsertUpdate(t *testing.T, store graphstore.Store, idPrefix string) {
	t.Helper()
	edge := graphstore.EdgeInfo{
		Kind:     graphstore.EdgeKindCalls,
		Source:   idPrefix + "src",
		Target:   idPrefix + "tgt",
		FilePath: idPrefix + "edge.go",
		Line:     1,
	}
	id1, err := store.UpsertEdge(edge)
	if err != nil {
		t.Fatalf("UpsertEdge first: %v", err)
	}
	edge.Line = 42
	id2, err := store.UpsertEdge(edge)
	if err != nil {
		t.Fatalf("UpsertEdge second: %v", err)
	}
	if id1 != id2 {
		t.Errorf("expected same id on update: id1=%d id2=%d", id1, id2)
	}
}

// RunNoteSymbolLinkRoundTrip exercises UpsertNoteSymbolLink →
// GetLinksForNote and asserts the link is read back with the expected
// QualifiedName. NoteID and QualifiedName are namespaced by namePrefix
// so distinct invocations cannot share rows. Replaces the mirrored body
// at sqlite_test.go:546 ↔ postgres_test.go:404.
func RunNoteSymbolLinkRoundTrip(t *testing.T, store graphstore.Store, namePrefix string) {
	t.Helper()
	noteID := namePrefix + "decision-rt"
	qualified := namePrefix + "pkg::Store"
	link := graphstore.NoteSymbolLink{
		NoteID:        noteID,
		QualifiedName: qualified,
		LinkKind:      "documents",
	}
	id, err := store.UpsertNoteSymbolLink(link)
	if err != nil {
		t.Fatalf("UpsertNoteSymbolLink: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero link ID")
	}
	links, err := store.GetLinksForNote(noteID)
	if err != nil {
		t.Fatalf("GetLinksForNote: %v", err)
	}
	found := false
	for _, l := range links {
		if l.QualifiedName == qualified {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("link not found in results: %+v", links)
	}
}

// RunNoteSymbolLinkIdempotent exercises UpsertNoteSymbolLink twice with
// the same (NoteID, QualifiedName, LinkKind) tuple and asserts the
// returned ID is stable AND only one link exists for the note (filtered
// to the prefix-owned QualifiedName so unrelated rows in a shared
// testcontainer cannot inflate the count). Replaces the mirrored body
// at sqlite_test.go:568 ↔ postgres_test.go:432.
func RunNoteSymbolLinkIdempotent(t *testing.T, store graphstore.Store, namePrefix string) {
	t.Helper()
	noteID := namePrefix + "n1-idem"
	qualified := namePrefix + "pkg::Fn"
	link := graphstore.NoteSymbolLink{
		NoteID:        noteID,
		QualifiedName: qualified,
		LinkKind:      "mentions",
	}
	id1, err := store.UpsertNoteSymbolLink(link)
	if err != nil {
		t.Fatalf("UpsertNoteSymbolLink first: %v", err)
	}
	id2, err := store.UpsertNoteSymbolLink(link)
	if err != nil {
		t.Fatalf("UpsertNoteSymbolLink second: %v", err)
	}
	if id1 != id2 {
		t.Errorf("expected idempotent insert, got id1=%d id2=%d", id1, id2)
	}
	links, err := store.GetLinksForNote(noteID)
	if err != nil {
		t.Fatalf("GetLinksForNote: %v", err)
	}
	count := 0
	for _, l := range links {
		if l.QualifiedName == qualified && l.LinkKind == "mentions" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 link after idempotent upsert, got %d", count)
	}
}
