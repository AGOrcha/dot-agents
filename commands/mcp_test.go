package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

func TestRunMCPList_ListsMCPConfigs(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	testutil.WriteScopeFile(t, agentsHome, "mcp", "global", "test-mcp.json", []byte(`{"mcpServers": {"test": {"command": "echo"}}}`))

	if err := runMCPList("global"); err != nil {
		t.Fatalf("runMCPList: %v", err)
	}
}

func TestRunMCPList_EmptyScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	mcpDir := filepath.Join(agentsHome, "mcp", "global")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// Empty dir — should print info message, not error.
	if err := runMCPList("global"); err != nil {
		t.Fatalf("runMCPList with empty scope: %v", err)
	}
}

func TestRunMCPList_MissingScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := runMCPList("nonexistent"); err != nil {
		t.Fatalf("runMCPList with missing scope: %v", err)
	}
}

func TestRunMCPShow_ReadsMCPConfig(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	testutil.WriteScopeFile(t, agentsHome, "mcp", "global", "demo.json", []byte(`{"mcpServers": {"demo": {"command": "node"}}}`))

	if err := runMCPShow("global", "demo.json"); err != nil {
		t.Fatalf("runMCPShow: %v", err)
	}
}

func TestRunMCPShow_NotFound(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	mcpDir := filepath.Join(agentsHome, "mcp", "global")
	if err := os.MkdirAll(mcpDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	err := runMCPShow("global", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing MCP config")
	}
}

func TestFindMCPSpec_EmptyName(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	_, err := findMCPSpec(agentsHome, "global", "")
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func makeMCPDeps(dryRun, yes, force bool) mcpDeps {
	return mcpDeps{
		Flags:              cmdutil.CanonicalCmdFlags{DryRun: dryRun, Yes: yes, Force: force},
		maxArgsWithHints:   MaximumNArgsWithHints,
		exactArgsWithHints: ExactArgsWithHints,
	}
}

// writeMCPConfig is retained for commands/coverage_test.go (out of write_scope
// for this refactor). All in-file callers were migrated to
// testutil.WriteScopeFile; when coverage_test.go migrates, this helper can be
// deleted outright. Tracked as a t5-followup finding.
func writeMCPConfig(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFindMCPSpec_Found(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "mcp", "global", "found.json", []byte("{}"))
	t.Setenv("AGENTS_HOME", agentsHome)

	spec, err := findMCPSpec(agentsHome, "global", "found.json")
	if err != nil {
		t.Fatalf("findMCPSpec: %v", err)
	}
	if spec == nil || spec.BaseName != "found.json" {
		t.Errorf("unexpected spec: %+v", spec)
	}
}

func TestFindMCPSpec_NotFoundHintsAtList(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "mcp", "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	_, err := findMCPSpec(agentsHome, "global", "absent")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if !strings.Contains(strings.Join(cliErr.Hints, " "), "da mcp list") {
		t.Errorf("missing hint pointing at `da mcp list`: %v", cliErr.Hints)
	}
}

func TestRunMCPRemove_DryRun_KeepsFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "mcp", "global", "dry.json", []byte("{}"))
	t.Setenv("AGENTS_HOME", agentsHome)

	deps := makeMCPDeps(true, false, false)
	if err := runMCPRemove(deps, "global", "dry.json"); err != nil {
		t.Fatalf("runMCPRemove dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "dry.json")); err != nil {
		t.Fatalf("dry-run should preserve file: %v", err)
	}
}

func TestRunMCPRemove_Force_DeletesFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "mcp", "global", "kill.json", []byte("{}"))
	t.Setenv("AGENTS_HOME", agentsHome)

	deps := makeMCPDeps(false, true, false)
	if err := runMCPRemove(deps, "global", "kill.json"); err != nil {
		t.Fatalf("runMCPRemove force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "mcp", "global", "kill.json")); !os.IsNotExist(err) {
		t.Fatalf("expected file removed; stat err = %v", err)
	}
}

func TestNewMCPCmd_Metadata(t *testing.T) {
	cmd := NewMCPCmd()
	if cmd.Use != "mcp" {
		t.Errorf("Use = %q", cmd.Use)
	}
	wantSubs := map[string]bool{"list": false, "show": false, "remove": false}
	for _, c := range cmd.Commands() {
		if _, ok := wantSubs[c.Name()]; ok {
			wantSubs[c.Name()] = true
		}
	}
	for name, found := range wantSubs {
		if !found {
			t.Errorf("missing subcommand: %s", name)
		}
	}
}
