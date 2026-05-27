package rules

import (
	"os"
	"path/filepath"
	"testing"
)

// TestResourceCommandParity_Rules exercises the canonical list/show/remove
// triplet for the rules resource family. The matching mcp + settings
// surfaces have their own parity tests in their respective subpackages —
// see commands/mcp/resource_parity_test.go and
// commands/settings/resource_parity_test.go. Plan
// root-command-decomposition task t12 split the previously-shared table in
// commands/resource_parity_test.go so each row lives next to its handler.
//
// `hooks` shares the same triplet but operates on bundle directories with a
// distinct logical-name resolution path; it is exercised by hooks/*_test.go
// and intentionally not included in this parity family.
func TestResourceCommandParity_Rules(t *testing.T) {
	const (
		scope  = "global"
		sample = "parity-rule.md"
		body   = "---\ndescription: parity rule\n---\n# parity\n"
	)
	samplePath := setupParityScope(t, scope, sample, body)

	if err := RunList(testDeps(false, false, false), scope); err != nil {
		t.Fatalf("rules list (populated): %v", err)
	}

	if err := RunShow(testDeps(false, false, false), scope, sample); err != nil {
		t.Fatalf("rules show: %v", err)
	}

	if err := RunRemove(testDeps(false, true, false), scope, sample); err != nil {
		t.Fatalf("rules remove: %v", err)
	}
	if _, err := os.Stat(samplePath); !os.IsNotExist(err) {
		t.Fatalf("rules remove: expected file gone, stat err=%v", err)
	}

	if err := RunList(testDeps(false, false, false), scope); err != nil {
		t.Fatalf("rules list (empty): %v", err)
	}

	if err := RunShow(testDeps(false, false, false), scope, sample); err == nil {
		t.Fatal("rules show after remove: expected error")
	}
}

// TestResourceCommandParity_Rules_DryRunPreserves confirms the rules
// surface honors --dry-run by leaving the underlying file in place. Mirrors
// the equivalent assertions in mcp/ and settings/ resource_parity_test.go.
func TestResourceCommandParity_Rules_DryRunPreserves(t *testing.T) {
	const (
		scope  = "global"
		sample = "keep.md"
		body   = "---\ndescription: keep\n---\nbody"
	)
	path := setupParityScope(t, scope, sample, body)

	if err := RunRemove(testDeps(true, false, false), scope, sample); err != nil {
		t.Fatalf("rules dry-run remove: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("rules dry-run should preserve file: %v", err)
	}
}

// setupParityScope provisions the canonical agentsHome/rules/<scope>/
// layout and writes the sample file. Returns the absolute sample path.
// Local to this file by design — the parity fixture shape is identical
// across the three resource subpackages but duplicating four lines is
// preferable to lifting a helper to internal/testutil for one caller.
func setupParityScope(t *testing.T, scope, sample, body string) string {
	t.Helper()
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	fakeHome := filepath.Join(tmp, "home")
	scopeDir := filepath.Join(agentsHome, "rules", scope)
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
