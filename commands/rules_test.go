package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

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
	dir := filepath.Join(agentsHome, "rules", scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rules.mdc"), []byte("# x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := runRulesList(scope); err != nil {
		t.Fatalf("runRulesList: %v", err)
	}
}

func TestRunRulesRemove(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	scope := "g"
	dir := filepath.Join(agentsHome, "rules", scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "drop.md")
	if err := os.WriteFile(p, []byte("z"), 0644); err != nil {
		t.Fatal(err)
	}
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
	dir := filepath.Join(agentsHome, "rules", scope)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lint.mdc"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
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
