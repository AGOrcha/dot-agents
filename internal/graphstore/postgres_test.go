package graphstore_test

import (
	"context"
	"os"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/graphstore"
)

// openPGTestStore opens a PostgresStore using the TEST_PG_URL environment variable.
// If the variable is not set the test is skipped.
func openPGTestStore(t *testing.T) *graphstore.PostgresStore {
	t.Helper()
	dsn := os.Getenv("TEST_PG_URL")
	if dsn == "" {
		t.Skip("TEST_PG_URL not set — skipping Postgres tests")
	}

	ctx := context.Background()
	s, err := graphstore.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ---------------------------------------------------------------------------
// Schema initialisation
// ---------------------------------------------------------------------------

func TestOpenPostgres_CreatesSchema(t *testing.T) {
	s := openPGTestStore(t)
	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("GetStats after open: %v", err)
	}
	// A freshly provisioned DB should be empty (or at least not error).
	_ = stats
}

func TestOpenPostgres_Idempotent(t *testing.T) {
	dsn := os.Getenv("TEST_PG_URL")
	if dsn == "" {
		t.Skip("TEST_PG_URL not set — skipping Postgres tests")
	}
	ctx := context.Background()

	s1, err := graphstore.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	s1.Close()

	s2, err := graphstore.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	s2.Close()
}

// ---------------------------------------------------------------------------
// Metadata
// ---------------------------------------------------------------------------

func TestPG_Metadata_RoundTrip(t *testing.T) {
	s := openPGTestStore(t)
	if err := s.SetMetadata("pg_test_key", "pg_value"); err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}
	val, err := s.GetMetadata("pg_test_key")
	if err != nil {
		t.Fatalf("GetMetadata: %v", err)
	}
	if val != "pg_value" {
		t.Errorf("want 'pg_value', got %q", val)
	}
}

func TestPG_Metadata_MissingKey(t *testing.T) {
	s := openPGTestStore(t)
	val, err := s.GetMetadata("nonexistent_pg_key_xyz")
	if err != nil {
		t.Fatalf("GetMetadata missing key: %v", err)
	}
	if val != "" {
		t.Errorf("expected empty string for missing key, got %q", val)
	}
}

func TestPG_Metadata_Overwrite(t *testing.T) {
	s := openPGTestStore(t)
	_ = s.SetMetadata("pg_overwrite_key", "v1")
	_ = s.SetMetadata("pg_overwrite_key", "v2")
	val, _ := s.GetMetadata("pg_overwrite_key")
	if val != "v2" {
		t.Errorf("want v2, got %q", val)
	}
}

// ---------------------------------------------------------------------------
// Node upsert + read
// ---------------------------------------------------------------------------

func TestPG_UpsertNode_Create(t *testing.T) {
	s := openPGTestStore(t)
	id, err := s.UpsertNode(makeNode("pgMain", graphstore.NodeKindFunction, "pg_main.go"), "")
	if err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero ID")
	}
}

func TestPG_UpsertNode_Update(t *testing.T) {
	s := openPGTestStore(t)
	node := makeNode("pgHandler", graphstore.NodeKindFunction, "pg_handler.go")
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

func TestPG_GetNode_RoundTrip(t *testing.T) {
	s := openPGTestStore(t)
	node := graphstore.NodeInfo{
		Kind: graphstore.NodeKindFunction, Name: "pgRun",
		FilePath: "pg_cmd/main.go", Language: "go",
		LineStart: 5, LineEnd: 20,
	}
	_, err := s.UpsertNode(node, "pgabc123")
	if err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}

	got, err := s.GetNode("pg_cmd/main.go::pgRun")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got == nil {
		t.Fatal("GetNode returned nil")
	}
	if got.Name != "pgRun" || got.Language != "go" || got.LineStart != 5 {
		t.Errorf("unexpected node: %+v", got)
	}
}

func TestPG_GetNode_NotFound(t *testing.T) {
	s := openPGTestStore(t)
	got, err := s.GetNode("missing::pg_node_xyz_not_here")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing node, got %+v", got)
	}
}

func TestPG_GetNodesByFile(t *testing.T) {
	s := openPGTestStore(t)
	file := "pg_pkg/foo_unique.go"
	for _, name := range []string{"pgA", "pgB", "pgC"} {
		_, err := s.UpsertNode(makeNode(name, graphstore.NodeKindFunction, file), "")
		if err != nil {
			t.Fatalf("UpsertNode %s: %v", name, err)
		}
	}

	nodes, err := s.GetNodesByFile(file)
	if err != nil {
		t.Fatalf("GetNodesByFile: %v", err)
	}
	if len(nodes) < 3 {
		t.Errorf("want at least 3 nodes, got %d", len(nodes))
	}
}

