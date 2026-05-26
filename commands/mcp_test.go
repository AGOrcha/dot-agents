package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/commands/internal/cmdutil"
)

// TestNewMCPCmd_ShimReturnsCobraTree mirrors TestNewSkillsCmd_ShimReturnsCobraTree:
// the parent-package shim must delegate to the mcp subpackage and return a
// fully assembled cobra command with the documented Use string.
func TestNewMCPCmd_ShimReturnsCobraTree(t *testing.T) {
	cmd := NewMCPCmd()
	if cmd == nil {
		t.Fatal("NewMCPCmd returned nil")
	}
	if cmd.Use != "mcp" {
		t.Errorf("Use = %q; want %q", cmd.Use, "mcp")
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

// makeMCPDeps is retained in the parent package because
// commands/coverage_test.go and commands/resource_parity_test.go still
// reference it directly. t12 relocates those tests; once that lands, t13
// drops this helper along with the mcp.go shim. Returns mcpDeps (alias
// for mcp.Deps) so subcommand builder shims accept the result unchanged.
func makeMCPDeps(dryRun, yes, force bool) mcpDeps {
	return mcpDeps{
		Flags:              cmdutil.CanonicalCmdFlags{DryRun: dryRun, Yes: yes, Force: force},
		MaxArgsWithHints:   MaximumNArgsWithHints,
		ExactArgsWithHints: ExactArgsWithHints,
		ErrorWithHints:     ErrorWithHints,
		UsageError:         UsageError,
	}
}

// writeMCPConfig is retained for commands/coverage_test.go (out of
// write_scope for this refactor). All in-file callers were migrated to
// testutil.WriteScopeFile in the subpackage; when coverage_test.go
// migrates this helper can be deleted outright.
func writeMCPConfig(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
