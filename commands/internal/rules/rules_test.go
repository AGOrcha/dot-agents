package rules

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AGOrcha/dot-agents/commands/internal/cmdutil"
	"github.com/AGOrcha/dot-agents/internal/testutil"
	"github.com/spf13/cobra"
)

// CLIError mirrors the parent-package CLIError shape used for hint-aware
// errors. We redeclare it locally instead of importing the parent commands/
// package so the rules subpackage stays import-cycle-free. The real CLIError
// in commands/ has the same Hints field; tests here only assert that the
// returned error wraps a *CLIError-shaped value.
type CLIError struct {
	Message string
	Hints   []string
}

func (e *CLIError) Error() string { return e.Message }

// testDeps returns a Deps with cobra positional validators + the hint
// helpers that produce *CLIError-shaped errors. Mirrors agents/skills test
// helpers so seam tests behave identically.
func testDeps(dryRun, yes, force bool) Deps {
	return Deps{
		Flags:          GlobalFlags{DryRun: dryRun, Yes: yes, Force: force},
		ErrorWithHints: func(msg string, hints ...string) error { return &CLIError{Message: msg, Hints: hints} },
		UsageError:     func(msg string, hints ...string) error { return &CLIError{Message: msg, Hints: hints} },
		MaximumNArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return cobra.MaximumNArgs(n)
		},
		ExactArgsWithHints: func(n int, hints ...string) cobra.PositionalArgs {
			return cobra.ExactArgs(n)
		},
	}
}

func TestRunRulesList_ListsRuleFiles(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	ruleContent := `---
description: Test rule
---
# My Rule
Some content.
`
	testutil.WriteScopeFile(t, agentsHome, "rules", "global", "test-rule.md", []byte(ruleContent))

	if err := RunList(testDeps(false, false, false), "global"); err != nil {
		t.Fatalf("RunList: %v", err)
	}
}

func TestRunRulesList_EmptyScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	rulesDir := filepath.Join(agentsHome, "rules", "global")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// Empty dir — should print info message, not error.
	if err := RunList(testDeps(false, false, false), "global"); err != nil {
		t.Fatalf("RunList with empty scope: %v", err)
	}
}

func TestRunRulesList_MissingScope(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// No rules dir at all — should print info message, not error.
	if err := RunList(testDeps(false, false, false), "nonexistent"); err != nil {
		t.Fatalf("RunList with missing scope: %v", err)
	}
}

func TestRunRulesShow_ReadsRuleFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)

	ruleContent := `---
description: A useful rule
---
# Rule Content
`
	testutil.WriteScopeFile(t, agentsHome, "rules", "global", "my-rule.md", []byte(ruleContent))

	if err := RunShow(testDeps(false, false, false), "global", "my-rule.md"); err != nil {
		t.Fatalf("RunShow: %v", err)
	}
}

func TestRunRulesShow_NotFound(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	rulesDir := filepath.Join(agentsHome, "rules", "global")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	err := RunShow(testDeps(false, false, false), "global", "nonexistent")
	if err == nil {
		t.Fatal("expected error for missing rule")
	}
}

