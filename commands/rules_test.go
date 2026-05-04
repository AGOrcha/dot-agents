package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
	"github.com/spf13/cobra"
)

func testRulesDeps() rulesDeps {
	return rulesDeps{
		errorWithHints: func(msg string, hints ...string) error {
			return fmt.Errorf("%s", msg)
		},
		usageError: func(msg string, hints ...string) error {
			return fmt.Errorf("%s", msg)
		},
		maxArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return func(*cobra.Command, []string) error { return nil }
		},
		exactArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return func(*cobra.Command, []string) error { return nil }
		},
	}
}

func TestExtractRuleFrontmatterDescription(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "r.mdc")
	content := "---\ndescription: Hello world\nglobs:\n  - \"*.go\"\n---\n# Body\n"
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if got := extractRuleFrontmatterDescription(p); got != "Hello world" {
		t.Fatalf("got %q", got)
	}
}

func TestRunRulesList(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	testutil.WriteScopeFile(t, agentsHome, "rules", scope, "rules.mdc", []byte("# x"))
	if err := runRulesList(scope); err != nil {
		t.Fatalf("runRulesList: %v", err)
	}
}

func TestRunRulesRemove(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	testutil.WriteScopeFile(t, agentsHome, "rules", scope, "drop.md", []byte("z"))
	p := filepath.Join(agentsHome, "rules", scope, "drop.md")
	deps := testRulesDeps()
	deps.Flags.Yes = true
	if err := runRulesRemove(deps, scope, "drop"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("expected file removed")
	}
}

func TestFindRuleSpec(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	scope := "proj"
	testutil.WriteScopeFile(t, agentsHome, "rules", scope, "lint.mdc", []byte("x"))
	deps := testRulesDeps()
	got, err := findRuleSpec(deps, agentsHome, scope, "lint")
	if err != nil {
		t.Fatalf("findRuleSpec: %v", err)
	}
	if got.BaseName != "lint.mdc" || got.Scope != scope {
		t.Fatalf("unexpected: %#v", got)
	}
	if _, err := findRuleSpec(deps, agentsHome, scope, "nope"); err == nil {
		t.Fatal("expected error")
	}
}
