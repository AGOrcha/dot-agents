package commands

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunReviewList_EmptyDir(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	proposalsDir := filepath.Join(agentsHome, "proposals")
	if err := os.MkdirAll(proposalsDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	// Empty proposals dir — should not error.
	if err := runReviewList(); err != nil {
		t.Fatalf("runReviewList with empty dir: %v", err)
	}
}

func TestRunReviewList_NoDirAtAll(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	// Don't create proposals dir at all.
	if err := os.MkdirAll(agentsHome, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	if err := runReviewList(); err != nil {
		t.Fatalf("runReviewList with missing dir: %v", err)
	}
}

func TestRunReviewShow_ValidProposal(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	proposalsDir := filepath.Join(agentsHome, "proposals")
	if err := os.MkdirAll(proposalsDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	proposalYAML := `schema_version: 1
id: test-prop
status: pending
type: rule
action: add
target: rules/global/test.md
rationale: test rationale
content: "# test rule"
created_at: "2025-01-01T00:00:00Z"
created_by: test
`
	if err := os.WriteFile(filepath.Join(proposalsDir, "test-prop.yaml"), []byte(proposalYAML), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runReviewShow("test-prop"); err != nil {
		t.Fatalf("runReviewShow: %v", err)
	}
}

func TestRunReviewShow_NotFound(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	proposalsDir := filepath.Join(agentsHome, "proposals")
	if err := os.MkdirAll(proposalsDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	err := runReviewShow("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing proposal")
	}
}

func TestRunReviewList_WithPendingProposal(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	proposalsDir := filepath.Join(agentsHome, "proposals")
	if err := os.MkdirAll(proposalsDir, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)

	proposalYAML := `schema_version: 1
id: my-pending
status: pending
type: skill
action: add
target: skills/global/test
rationale: adding test skill
content: "# skill"
created_at: "2025-01-01T00:00:00Z"
created_by: tester
`
	if err := os.WriteFile(filepath.Join(proposalsDir, "my-pending.yaml"), []byte(proposalYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Should list the pending proposal without error.
	if err := runReviewList(); err != nil {
		t.Fatalf("runReviewList with pending proposal: %v", err)
	}
}

func TestOneLine(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"hello", "hello"},
		{"first\nsecond", "first"},
		{"  padded  \nmore", "padded"},
	}
	for _, tc := range tests {
		got := oneLine(tc.in)
		if got != tc.want {
			t.Errorf("oneLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
