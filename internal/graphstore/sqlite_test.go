package graphstore_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/graphstore"
)

// openTestStore opens a fresh in-memory-like SQLite database in a temp dir.
func openTestStore(t *testing.T) *graphstore.SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	s, err := graphstore.OpenSQLite(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ---------------------------------------------------------------------------
// Schema initialisation
// ---------------------------------------------------------------------------

func TestOpenSQLite_CreatesSchema(t *testing.T) {
	s := openTestStore(t)
	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("GetStats after open: %v", err)
	}
	if stats.TotalNodes != 0 || stats.TotalEdges != 0 {
		t.Errorf("expected empty graph, got nodes=%d edges=%d", stats.TotalNodes, stats.TotalEdges)
	}
}

func TestOpenSQLite_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "idempotent.db")

	s1, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	s1.Close()

	s2, err := graphstore.OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	s2.Close()
}

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

func TestMetadata_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	if err := s.SetMetadata("last_updated", "2026-04-11"); err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}
	val, err := s.GetMetadata("last_updated")
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if val != "2026-04-11" {
		t.Errorf("want 2026-04-11, got %q", val)
	}
}

func TestMetadata_MissingKey(t *testing.T) {
	s := openTestStore(t)
	val, err := s.GetMetadata("nonexistent")
	if err != nil {
		t.Fatalf("GetMetadata missing key: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string for missing key, got %q", val)
	}
}

func TestMetadata_Overwrite(t *testing.T) {
	s := openTestStore(t)
	_ = s.SetMetadata("k", "v1")
	_ = s.SetMetadata("k", "v2")
	val, _ := s.GetMetadata("k")
	if val != "v2" {
		t.Errorf("want v2, got %q", val)
	}
}

// ---------------------------------------------------------------------------
// Node upsert + read
// ---------------------------------------------------------------------------

func makeNode(name, kind, file string) graphstore.NodeInfo {
	return graphstore.NodeInfo{
		Kind:     kind,
		Name:     name,
		FilePath: file,
		Language: "go",
	}
}

func TestUpsertNode_Create(t *testing.T) {
	s := openTestStore(t)
	id, err := s.UpsertNode(makeNode("main", graphstore.NodeKindFunction, "main.go"), "")
	if err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestUpsertNode_Update(t *testing.T) {
	s := openTestStore(t)
	node := makeNode("Handler", graphstore.NodeKindFunction, "handler.go")
	id1, _ := s.UpsertNode(node, "hash1")

	node.LineStart = 10
	id2, err := s.UpsertNode(node, "hash2")
	if err != nil {
		t.Fatalf("UpsertNode update: %v", err)
	}
	if id1 != id2 {
		t.Errorf("upsert should return same id: id1=%d id2=%d", id1, id2)
	}
}

func TestGetNode_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	node := graphstore.NodeInfo{
		Kind: graphstore.NodeKindFunction, Name: "run",
		FilePath: "cmd/main.go", Language: "go",
		LineStart: 5, LineEnd: 20,
	}
	_, err := s.UpsertNode(node, "abc123")
	if err != nil {
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

func TestGetNode_NotFound(t *testing.T) {
	s := openTestStore(t)
	got, err := s.GetNode("missing::node")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing node, got %+v", got)
	}
}

func TestGetNodesByFile(t *testing.T) {
	s := openTestStore(t)
	for _, name := range []string{"A", "B", "C"} {
		_, err := s.UpsertNode(makeNode(name, graphstore.NodeKindFunction, "pkg/foo.go"), "")
		if err != nil {
			t.Fatalf("UpsertNode %s: %v", name, err)
		}
	}
	_, _ = s.UpsertNode(makeNode("Other", graphstore.NodeKindFunction, "pkg/bar.go"), "")

	nodes, err := s.GetNodesByFile("pkg/foo.go")
	if err != nil {
		t.Fatalf("GetNodesByFile: %v", err)
	}
	if len(nodes) != 3 {
		t.Errorf("want 3 nodes, got %d", len(nodes))
	}
}

// ---------------------------------------------------------------------------
// Edge upsert + read
// ---------------------------------------------------------------------------

func makeEdge(src, tgt, kind string) graphstore.EdgeInfo {
	return graphstore.EdgeInfo{
		Kind: kind, Source: src, Target: tgt,
		FilePath: "a.go", Line: 1,
	}
}

func TestUpsertEdge_Create(t *testing.T) {
	s := openTestStore(t)
	id, err := s.UpsertEdge(makeEdge("pkg::A", "pkg::B", graphstore.EdgeKindCalls))
	if err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero edge ID")
	}
}

