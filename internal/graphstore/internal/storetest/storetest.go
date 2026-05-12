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

	"github.com/NikashPrakash/dot-agents/internal/graphstore"
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
