package graphstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// errBackendProbe stands in for "the selected backend's tooling is absent".
var errBackendProbe = errors.New("code graph backend unavailable")

// NewMCPServer is the pre-cutover constructor, kept because the bridge is the
// documented rollback backend. When the release cannot be discovered the
// server must still be usable — it advertises the full surface and reports an
// explicit capability failure per call — rather than being nil or panicking.
func TestNewMCPServerWithoutADiscoverableRelease(t *testing.T) {
	t.Setenv("PATH", "")
	srv := NewMCPServer(t.TempDir())
	if srv == nil {
		t.Fatal("expected a server even with no discoverable release")
	}
	if srv.provider != nil {
		t.Error("expected no provider when the release cannot be discovered")
	}
	if srv.providerErr == nil {
		t.Error("expected the discovery failure to be retained")
	}
	if srv.bridge == nil {
		t.Fatal("expected a tool bridge to be wired even when it is unavailable")
	}
	if srv.bridge.Available() {
		t.Error("an undiscoverable release must not report itself available")
	}
	if srv.bridge.Unavailable() == "" {
		t.Error("an unavailable bridge must explain why")
	}
}

// With a discoverable release the bridge becomes the provider, which is the
// rollback configuration.
func TestNewMCPServerWithADiscoverableRelease(t *testing.T) {
	workDir := t.TempDir()
	venvBin := filepath.Join(workDir, ".venv", "bin")
	if err := os.MkdirAll(venvBin, 0o755); err != nil {
		t.Fatal(err)
	}
	shim := filepath.Join(venvBin, "code-review-graph")
	if err := os.WriteFile(shim, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	srv := NewMCPServer(workDir)
	if srv.provider == nil {
		t.Fatal("expected the discovered bridge to be the provider")
	}
	if srv.providerErr != nil {
		t.Errorf("unexpected provider error: %v", srv.providerErr)
	}
}