// ---------------------------------------------------------------------------
// Edge upsert + read
// ---------------------------------------------------------------------------

func TestPG_UpsertEdge_Create(t *testing.T) {
	s := openPGTestStore(t)
	id, err := s.UpsertEdge(makeEdge("pg_pkg::A", "pg_pkg::B", graphstore.EdgeKindCalls))
	if err != nil {
		t.Fatalf("UpsertEdge: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero edge ID")
	}
}

func TestPG_UpsertEdge_Update(t *testing.T) {
	s := openPGTestStore(t)
	e := makeEdge("pg_src_E", "pg_tgt_E", graphstore.EdgeKindCalls)
	id1, _ := s.UpsertEdge(e)
	e.Line = 99
	id2, err := s.UpsertEdge(e)
	if err != nil {
		t.Fatalf("UpsertEdge update: %v", err)
	}
	if id1 != id2 {
		t.Errorf("expected same id on update: id1=%d id2=%d", id1, id2)
	}
}

func TestPG_GetEdgesBySource(t *testing.T) {
	s := openPGTestStore(t)
	_, _ = s.UpsertEdge(makeEdge("pg_X", "pg_Y", graphstore.EdgeKindCalls))
	_, _ = s.UpsertEdge(makeEdge("pg_X", "pg_Z", graphstore.EdgeKindImportsFrom))
	_, _ = s.UpsertEdge(makeEdge("pg_W", "pg_X", graphstore.EdgeKindCalls))

	edges, err := s.GetEdgesBySource("pg_X")
	if err != nil {
		t.Fatalf("GetEdgesBySource: %v", err)
	}
	if len(edges) < 2 {
		t.Errorf("want at least 2 edges from pg_X, got %d", len(edges))
	}
}

func TestPG_GetEdgesByTarget(t *testing.T) {
	s := openPGTestStore(t)
	_, _ = s.UpsertEdge(makeEdge("pg_A", "pg_Z_tgt", graphstore.EdgeKindCalls))
	_, _ = s.UpsertEdge(makeEdge("pg_B", "pg_Z_tgt", graphstore.EdgeKindCalls))
	_, _ = s.UpsertEdge(makeEdge("pg_A", "pg_Y_tgt", graphstore.EdgeKindCalls))

	edges, err := s.GetEdgesByTarget("pg_Z_tgt")
	if err != nil {
		t.Fatalf("GetEdgesByTarget: %v", err)
	}
	if len(edges) < 2 {
		t.Errorf("want at least 2 edges to pg_Z_tgt, got %d", len(edges))
	}
}

// ---------------------------------------------------------------------------
// RemoveFileData + StoreFileNodesEdges
// ---------------------------------------------------------------------------

func TestPG_RemoveFileData(t *testing.T) {
	s := openPGTestStore(t)
	file := "pg_remove_test_unique.go"
	_, _ = s.UpsertNode(makeNode("pgFn1", graphstore.NodeKindFunction, file), "")
	_, _ = s.UpsertNode(makeNode("pgFn2", graphstore.NodeKindFunction, file), "")
	_, _ = s.UpsertEdge(makeEdge(file+"::pgFn1", file+"::pgFn2", graphstore.EdgeKindCalls))

	if err := s.RemoveFileData(file); err != nil {
		t.Fatalf("RemoveFileData: %v", err)
	}
	nodes, _ := s.GetNodesByFile(file)
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes after remove, got %d", len(nodes))
	}
}