func TestExtractRuleFrontmatterDescription(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "with_description",
			content: "---\ndescription: My rule desc\n---\n# Content",
			want:    "My rule desc",
		},
		{
			name:    "no_frontmatter",
			content: "# Just content",
			want:    "",
		},
		{
			name:    "empty_description",
			content: "---\ntitle: foo\n---\n# Content",
			want:    "",
		},
		{
			name:    "crlf_frontmatter",
			content: "---\r\ndescription: CRLF desc\r\n---\r\n# body",
			want:    "CRLF desc",
		},
		{
			name:    "case_insensitive_key",
			content: "---\nDescription: Caps Key\n---\n# body",
			want:    "Caps Key",
		},
		{
			name:    "unterminated_frontmatter",
			content: "---\ndescription: never closed",
			want:    "",
		},
		{
			name:    "empty_file",
			content: "",
			want:    "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmp := t.TempDir()
			p := filepath.Join(tmp, "rule.md")
			if err := os.WriteFile(p, []byte(tc.content), 0644); err != nil {
				t.Fatal(err)
			}
			got := ExtractRuleFrontmatterDescription(p)
			if got != tc.want {
				t.Errorf("ExtractRuleFrontmatterDescription = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractRuleFrontmatterDescription_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	if got := ExtractRuleFrontmatterDescription(filepath.Join(tmp, "missing.md")); got != "" {
		t.Errorf("missing file should yield empty string, got %q", got)
	}
}

func TestRunRulesRemove_DryRun_KeepsFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "rules", "global", "keep.md", []byte("---\ndescription: keep\n---\nbody"))
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := RunRemove(testDeps(true, false, false), "global", "keep.md"); err != nil {
		t.Fatalf("RunRemove dry-run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "rules", "global", "keep.md")); err != nil {
		t.Fatalf("dry-run should preserve file: %v", err)
	}
}

func TestRunRulesRemove_Force_DeletesFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "rules", "global", "gone.md", []byte("body"))
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := RunRemove(testDeps(false, true, false), "global", "gone.md"); err != nil {
		t.Fatalf("RunRemove force: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "rules", "global", "gone.md")); !os.IsNotExist(err) {
		t.Fatalf("expected file removed; stat err = %v", err)
	}
}

func TestRunRulesRemove_NotFoundEmitsHint(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "rules", "global"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	err := RunRemove(testDeps(false, true, false), "global", "missing.md")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	var cliErr *CLIError
	if !errors.As(err, &cliErr) {
		t.Fatalf("expected *CLIError, got %T", err)
	}
	if !strings.Contains(strings.Join(cliErr.Hints, " "), "da rules list") {
		t.Errorf("expected hint pointing at `da rules list`, got %v", cliErr.Hints)
	}
}

func TestFindRuleSpec_EmptyName(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(agentsHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	if _, err := FindRuleSpec(testDeps(false, false, false), agentsHome, "global", "   "); err == nil {
		t.Fatal("expected usage error for empty name")
	}
}

func TestFindRuleSpec_Found(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "rules", "global", "alpha.md", []byte("x"))
	t.Setenv("AGENTS_HOME", agentsHome)

	spec, err := FindRuleSpec(testDeps(false, false, false), agentsHome, "global", "alpha.md")
	if err != nil {
		t.Fatalf("FindRuleSpec: %v", err)
	}
	if spec == nil || spec.BaseName != "alpha.md" {
		t.Errorf("unexpected spec: %+v", spec)
	}
}

func TestCanonicalCmdExampleBlock_JoinsLines(t *testing.T) {
	got := cmdutil.CanonicalCmdExampleBlock("a", "b", "c")
	if got != "a\nb\nc" {
		t.Errorf("cmdutil.CanonicalCmdExampleBlock = %q", got)
	}
}

// ─── maxArgs/exactArgs nil-check paths ──────────────────────────────────────

// TestMaxArgs_NilDeps exercises the nil-check guard in maxArgs. When Deps has
// a nil MaximumNArgsWithHints callback (data-layer path), maxArgs must return
// nil safely.
func TestMaxArgs_NilDeps(t *testing.T) {
	emptyDeps := Deps{} // zero-valued, no MaximumNArgsWithHints
	result := maxArgs(emptyDeps, 1, "test hint")
	if result != nil {
		t.Errorf("maxArgs with nil Deps should return nil, got %v", result)
	}
}

// TestExactArgs_NilDeps exercises the nil-check guard in exactArgs. When Deps
// has a nil ExactArgsWithHints callback (data-layer path), exactArgs must
// return nil safely.
func TestExactArgs_NilDeps(t *testing.T) {
	emptyDeps := Deps{} // zero-valued, no ExactArgsWithHints
	result := exactArgs(emptyDeps, 2, "test hint")
	if result != nil {
		t.Errorf("exactArgs with nil Deps should return nil, got %v", result)
	}
}
