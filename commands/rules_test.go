package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/testutil"
)

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

	if err := runRulesList("global"); err != nil {
		t.Fatalf("runRulesList: %v", err)
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
	if err := runRulesList("global"); err != nil {
		t.Fatalf("runRulesList with empty scope: %v", err)
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
	if err := runRulesList("nonexistent"); err != nil {
		t.Fatalf("runRulesList with missing scope: %v", err)
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

	deps := rulesDeps{
		errorWithHints:     ErrorWithHints,
		usageError:         UsageError,
		maxArgsWithHints:   MaximumNArgsWithHints,
		exactArgsWithHints: ExactArgsWithHints,
	}

	if err := runRulesShow(deps, "global", "my-rule.md"); err != nil {
		t.Fatalf("runRulesShow: %v", err)
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

	deps := rulesDeps{
		errorWithHints:     ErrorWithHints,
		usageError:         UsageError,
		maxArgsWithHints:   MaximumNArgsWithHints,
		exactArgsWithHints: ExactArgsWithHints,
	}

	err := runRulesShow(deps, "global", "nonexistent")
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
			got := extractRuleFrontmatterDescription(p)
			if got != tc.want {
				t.Errorf("extractRuleFrontmatterDescription = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExtractRuleFrontmatterDescription_MissingFile(t *testing.T) {
	tmp := t.TempDir()
	if got := extractRuleFrontmatterDescription(filepath.Join(tmp, "missing.md")); got != "" {
		t.Errorf("missing file should yield empty string, got %q", got)
	}
}

func makeRulesDeps(dryRun, yes, force bool) rulesDeps {
	return rulesDeps{
		Flags:              canonicalCmdFlags{DryRun: dryRun, Yes: yes, Force: force},
		errorWithHints:     ErrorWithHints,
		usageError:         UsageError,
		maxArgsWithHints:   MaximumNArgsWithHints,
		exactArgsWithHints: ExactArgsWithHints,
	}
}

func TestRunRulesRemove_DryRun_KeepsFile(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "rules", "global", "keep.md", []byte("---\ndescription: keep\n---\nbody"))
	t.Setenv("AGENTS_HOME", agentsHome)

	deps := makeRulesDeps(true, false, false)
	if err := runRulesRemove(deps, "global", "keep.md"); err != nil {
		t.Fatalf("runRulesRemove dry-run: %v", err)
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

	deps := makeRulesDeps(false, true, false)
	if err := runRulesRemove(deps, "global", "gone.md"); err != nil {
		t.Fatalf("runRulesRemove force: %v", err)
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

	deps := makeRulesDeps(false, true, false)
	err := runRulesRemove(deps, "global", "missing.md")
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

	deps := makeRulesDeps(false, false, false)
	if _, err := findRuleSpec(deps, agentsHome, "global", "   "); err == nil {
		t.Fatal("expected usage error for empty name")
	}
}

func TestFindRuleSpec_Found(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	testutil.WriteScopeFile(t, agentsHome, "rules", "global", "alpha.md", []byte("x"))
	t.Setenv("AGENTS_HOME", agentsHome)

	deps := makeRulesDeps(false, false, false)
	spec, err := findRuleSpec(deps, agentsHome, "global", "alpha.md")
	if err != nil {
		t.Fatalf("findRuleSpec: %v", err)
	}
	if spec == nil || spec.BaseName != "alpha.md" {
		t.Errorf("unexpected spec: %+v", spec)
	}
}

func TestNewRulesCmd_Metadata(t *testing.T) {
	cmd := NewRulesCmd()
	if cmd.Use != "rules" {
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

func TestCanonicalCmdExampleBlock_JoinsLines(t *testing.T) {
	got := canonicalCmdExampleBlock("a", "b", "c")
	if got != "a\nb\nc" {
		t.Errorf("canonicalCmdExampleBlock = %q", got)
	}
}
