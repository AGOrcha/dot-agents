// Package graphstore — coverage for the DiscoverCRGBin not-found error
// branch and the NewCRGBridge discover-error branch.
package graphstore

import (
	"os"
	"strings"
	"testing"
)

// TestDiscoverCRGBin_NotFound exercises the final "not found" error return.
// We clear PATH so exec.LookPath fails, and use a repo dir whose .venv
// candidates do not exist.
func TestDiscoverCRGBin_NotFound(t *testing.T) {
	t.Setenv("PATH", "")
	if _, err := DiscoverCRGBin(t.TempDir()); err == nil {
		t.Fatal("expected not-found error when PATH is empty and no .venv exists")
	} else if !strings.Contains(err.Error(), "code-review-graph not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestNewCRGBridge_DiscoverError exercises the NewCRGBridge branch where
// DiscoverCRGBin fails. Same PATH-clearing trick.
func TestNewCRGBridge_DiscoverError(t *testing.T) {
	t.Setenv("PATH", "")
	if _, err := NewCRGBridge(t.TempDir()); err == nil {
		t.Fatal("expected NewCRGBridge to propagate DiscoverCRGBin error")
	}
}

// TestDiscoverCRGBin_PATHFallback covers the exec.LookPath success branch
// when the binary lives only on PATH (no .venv anywhere).
func TestDiscoverCRGBin_PATHFallback(t *testing.T) {
	dir := t.TempDir()
	binDir := dir + "/bin"
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := binDir + "/code-review-graph"
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	// Use a different repo dir so no .venv candidate is found.
	repo := t.TempDir()
	got, err := DiscoverCRGBin(repo)
	if err != nil {
		t.Fatalf("DiscoverCRGBin: %v", err)
	}
	if got != bin {
		t.Errorf("expected PATH lookup to return %s, got %s", bin, got)
	}
}
