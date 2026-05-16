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

// TestNewMCPServer_BridgeSuccessPath shims a .venv/bin/code-review-graph so
// NewCRGBridge succeeds, exercising the bridge-assigned branch
// (mcp_server.go:75-77). On CI runners with no CRG installed, only the
// bridgeErr branch is reached; this test seeds a synthetic binary to cover
// the success path deterministically.
func TestNewMCPServer_BridgeSuccessPath(t *testing.T) {
	workDir := t.TempDir()
	venvBin := filepath.Join(workDir, ".venv", "bin")
	if err := os.MkdirAll(venvBin, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(venvBin, "code-review-graph")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Independently force the store error path so we exercise only the bridge
	// success branch without depending on a working SQLite path. Pointing
	// KG_HOME at a regular file makes defaultGraphstoreDBPath's parent
	// (<file>/ops) impossible to create, so OpenSQLite fails fast and leaves
	// no open DB handle. (Setting KG_HOME=workDir instead would *succeed* and
	// leak an unclosed sqlite handle that blocks t.TempDir RemoveAll on
	// Windows.)
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KG_HOME", blocker)

	srv := NewMCPServer(workDir)
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
	if srv.bridge == nil {
		t.Error("expected non-nil bridge when CRG is discoverable")
	}
	if srv.bridgeErr != nil {
		t.Errorf("expected nil bridgeErr on success; got %v", srv.bridgeErr)
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