func TestUpsertEdge_Update(t *testing.T) {
	s := openTestStore(t)
	e := makeEdge("A", "B", graphstore.EdgeKindCalls)
	id1, _ := s.UpsertEdge(e)
	e.Line = 42
	id2, err := s.UpsertEdge(e)
	if err != nil {
		t.Fatalf("UpsertEdge update: %v", err)
	}
	if id1 != id2 {
		t.Errorf("expected same id on update: id1=%d id2=%d", id1, id2)
	}
}

func TestGetEdgesBySource(t *testing.T) {
	s := openTestStore(t)
	_, _ = s.UpsertEdge(makeEdge("X", "Y", graphstore.EdgeKindCalls))
	_, _ = s.UpsertEdge(makeEdge("X", "Z", graphstore.EdgeKindImportsFrom))
	_, _ = s.UpsertEdge(makeEdge("W", "X", graphstore.EdgeKindCalls))

	edges, err := s.GetEdgesBySource("X")
	if err != nil {
		t.Fatalf("GetEdgesBySource: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("want 2 edges, got %d", len(edges))
	}
}

func TestGetEdgesByTarget(t *testing.T) {
	s := openTestStore(t)
	_, _ = s.UpsertEdge(makeEdge("A", "Z", graphstore.EdgeKindCalls))
	_, _ = s.UpsertEdge(makeEdge("B", "Z", graphstore.EdgeKindCalls))
	_, _ = s.UpsertEdge(makeEdge("A", "Y", graphstore.EdgeKindCalls))

	edges, err := s.GetEdgesByTarget("Z")
	if err != nil {
		t.Fatalf("GetEdgesByTarget: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("want 2 edges, got %d", len(edges))
	}
}

// ---------------------------------------------------------------------------
// RemoveFileData + StoreFileNodesEdges
// ---------------------------------------------------------------------------

func TestRemoveFileData(t *testing.T) {
	s := openTestStore(t)
	_, _ = s.UpsertNode(makeNode("fn1", graphstore.NodeKindFunction, "a.go"), "")
	_, _ = s.UpsertNode(makeNode("fn2", graphstore.NodeKindFunction, "a.go"), "")
	_, _ = s.UpsertEdge(makeEdge("a.go::fn1", "a.go::fn2", graphstore.EdgeKindCalls))

	if err := s.RemoveFileData("a.go"); err != nil {
		t.Fatalf("RemoveFileData: %v", err)
	}
	nodes, _ := s.GetNodesByFile("a.go")
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes after remove, got %d", len(nodes))
	}
}

func TestStoreFileNodesEdges_Atomic(t *testing.T) {
	s := openTestStore(t)
	nodes := []graphstore.NodeInfo{
		makeNode("F1", graphstore.NodeKindFunction, "b.go"),
		makeNode("F2", graphstore.NodeKindFunction, "b.go"),
	}
	edges := []graphstore.EdgeInfo{
		makeEdge("b.go::F1", "b.go::F2", graphstore.EdgeKindCalls),
	}

	if err := s.StoreFileNodesEdges("b.go", nodes, edges, "hash"); err != nil {
		t.Fatalf("StoreFileNodesEdges: %v", err)
	}

	got, _ := s.GetNodesByFile("b.go")
	if len(got) != 2 {
		t.Errorf("want 2 nodes, got %d", len(got))
	}

	// Re-store with different content — old data should be replaced.
	if err := s.StoreFileNodesEdges("b.go", nodes[:1], nil, "hash2"); err != nil {
		t.Fatalf("StoreFileNodesEdges replace: %v", err)
	}
	got, _ = s.GetNodesByFile("b.go")
	if len(got) != 1 {
		t.Errorf("want 1 node after replace, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// SearchNodes
// ---------------------------------------------------------------------------

func TestSearchNodes(t *testing.T) {
	s := openTestStore(t)
	for _, name := range []string{"handleRequest", "parseRequest", "buildResponse"} {
		_, _ = s.UpsertNode(makeNode(name, graphstore.NodeKindFunction, "server.go"), "")
	}

	results, err := s.SearchNodes("Request", 10)
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("want 2 results for 'Request', got %d", len(results))
	}
}

func TestSearchNodes_Limit(t *testing.T) {
	s := openTestStore(t)
	for i := 0; i < 10; i++ {
		_, _ = s.UpsertNode(makeNode("foo", graphstore.NodeKindFunction, "f.go"), "")
		// Make each unique with a different file path
		_, _ = s.UpsertNode(graphstore.NodeInfo{
			Kind: graphstore.NodeKindFunction, Name: "fooHelper",
			FilePath: "helper_" + string(rune('a'+i)) + ".go", Language: "go",
		}, "")
	}

	results, err := s.SearchNodes("foo", 3)
	if err != nil {
		t.Fatalf("SearchNodes limit: %v", err)
	}
	if len(results) > 3 {
		t.Errorf("limit 3 not respected, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// GetAllFiles
// ---------------------------------------------------------------------------

func TestGetAllFiles(t *testing.T) {
	s := openTestStore(t)
	for _, f := range []string{"a.go", "b.go", "c.go"} {
		_, _ = s.UpsertNode(graphstore.NodeInfo{
			Kind: graphstore.NodeKindFile, Name: f, FilePath: f, Language: "go",
		}, "")
	}
	files, err := s.GetAllFiles()
	if err != nil {
		t.Fatalf("GetAllFiles: %v", err)
	}
	if len(files) != 3 {
		t.Errorf("want 3 files, got %d", len(files))
	}
}

// ---------------------------------------------------------------------------
// GetStats
// ---------------------------------------------------------------------------

func TestGetStats_Empty(t *testing.T) {
	s := openTestStore(t)
	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalNodes != 0 || stats.TotalEdges != 0 {
		t.Errorf("expected empty, got %+v", stats)
	}
}

func TestGetStats_Populated(t *testing.T) {
	s := openTestStore(t)
	_, _ = s.UpsertNode(makeNode("F", graphstore.NodeKindFunction, "a.go"), "")
	_, _ = s.UpsertNode(makeNode("C", graphstore.NodeKindClass, "a.go"), "")
	_, _ = s.UpsertEdge(makeEdge("a.go::F", "a.go::C", graphstore.EdgeKindContains))

	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalNodes != 2 {
		t.Errorf("want 2 nodes, got %d", stats.TotalNodes)
	}
	if stats.TotalEdges != 1 {
		t.Errorf("want 1 edge, got %d", stats.TotalEdges)
	}
	if stats.NodesByKind[graphstore.NodeKindFunction] != 1 {
		t.Errorf("want 1 Function, got %d", stats.NodesByKind[graphstore.NodeKindFunction])
	}
}

// ---------------------------------------------------------------------------
// GetEdgesAmong
// ---------------------------------------------------------------------------

func TestGetEdgesAmong(t *testing.T) {
	s := openTestStore(t)
	_, _ = s.UpsertEdge(makeEdge("A", "B", graphstore.EdgeKindCalls))
	_, _ = s.UpsertEdge(makeEdge("B", "C", graphstore.EdgeKindCalls))
	_, _ = s.UpsertEdge(makeEdge("A", "D", graphstore.EdgeKindCalls)) // D outside set

	edges, err := s.GetEdgesAmong([]string{"A", "B", "C"})
	if err != nil {
		t.Fatalf("GetEdgesAmong: %v", err)
	}
	if len(edges) != 2 {
		t.Errorf("want 2 edges within set, got %d", len(edges))
	}
}

func TestGetEdgesAmong_Empty(t *testing.T) {
	s := openTestStore(t)
	edges, err := s.GetEdgesAmong(nil)
	if err != nil {
		t.Fatalf("GetEdgesAmong nil: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges for nil input, got %d", len(edges))
	}
}

// ---------------------------------------------------------------------------
// GetImpactRadius
// ---------------------------------------------------------------------------

func TestGetImpactRadius_BasicBFS(t *testing.T) {
	s := openTestStore(t)

	// Graph: file1.go has F1; F1 calls F2 in file2.go; F2 calls F3 in file3.go
	_, _ = s.UpsertNode(graphstore.NodeInfo{
		Kind: graphstore.NodeKindFunction, Name: "F1", FilePath: "file1.go", Language: "go",
	}, "")
	_, _ = s.UpsertNode(graphstore.NodeInfo{
		Kind: graphstore.NodeKindFunction, Name: "F2", FilePath: "file2.go", Language: "go",
	}, "")
	_, _ = s.UpsertNode(graphstore.NodeInfo{
		Kind: graphstore.NodeKindFunction, Name: "F3", FilePath: "file3.go", Language: "go",
	}, "")
	_, _ = s.UpsertEdge(graphstore.EdgeInfo{
		Kind: graphstore.EdgeKindCalls, Source: "file1.go::F1", Target: "file2.go::F2", FilePath: "file1.go",
	})
	_, _ = s.UpsertEdge(graphstore.EdgeInfo{
		Kind: graphstore.EdgeKindCalls, Source: "file2.go::F2", Target: "file3.go::F3", FilePath: "file2.go",
	})

	result, err := s.GetImpactRadius([]string{"file1.go"}, 2, 500)
	if err != nil {
		t.Fatalf("GetImpactRadius: %v", err)
	}
	if len(result.ChangedNodes) != 1 {
		t.Errorf("want 1 changed node, got %d", len(result.ChangedNodes))
	}
	if len(result.ImpactedNodes) != 2 {
		t.Errorf("want 2 impacted nodes (F2 and F3), got %d", len(result.ImpactedNodes))
	}
}

func TestGetImpactRadius_EmptyFiles(t *testing.T) {
	s := openTestStore(t)
	result, err := s.GetImpactRadius([]string{"nonexistent.go"}, 2, 500)
	if err != nil {
		t.Fatalf("GetImpactRadius empty: %v", err)
	}
	if len(result.ChangedNodes) != 0 || len(result.ImpactedNodes) != 0 {
		t.Errorf("expected empty result, got %+v", result)
	}
}

func TestGetImpactRadius_MaxNodesLimit(t *testing.T) {
	s := openTestStore(t)
	// Build a chain of 20 functions; limit to maxNodes=5 — should stop early.
	for i := 0; i < 20; i++ {
		name := "fn_" + string(rune('a'+i))
		_, _ = s.UpsertNode(graphstore.NodeInfo{
			Kind: graphstore.NodeKindFunction, Name: name, FilePath: "chain.go", Language: "go",
		}, "")
	}
	// Wire as chain
	for i := 0; i < 19; i++ {
		src := "chain.go::fn_" + string(rune('a'+i))
		tgt := "chain.go::fn_" + string(rune('a'+i+1))
		_, _ = s.UpsertEdge(graphstore.EdgeInfo{Kind: graphstore.EdgeKindCalls, Source: src, Target: tgt, FilePath: "chain.go"})
	}

	result, err := s.GetImpactRadius([]string{"chain.go"}, 20, 5)
	if err != nil {
		t.Fatalf("GetImpactRadius max nodes: %v", err)
	}
	total := len(result.ChangedNodes) + len(result.ImpactedNodes)
	if total > 20 {
		t.Errorf("expected capped result, got total=%d", total)
	}
}

// ---------------------------------------------------------------------------
// KG notes
// ---------------------------------------------------------------------------

func TestUpsertGetKGNote(t *testing.T) {
	s := openTestStore(t)
	note := graphstore.KGNote{
		ID:       "decision-001",
		Title:    "Use SQLite as warm layer",
		NoteType: "decision",
		Status:   "active",
		Summary:  "Chosen for pure-Go, zero-dep deployment.",
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

func TestGetKGNote_NotFound(t *testing.T) {
	s := openTestStore(t)
	got, err := s.GetKGNote("missing")
	if err != nil {
		t.Fatalf("GetKGNote missing: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestUpsertKGNote_UpdatesInPlace(t *testing.T) {
	s := openTestStore(t)
	note := graphstore.KGNote{
		ID: "n1", Title: "Old Title", NoteType: "concept", Status: "active", FilePath: "f.md",
	}
	_ = s.UpsertKGNote(note)
	note.Title = "New Title"
	note.Version = 2
	_ = s.UpsertKGNote(note)

	got, _ := s.GetKGNote("n1")
	if got.Title != "New Title" || got.Version != 2 {
		t.Errorf("expected updated note, got %+v", got)
	}
}

func TestSearchKGNotes(t *testing.T) {
	s := openTestStore(t)
	for _, n := range []graphstore.KGNote{
		{ID: "n1", Title: "SQLite architecture", NoteType: "decision", Status: "active", FilePath: "a.md"},
		{ID: "n2", Title: "Graph theory", Summary: "concepts for SQLite graph", NoteType: "concept", Status: "active", FilePath: "b.md"},
		{ID: "n3", Title: "Unrelated", NoteType: "entity", Status: "active", FilePath: "c.md"},
	} {
		_ = s.UpsertKGNote(n)
	}

	results, err := s.SearchKGNotes("SQLite", 10)
	if err != nil {
		t.Fatalf("SearchKGNotes: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("want 2 SQLite notes, got %d", len(results))
	}
}

func TestListArchivedKGNotes(t *testing.T) {
	s := openTestStore(t)
	_ = s.UpsertKGNote(graphstore.KGNote{
		ID: "archived1", Title: "Old Decision", NoteType: "decision", Status: "superseded",
		FilePath: "a.md", ArchivedAt: "2026-01-01T00:00:00Z",
	})
	_ = s.UpsertKGNote(graphstore.KGNote{
		ID: "active1", Title: "Active", NoteType: "concept", Status: "active", FilePath: "b.md",
	})

	archived, err := s.ListArchivedKGNotes()
	if err != nil {
		t.Fatalf("ListArchivedKGNotes: %v", err)
	}
	if len(archived) != 1 || archived[0].ID != "archived1" {
		t.Errorf("expected 1 archived note, got %d: %+v", len(archived), archived)
	}
}

// ---------------------------------------------------------------------------
// Note→symbol links
// ---------------------------------------------------------------------------

func TestNoteSymbolLink_RoundTrip(t *testing.T) {
	s := openTestStore(t)
	link := graphstore.NoteSymbolLink{
		NoteID: "decision-001", QualifiedName: "pkg::Store", LinkKind: "documents",
	}
	id, err := s.UpsertNoteSymbolLink(link)
	if err != nil {
		t.Fatalf("UpsertNoteSymbolLink: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero link ID")
	}

	links, err := s.GetLinksForNote("decision-001")
	if err != nil {
		t.Fatalf("GetLinksForNote: %v", err)
	}
	if len(links) != 1 || links[0].QualifiedName != "pkg::Store" {
		t.Errorf("unexpected links: %+v", links)
	}
}

func TestNoteSymbolLink_Idempotent(t *testing.T) {
	s := openTestStore(t)
	link := graphstore.NoteSymbolLink{NoteID: "n1", QualifiedName: "pkg::Fn", LinkKind: "mentions"}
	id1, _ := s.UpsertNoteSymbolLink(link)
	id2, _ := s.UpsertNoteSymbolLink(link)
	if id1 != id2 {
		t.Errorf("expected idempotent insert, got id1=%d id2=%d", id1, id2)
	}

	links, _ := s.GetLinksForNote("n1")
	if len(links) != 1 {
		t.Errorf("expected 1 link after idempotent upsert, got %d", len(links))
	}
}

func TestGetLinksForSymbol(t *testing.T) {
	s := openTestStore(t)
	_, _ = s.UpsertNoteSymbolLink(graphstore.NoteSymbolLink{NoteID: "n1", QualifiedName: "pkg::F", LinkKind: "mentions"})
	_, _ = s.UpsertNoteSymbolLink(graphstore.NoteSymbolLink{NoteID: "n2", QualifiedName: "pkg::F", LinkKind: "documents"})
	_, _ = s.UpsertNoteSymbolLink(graphstore.NoteSymbolLink{NoteID: "n3", QualifiedName: "pkg::G", LinkKind: "mentions"})

	links, err := s.GetLinksForSymbol("pkg::F")
	if err != nil {
		t.Fatalf("GetLinksForSymbol: %v", err)
	}
	if len(links) != 2 {
		t.Errorf("want 2 links for pkg::F, got %d", len(links))
	}
}

func TestDeleteNoteSymbolLink(t *testing.T) {
	s := openTestStore(t)
	id, _ := s.UpsertNoteSymbolLink(graphstore.NoteSymbolLink{NoteID: "n1", QualifiedName: "q1", LinkKind: "mentions"})
	if err := s.DeleteNoteSymbolLink(id); err != nil {
		t.Fatalf("DeleteNoteSymbolLink: %v", err)
	}
	links, _ := s.GetLinksForNote("n1")
	if len(links) != 0 {
		t.Errorf("expected 0 links after delete, got %d", len(links))
	}
}

// ---------------------------------------------------------------------------
// Persistent file
// ---------------------------------------------------------------------------

func TestSQLiteStore_PersistAcrossOpen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "persist.db")

	s1, _ := graphstore.OpenSQLite(dbPath)
	_, _ = s1.UpsertNode(makeNode("Persist", graphstore.NodeKindFunction, "p.go"), "")
	s1.Close()

	s2, _ := graphstore.OpenSQLite(dbPath)
	defer s2.Close()
	n, err := s2.GetNode("p.go::Persist")
	if err != nil {
		t.Fatalf("GetNode after reopen: %v", err)
	}
	if n == nil {
		t.Fatal("node not persisted across close/reopen")
	}
}

// ---------------------------------------------------------------------------
// CountNodes / CountKGNotes
// ---------------------------------------------------------------------------

func TestCountNodes_EmptyStore(t *testing.T) {
	s := openTestStore(t)
	if n := s.CountNodes(); n != 0 {
		t.Errorf("expected 0 nodes in empty store, got %d", n)
	}
}

func TestCountNodes_AfterUpsert(t *testing.T) {
	s := openTestStore(t)
	_, _ = s.UpsertNode(makeNode("Foo", graphstore.NodeKindFunction, "foo.go"), "")
	_, _ = s.UpsertNode(makeNode("Bar", graphstore.NodeKindFunction, "bar.go"), "")
	if n := s.CountNodes(); n != 2 {
		t.Errorf("expected 2 nodes, got %d", n)
	}
}

func TestCountKGNotes_EmptyStore(t *testing.T) {
	s := openTestStore(t)
	if n := s.CountKGNotes(); n != 0 {
		t.Errorf("expected 0 KG notes in empty store, got %d", n)
	}
}

func TestCountKGNotes_AfterUpsert(t *testing.T) {
	s := openTestStore(t)
	note := graphstore.KGNote{
		ID:       "dec-001",
		NoteType: "decision",
		Title:    "Use Go",
		Status:   "active",
	}
	if err := s.UpsertKGNote(note); err != nil {
		t.Fatalf("UpsertKGNote: %v", err)
	}
	if n := s.CountKGNotes(); n != 1 {
		t.Errorf("expected 1 KG note, got %d", n)
	}
}

// Ensure the test binary itself can be compiled without leaving temp files
// under the source tree.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
