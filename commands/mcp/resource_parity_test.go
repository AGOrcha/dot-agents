package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResourceCommandParity_MCP exercises the canonical list/show/remove
// triplet for the mcp resource family. This is the mcp-row of the
// per-resource parity contract previously expressed as a single table in
// commands/resource_parity_test.go; plan root-command-decomposition task
// t12 re-homed each row into its owning subpackage so the parity contract
// runs in lockstep with the per-subpackage *_test.go files (no cross-
// package fixture duplication, no stale parent helper).
//
// The same shape is exercised in commands/rules/resource_parity_test.go
// and commands/settings/resource_parity_test.go. Behavioral drift between
// the three surfaces is caught when any single per-subpackage test
// diverges from the others.
//
// `hooks` shares the same list/show/remove triplet but operates on bundle
// directories (HOOK.yaml) and has its own logical-name resolution path
// through commands/hooks; it is exercised by hooks/*_test.go and
// intentionally not included in this parity family.
func TestResourceCommandParity_MCP(t *testing.T) {
	const (
		scope  = "global"
		sample = "parity-mcp.json"
		body   = `{"mcpServers":{"parity":{"command":"echo"}}}`
	)
	samplePath := setupParityScope(t, scope, sample, body)

	if err := RunList(scope); err != nil {
		t.Fatalf("mcp list (populated): %v", err)
	}

	if err := RunShow(Deps{}, scope, sample); err != nil {
		t.Fatalf("mcp show: %v", err)
	}

	if err := RunRemove(makeDeps(false, true, false), scope, sample); err != nil {
		t.Fatalf("mcp remove: %v", err)
	}
	if _, err := os.Stat(samplePath); !os.IsNotExist(err) {
		t.Fatalf("mcp remove: expected file gone, stat err=%v", err)
	}

	if err := RunList(scope); err != nil {
		t.Fatalf("mcp list (empty): %v", err)
	}

	if err := RunShow(Deps{}, scope, sample); err == nil {
		t.Fatal("mcp show after remove: expected error")
	}
}

// TestResourceCommandParity_MCP_DryRunPreserves confirms the mcp surface
// honors --dry-run by leaving the underlying file in place. Mirrors the
// equivalent assertions in rules/ and settings/ resource_parity_test.go.
func TestResourceCommandParity_MCP_DryRunPreserves(t *testing.T) {
	const (
		scope  = "global"
		sample = "keep.json"
		body   = `{}`
	)
	path := setupParityScope(t, scope, sample, body)

	if err := RunRemove(makeDeps(true, false, false), scope, sample); err != nil {
		t.Fatalf("mcp dry-run remove: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("mcp dry-run should preserve file: %v", err)
	}
}

// setupParityScope provisions the canonical agentsHome/mcp/<scope>/ layout
// and writes the sample file. Returns the absolute sample path. Local to
// this file because the parity fixture shape is identical across the three
// resource subpackages — duplicating four lines is preferable to lifting a
// helper to internal/testutil only to host one caller.
func setupParityScope(t *testing.T, scope, sample, body string) string {
	t.Helper()
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	fakeHome := filepath.Join(tmp, "home")
	scopeDir := filepath.Join(agentsHome, "mcp", scope)
	if err := os.MkdirAll(scopeDir, 0o755); err != nil {
		t.Fatalf("setup scope dir: %v", err)
	}
	if err := os.MkdirAll(fakeHome, 0o755); err != nil {
		t.Fatalf("setup fake home: %v", err)
	}
	t.Setenv("HOME", fakeHome)
	t.Setenv("AGENTS_HOME", agentsHome)

	samplePath := filepath.Join(scopeDir, sample)
	if err := os.WriteFile(samplePath, []byte(body), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}
	return samplePath
}