func TestPG_StoreFileNodesEdges_Atomic(t *testing.T) {
	s := openPGTestStore(t)
	file := "pg_atomic_unique.go"
	nodes := []graphstore.NodeInfo{
		makeNode("pgF1", graphstore.NodeKindFunction, file),
		makeNode("pgF2", graphstore.NodeKindFunction, file),
	}
	edges := []graphstore.EdgeInfo{
		makeEdge(file+"::pgF1", file+"::pgF2", graphstore.EdgeKindCalls),
	}

	if err := s.StoreFileNodesEdges(file, nodes, edges, "pg_hash"); err != nil {
		t.Fatalf("StoreFileNodesEdges: %v", err)
	}

	got, _ := s.GetNodesByFile(file)
	if len(got) < 2 {
		t.Errorf("want at least 2 nodes, got %d", len(got))
	}

	// Re-store with only 1 node — should replace.
	if err := s.StoreFileNodesEdges(file, nodes[:1], nil, "pg_hash2"); err != nil {
		t.Fatalf("StoreFileNodesEdges replace: %v", err)
	}
	got, _ = s.GetNodesByFile(file)
	if len(got) != 1 {
		t.Errorf("want 1 node after replace, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// SearchNodes
// ---------------------------------------------------------------------------

func TestPG_SearchNodes(t *testing.T) {
	s := openPGTestStore(t)
	for _, name := range []string{"pgHandleRequest", "pgParseRequest", "pgBuildResponse"} {
		_, _ = s.UpsertNode(makeNode(name, graphstore.NodeKindFunction, "pg_server.go"), "")
	}

	results, err := s.SearchNodes("pgRequest", 10)
	if err != nil {
		t.Fatalf("SearchNodes: %v", err)
	}
	if len(results) < 2 {
		t.Errorf("want at least 2 results for 'pgRequest', got %d", len(results))
	}
}

func TestPG_SearchNodes_Limit(t *testing.T) {
	s := openPGTestStore(t)
	for i := 0; i < 5; i++ {
		_, _ = s.UpsertNode(graphstore.NodeInfo{
			Kind: graphstore.NodeKindFunction, Name: "pgFooLimitHelper",
			FilePath: "pg_limit_helper_" + string(rune('a'+i)) + ".go", Language: "go",
		}, "")
	}

	results, err := s.SearchNodes("pgFooLimitHelper", 2)
	if err != nil {
		t.Fatalf("SearchNodes limit: %v", err)
	}
	if len(results) > 2 {
		t.Errorf("limit 2 not respected, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// GetStats
// ---------------------------------------------------------------------------

func TestPG_GetStats(t *testing.T) {
	s := openPGTestStore(t)
	stats, err := s.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	// Basic sanity: counts are non-negative.
	if stats.TotalNodes < 0 || stats.TotalEdges < 0 {
		t.Errorf("unexpected negative stats: %+v", stats)
	}
}

// ---------------------------------------------------------------------------
// GetEdgesAmong (nil / empty input)
// ---------------------------------------------------------------------------

func TestPG_GetEdgesAmong_Empty(t *testing.T) {
	s := openPGTestStore(t)
	edges, err := s.GetEdgesAmong(nil)
	if err != nil {
		t.Fatalf("GetEdgesAmong nil: %v", err)
	}
	if len(edges) != 0 {
		t.Errorf("expected 0 edges for nil input, got %d", len(edges))
	}
}

// ---------------------------------------------------------------------------
// KG notes
// ---------------------------------------------------------------------------

func TestPG_UpsertGetKGNote(t *testing.T) {
	s := openPGTestStore(t)
	note := graphstore.KGNote{
		ID:       "pg-decision-001",
		Title:    "Use Postgres as warm layer",
		NoteType: "decision",
		Status:   "active",
		Summary:  "Chosen for scalability.",
		FilePath: "/kg/notes/pg-decision-001.md",
		Version:  1,
	}
	if err := s.UpsertKGNote(note); err != nil {
		t.Fatalf("UpsertKGNote: %v", err)
	}

	got, err := s.GetKGNote("pg-decision-001")
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

func TestPG_GetKGNote_NotFound(t *testing.T) {
	s := openPGTestStore(t)
	got, err := s.GetKGNote("pg-missing-xyz-not-here")
	if err != nil {
		t.Fatalf("GetKGNote missing: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestPG_UpsertKGNote_UpdatesInPlace(t *testing.T) {
	s := openPGTestStore(t)
	note := graphstore.KGNote{
		ID: "pg-n1", Title: "Old PG Title", NoteType: "concept", Status: "active", FilePath: "pg_f.md",
	}
	_ = s.UpsertKGNote(note)
	note.Title = "New PG Title"
	note.Version = 2
	_ = s.UpsertKGNote(note)

	got, _ := s.GetKGNote("pg-n1")
	if got.Title != "New PG Title" || got.Version != 2 {
		t.Errorf("expected updated note, got %+v", got)
	}
}

func TestPG_SearchKGNotes(t *testing.T) {
	s := openPGTestStore(t)
	for _, n := range []graphstore.KGNote{
		{ID: "pg-sn1", Title: "Postgres architecture", NoteType: "decision", Status: "active", FilePath: "pg_a.md"},
		{ID: "pg-sn2", Title: "Graph theory", Summary: "concepts for Postgres graph", NoteType: "concept", Status: "active", FilePath: "pg_b.md"},
		{ID: "pg-sn3", Title: "Unrelated PG Note", NoteType: "entity", Status: "active", FilePath: "pg_c.md"},
	} {
		_ = s.UpsertKGNote(n)
	}

	results, err := s.SearchKGNotes("Postgres", 10)
	if err != nil {
		t.Fatalf("SearchKGNotes: %v", err)
	}
	if len(results) < 2 {
		t.Errorf("want at least 2 Postgres notes, got %d", len(results))
	}
}

func TestPG_ListArchivedKGNotes(t *testing.T) {
	s := openPGTestStore(t)
	_ = s.UpsertKGNote(graphstore.KGNote{
		ID: "pg-archived1", Title: "Old PG Decision", NoteType: "decision", Status: "superseded",
		FilePath: "pg_a.md", ArchivedAt: "2026-01-01T00:00:00Z",
	})
	_ = s.UpsertKGNote(graphstore.KGNote{
		ID: "pg-active1", Title: "Active PG", NoteType: "concept", Status: "active", FilePath: "pg_b.md",
	})

	archived, err := s.ListArchivedKGNotes()
	if err != nil {
		t.Fatalf("ListArchivedKGNotes: %v", err)
	}
	found := false
	for _, a := range archived {
		if a.ID == "pg-archived1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected pg-archived1 in archived list, got %+v", archived)
	}
}

// ---------------------------------------------------------------------------
// Note→symbol links
// ---------------------------------------------------------------------------

func TestPG_NoteSymbolLink_RoundTrip(t *testing.T) {
	s := openPGTestStore(t)
	link := graphstore.NoteSymbolLink{
		NoteID: "pg-decision-001", QualifiedName: "pg_pkg::Store", LinkKind: "documents",
	}
	id, err := s.UpsertNoteSymbolLink(link)
	if err != nil {
		t.Fatalf("UpsertNoteSymbolLink: %v", err)
	}
	if id == 0 {
		t.Error("expected non-zero link ID")
	}

	links, err := s.GetLinksForNote("pg-decision-001")
	if err != nil {
		t.Fatalf("GetLinksForNote: %v", err)
	}
	found := false
	for _, l := range links {
		if l.QualifiedName == "pg_pkg::Store" {
			found = true
		}
	}
	if !found {
		t.Errorf("link not found in results: %+v", links)
	}
}

func TestPG_NoteSymbolLink_Idempotent(t *testing.T) {
	s := openPGTestStore(t)
	link := graphstore.NoteSymbolLink{NoteID: "pg-n1-idem", QualifiedName: "pg_pkg::Fn", LinkKind: "mentions"}
	id1, _ := s.UpsertNoteSymbolLink(link)
	id2, _ := s.UpsertNoteSymbolLink(link)
	if id1 != id2 {
		t.Errorf("expected idempotent insert, got id1=%d id2=%d", id1, id2)
	}

	links, _ := s.GetLinksForNote("pg-n1-idem")
	count := 0
	for _, l := range links {
		if l.QualifiedName == "pg_pkg::Fn" && l.LinkKind == "mentions" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected 1 link after idempotent upsert, got %d", count)
	}
}

func TestPG_GetLinksForSymbol(t *testing.T) {
	s := openPGTestStore(t)
	_, _ = s.UpsertNoteSymbolLink(graphstore.NoteSymbolLink{NoteID: "pg-n1-lfs", QualifiedName: "pg_pkg::FLfs", LinkKind: "mentions"})
	_, _ = s.UpsertNoteSymbolLink(graphstore.NoteSymbolLink{NoteID: "pg-n2-lfs", QualifiedName: "pg_pkg::FLfs", LinkKind: "documents"})
	_, _ = s.UpsertNoteSymbolLink(graphstore.NoteSymbolLink{NoteID: "pg-n3-lfs", QualifiedName: "pg_pkg::GLfs", LinkKind: "mentions"})

	links, err := s.GetLinksForSymbol("pg_pkg::FLfs")
	if err != nil {
		t.Fatalf("GetLinksForSymbol: %v", err)
	}
	if len(links) < 2 {
		t.Errorf("want at least 2 links for pg_pkg::FLfs, got %d", len(links))
	}
}

func TestPG_DeleteNoteSymbolLink(t *testing.T) {
	s := openPGTestStore(t)
	id, _ := s.UpsertNoteSymbolLink(graphstore.NoteSymbolLink{
		NoteID: "pg-n1-del", QualifiedName: "pg_q1_del", LinkKind: "mentions",
	})
	if err := s.DeleteNoteSymbolLink(id); err != nil {
		t.Fatalf("DeleteNoteSymbolLink: %v", err)
	}
	links, _ := s.GetLinksForNote("pg-n1-del")
	for _, l := range links {
		if l.ID == id {
			t.Errorf("link still present after delete: %+v", l)
		}
	}
}

// ---------------------------------------------------------------------------
// Interface compliance
// ---------------------------------------------------------------------------

// TestPG_ImplementsStore verifies at compile time that *PostgresStore satisfies
// the Store interface. This catches missing methods at build time rather than
// waiting for a runtime assignment.
func TestPG_ImplementsStore(t *testing.T) {
	var _ graphstore.Store = (*graphstore.PostgresStore)(nil)
}
