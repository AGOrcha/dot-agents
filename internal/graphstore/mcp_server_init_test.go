package graphstore

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNewMCPServer_BridgeErrorPath uses a workDir with no .venv and an
// empty PATH to deterministically trigger the bridgeErr branch even on
// machines where CRG happens to be installed.
func TestNewMCPServer_BridgeErrorPath(t *testing.T) {
	t.Setenv("PATH", "")
	workDir := t.TempDir()
	srv := NewMCPServer(workDir)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.bridge != nil {
		t.Error("expected nil bridge when CRG cannot be discovered")
	}
	if srv.bridgeErr == nil {
		t.Error("expected non-nil bridgeErr when CRG cannot be discovered")
	}
}

// TestNewMCPServer_StoreErrorPath forces OpenSQLite to fail by pointing
// KG_HOME at a regular file so the parent of the db path cannot be a directory.
func TestNewMCPServer_StoreErrorPath(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// defaultGraphstoreDBPath joins KG_HOME with "ops/graphstore.db".
	// Setting KG_HOME=<blocker> means the path becomes <blocker>/ops/graphstore.db
	// whose parent <blocker>/ops cannot be created because <blocker> is a file.
	t.Setenv("KG_HOME", blocker)

	workDir := t.TempDir()
	srv := NewMCPServer(workDir)
	if srv == nil {
		t.Fatal("expected non-nil server even on storeErr")
	}
	if srv.store != nil {
		t.Errorf("expected nil store when path is unwritable; got %T", srv.store)
	}
	if srv.storeErr == nil {
		t.Error("expected non-nil storeErr when OpenSQLite fails")
	}
}
