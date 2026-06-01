package graphstore_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/internal/graphstore"
)

// openThenCloseStore opens a SQLiteStore and immediately closes its underlying
// connection. Subsequent operations should return database-is-closed errors,
// which exercise the defensive error branches in sqlite.go.
func openThenCloseStore(t *testing.T) *graphstore.SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	s, err := graphstore.OpenSQLite(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return s
}

func TestSQLiteStore_SetMetadata_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if err := s.SetMetadata("k", "v"); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_UpsertNode_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.UpsertNode(graphstore.NodeInfo{Kind: "Function", Name: "foo", FilePath: "f.go"}, "h"); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_UpsertEdge_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.UpsertEdge(graphstore.EdgeInfo{Kind: "CALLS", Source: "a", Target: "b"}); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_RemoveFileData_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if err := s.RemoveFileData("f.go"); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_StoreFileNodesEdges_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if err := s.StoreFileNodesEdges("f.go", nil, nil, "h"); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_GetNodesByFile_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.GetNodesByFile("f.go"); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_GetEdgesBySource_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.GetEdgesBySource("q"); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_GetEdgesByTarget_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.GetEdgesByTarget("q"); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_GetEdgesAmong_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.GetEdgesAmong([]string{"a", "b"}); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_GetAllFiles_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.GetAllFiles(); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_SearchNodes_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.SearchNodes("q", 10); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_GetStats_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.GetStats(); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_GetImpactRadius_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.GetImpactRadius([]string{"f.go"}, 2, 100); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_UpsertKGNote_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if err := s.UpsertKGNote(graphstore.KGNote{ID: "n1", Title: "t"}); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_GetKGNote_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.GetKGNote("n1"); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_SearchKGNotes_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.SearchKGNotes("q", 10); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_ListArchivedKGNotes_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.ListArchivedKGNotes(); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_UpsertNoteSymbolLink_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.UpsertNoteSymbolLink(graphstore.NoteSymbolLink{NoteID: "n1", QualifiedName: "q"}); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_GetLinksForNote_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.GetLinksForNote("n1"); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_GetLinksForSymbol_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if _, err := s.GetLinksForSymbol("q"); err == nil {
		t.Error("expected error after close")
	}
}

func TestSQLiteStore_DeleteNoteSymbolLink_AfterClose(t *testing.T) {
	s := openThenCloseStore(t)
	if err := s.DeleteNoteSymbolLink(1); err == nil {
		t.Error("expected error after close")
	}
}

// TestOpenSQLite_MkdirError exercises the mkdir error path by passing a path
// whose parent is a regular file (cannot become a directory).
func TestOpenSQLite_MkdirError(t *testing.T) {
	dir := t.TempDir()
	regularFile := filepath.Join(dir, "blocker")
	// Create a regular file at dir/blocker.
	if err := os.WriteFile(regularFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Now ask OpenSQLite to create a file under dir/blocker/foo.db — its parent
	// dir/blocker is a regular file, so MkdirAll must fail.
	target := filepath.Join(regularFile, "foo.db")
	if _, err := graphstore.OpenSQLite(target); err == nil {
		t.Error("expected mkdir error when parent is a regular file")
	} else if !strings.Contains(err.Error(), "create db dir") {
		t.Errorf("expected 'create db dir' in error, got %v", err)
	}
}
