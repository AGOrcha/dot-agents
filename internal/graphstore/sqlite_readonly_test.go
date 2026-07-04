package graphstore

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildRealStore materialises a real (WAL) store with one node and returns its
// path, closed and ready for a read-only reopen.
func buildRealStore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "graphstore.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	if _, err := s.UpsertNode(NodeInfo{Kind: NodeKindFunction, Name: "Bar", FilePath: "foo/foo.go", Language: "go"}, "h"); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close seed store: %v", err)
	}
	return path
}

// The read-only open is creation-safe BY CONSTRUCTION: opening an absent store
// fails (as a wrapped os.ErrNotExist) and leaves NO file behind. There is no
// pre-open stat gating creation here — the open itself (mode=ro) is what
// guarantees nothing is written, which this proves by asserting the path is
// still absent afterward.
func TestOpenSQLiteReadOnly_MissingFileIsCreationSafe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "graphstore.db")
	store, err := OpenSQLiteReadOnly(path)
	if err == nil {
		store.Close()
		t.Fatal("opening an absent read-only store should error")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("absent store error should wrap os.ErrNotExist, got %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("read-only open created a store file: stat err = %v", statErr)
	}
	// The parent dir must not be created either (no MkdirAll on the read path).
	if _, statErr := os.Stat(filepath.Dir(path)); !os.IsNotExist(statErr) {
		t.Error("read-only open created the parent directory")
	}
}

// A read-only open of an existing store reads back its contents.
func TestOpenSQLiteReadOnly_ReadsExistingStore(t *testing.T) {
	path := buildRealStore(t)
	store, err := OpenSQLiteReadOnly(path)
	if err != nil {
		t.Fatalf("OpenSQLiteReadOnly: %v", err)
	}
	defer store.Close()
	stats, err := store.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalNodes != 1 {
		t.Errorf("read-only store TotalNodes = %d, want 1", stats.TotalNodes)
	}
}

// The handle is genuinely read-only: a write attempt fails rather than mutating
// the store (further proof the open carried no create/write capability).
func TestOpenSQLiteReadOnly_RejectsWrites(t *testing.T) {
	path := buildRealStore(t)
	store, err := OpenSQLiteReadOnly(path)
	if err != nil {
		t.Fatalf("OpenSQLiteReadOnly: %v", err)
	}
	defer store.Close()
	if _, err := store.UpsertNode(NodeInfo{Kind: NodeKindFunction, Name: "New", FilePath: "x.go"}, "h"); err == nil {
		t.Fatal("a write to a read-only store should fail")
	}
}

// The sqlOpen seam failure surfaces as a wrapped open error.
func TestOpenSQLiteReadOnly_SQLOpenError(t *testing.T) {
	withSQLOpen(t, func(driver, dsn string) (*sql.DB, error) {
		return nil, errors.New("synthetic open failure")
	})
	if _, err := OpenSQLiteReadOnly(filepath.Join(t.TempDir(), "x.db")); err == nil {
		t.Fatal("expected error when sql.Open fails")
	}
}

// A non-not-exist open failure (a regular file where the store's parent dir
// should be, yielding ENOTDIR) is reported as an open error, NOT as a missing
// store — so callers do not misreport it as "not built".
func TestOpenSQLiteReadOnly_NonNotExistError(t *testing.T) {
	dir := t.TempDir()
	notADir := filepath.Join(dir, "ops")
	if err := os.WriteFile(notADir, []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	_, err := OpenSQLiteReadOnly(filepath.Join(notADir, "graphstore.db"))
	if err == nil {
		t.Fatal("expected an open error for a store under a non-directory")
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Errorf("ENOTDIR-style failure must not be classified as a missing store: %v", err)
	}
}

func TestReadOnlyDSN(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		wantPrefix string
	}{
		{"absolute_unix", "/var/kg/ops/graphstore.db", "file:///var/kg/ops/graphstore.db?"},
		{"windows_drive", `C:/kg/ops/graphstore.db`, "file:///C:/kg/ops/graphstore.db?"},
		{"relative", "kg/graphstore.db", "file:///kg/graphstore.db?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := readOnlyDSN(tc.path)
			if !strings.HasPrefix(got, tc.wantPrefix) {
				t.Errorf("readOnlyDSN(%q) = %q, want prefix %q", tc.path, got, tc.wantPrefix)
			}
			if !strings.HasSuffix(got, "mode=ro") {
				t.Errorf("readOnlyDSN(%q) = %q, want mode=ro suffix", tc.path, got)
			}
		})
	}
	// A path with a space must be percent-encoded into a valid URI.
	if got := readOnlyDSN("/a b/graphstore.db"); !strings.Contains(got, "%20") {
		t.Errorf("readOnlyDSN did not encode the space: %q", got)
	}
}
