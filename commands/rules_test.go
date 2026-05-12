package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunRulesList_ListsRuleFiles(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	rulesDir := filepath.Join(agentsHome, "rules", "global")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	ruleContent := `---
description: Test rule
---
# My Rule
Some content.
`
	if err := os.WriteFile(filepath.Join(rulesDir, "test-rule.md"), []byte(ruleContent), 0644); err != nil {
		t.Fatal(err)
	}

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
	rulesDir := filepath.Join(agentsHome, "rules", "global")
	if err := os.MkdirAll(rulesDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	ruleContent := `---
description: A useful rule
---
# Rule Content
`
	if err := os.WriteFile(filepath.Join(rulesDir, "my-rule.md"), []byte(ruleContent), 0644); err != nil {
		t.Fatal(err)
	}

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
