package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

// setupAgentsHomeAndHome provisions an isolated AGENTS_HOME + HOME pair
// and returns the agents-home path. Mirrors the same-named helper in
// commands/coverage_test.go so the RunE coverage assertions inside the
// subpackage have the same fixture surface as the parent.
func setupAgentsHomeAndHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	fakeHome := filepath.Join(tmp, "home")
	for _, d := range []string{agentsHome, fakeHome} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatalf("setup dir %s: %v", d, err)
		}
	}
	t.Setenv("HOME", fakeHome)
	t.Setenv("AGENTS_HOME", agentsHome)
	return agentsHome
}

// writeFile is a tiny helper for the canonical-file fixtures the show /
// remove RunE assertions need. Mirrors writeMCPConfig in the parent
// commands/mcp_test.go (which the parent must retain until t12
// migrates commands/coverage_test.go to testutil.WriteScopeFile).
func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestNewListCmd_RunE covers the cobra wiring for `da mcp list`: the
// constructed command's RunE invokes RunList for both the implicit-scope
// (nil args) and explicit-scope branches.
func TestNewListCmd_RunE(t *testing.T) {
	setupAgentsHomeAndHome(t)
	deps := makeDeps(false, false, false)
	cmd := NewListCmd(deps)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Errorf("mcp list RunE: %v", err)
	}
	if err := cmd.RunE(cmd, []string{"some-project"}); err != nil {
		t.Errorf("mcp list with scope RunE: %v", err)
	}
}

// TestNewShowCmd_RunE covers the cobra wiring for `da mcp show`.
func TestNewShowCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHomeAndHome(t)
	writeFile(t, filepath.Join(agentsHome, "mcp", "global"), "showme.json", "{}")
	deps := makeDeps(false, false, false)
	cmd := NewShowCmd(deps)
	if err := cmd.RunE(cmd, []string{"global", "showme.json"}); err != nil {
		t.Errorf("mcp show RunE: %v", err)
	}
}

// TestNewRemoveCmd_RunE covers the cobra wiring for `da mcp remove`,
// using Yes:true to bypass the interactive prompt.
func TestNewRemoveCmd_RunE(t *testing.T) {
	agentsHome := setupAgentsHomeAndHome(t)
	writeFile(t, filepath.Join(agentsHome, "mcp", "global"), "rmme.json", "{}")
	deps := makeDeps(false, true, false)
	cmd := NewRemoveCmd(deps)
	if err := cmd.RunE(cmd, []string{"global", "rmme.json"}); err != nil {
		t.Errorf("mcp remove RunE: %v", err)
	}
}
