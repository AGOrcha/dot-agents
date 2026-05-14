package commands

import (
	"os"
	"path/filepath"
	"strings"
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

// writeProposal stores a YAML proposal under AGENTS_HOME/proposals/<id>.yaml.
func writeProposal(t *testing.T, agentsHome, id, body string) {
	t.Helper()
	dir := filepath.Join(agentsHome, "proposals")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func validProposalYAML(id, status string) string {
	return `schema_version: 1
id: ` + id + `
status: ` + status + `
type: rule
action: add
target: rules/global/` + id + `.md
rationale: a meaningful rationale
content: "# canonical content"
created_at: "2025-01-01T00:00:00Z"
created_by: test
`
}

func TestRunReviewApprove_AppliesAndArchives(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	writeProposal(t, agentsHome, "apply-me", validProposalYAML("apply-me", "pending"))

	if err := runReviewApprove("apply-me"); err != nil {
		t.Fatalf("runReviewApprove: %v", err)
	}

	// Target rule file should now exist.
	target := filepath.Join(agentsHome, "rules", "global", "apply-me.md")
	if _, err := os.Stat(target); err != nil {
		t.Errorf("expected target file applied at %s: %v", target, err)
	}

	// Original proposal should have moved to archive.
	if _, err := os.Stat(filepath.Join(agentsHome, "proposals", "apply-me.yaml")); !os.IsNotExist(err) {
		t.Errorf("expected source proposal removed; stat err = %v", err)
	}
}

func TestRunReviewApprove_RejectsNonPending(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	writeProposal(t, agentsHome, "already-approved", validProposalYAML("already-approved", "approved"))

	err := runReviewApprove("already-approved")
	if err == nil {
		t.Fatal("expected error when approving a non-pending proposal")
	}
	if !strings.Contains(err.Error(), "not pending") {
		t.Errorf("expected 'not pending' in error, got %v", err)
	}
}

func TestRunReviewApprove_InvalidProposal(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	// Missing rationale -> validation fails.
	body := `schema_version: 1
id: invalid
status: pending
type: rule
action: add
target: rules/global/x.md
content: "# x"
created_at: "2025-01-01T00:00:00Z"
created_by: t
`
	writeProposal(t, agentsHome, "invalid", body)
	if err := runReviewApprove("invalid"); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRunReviewReject_MarksAndArchives(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	writeProposal(t, agentsHome, "to-reject", validProposalYAML("to-reject", "pending"))

	if err := runReviewReject("to-reject", "not ready"); err != nil {
		t.Fatalf("runReviewReject: %v", err)
	}
	if _, err := os.Stat(filepath.Join(agentsHome, "proposals", "to-reject.yaml")); !os.IsNotExist(err) {
		t.Errorf("expected source proposal removed; stat err = %v", err)
	}
}

func TestRunReviewReject_NonPending(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	writeProposal(t, agentsHome, "stale", validProposalYAML("stale", "rejected"))
	if err := runReviewReject("stale", ""); err == nil {
		t.Fatal("expected error when rejecting non-pending proposal")
	}
}

func TestRunReviewReject_NotFound(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "proposals"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := runReviewReject("ghost", "reason"); err == nil {
		t.Fatal("expected error for missing proposal")
	}
}

func TestRunReviewApprove_NotFound(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	if err := os.MkdirAll(filepath.Join(agentsHome, "proposals"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AGENTS_HOME", agentsHome)
	if err := runReviewApprove("ghost"); err == nil {
		t.Fatal("expected error for missing proposal")
	}
}

// TestRunReviewShow_InvalidYAML covers the parse error from LoadProposal that
// short-circuits before ValidateProposal — but with valid YAML missing required
// fields, the ValidateProposal error is the one we want.
func TestRunReviewShow_InvalidProposalReturnsValidationError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	// Proposal missing required `rationale` so ValidateProposal fails.
	writeProposal(t, agentsHome, "bad", `schema_version: 1
id: bad
status: pending
type: rule
action: add
target: rules/global/bad.md
content: "# bad"
created_at: "2025-01-01T00:00:00Z"
created_by: t
`)
	if err := runReviewShow("bad"); err == nil {
		t.Fatal("expected ValidateProposal error from runReviewShow")
	}
}

// TestRunReviewReject_ValidationError covers the err branch from
// ValidateProposal inside runReviewReject (line 162-164).
func TestRunReviewReject_ValidationError(t *testing.T) {
	tmp := t.TempDir()
	agentsHome := filepath.Join(tmp, ".agents")
	t.Setenv("AGENTS_HOME", agentsHome)
	writeProposal(t, agentsHome, "needs-rationale", `schema_version: 1
id: needs-rationale
status: pending
type: rule
action: add
target: rules/global/x.md
content: "# x"
created_at: "2025-01-01T00:00:00Z"
created_by: t
`)
	if err := runReviewReject("needs-rationale", "reason"); err == nil {
		t.Fatal("expected ValidateProposal error from runReviewReject")
	}
}

// TestOneLine_EmptyAfterTrim covers the len(lines)==0 guard. strings.Split
// always returns at least one element for an empty input, so this primarily
// validates the "TrimSpace gives empty string → trimmed first-line empty" path.
func TestOneLine_AllWhitespace(t *testing.T) {
	if got := oneLine("   \n\n  \t  "); got != "" {
		t.Errorf("oneLine(whitespace) = %q, want empty", got)
	}
}

// TestCaptureProposalRollback_ReadErrorPropagates: when targetPath is a
// directory, os.ReadFile returns a non-IsNotExist error, exercising the
// `return nil, err` branch (line 188-189).
func TestCaptureProposalRollback_ReadErrorPropagates(t *testing.T) {
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "iam-a-dir")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	restore, err := captureProposalRollback(dir)
	if err == nil {
		t.Errorf("expected non-IsNotExist read error, got restore=%T", restore)
	}
}

func TestCaptureProposalRollback_RestoresExistingFile(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "rule.md")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore, err := captureProposalRollback(target)
	if err != nil {
		t.Fatalf("captureProposalRollback: %v", err)
	}
	if err := os.WriteFile(target, []byte("modified"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, _ := os.ReadFile(target)
	if string(got) != "original" {
		t.Errorf("file not restored, got %q", got)
	}
}

func TestCaptureProposalRollback_RemovesIfMissingBefore(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, "sub", "rule.md")
	restore, err := captureProposalRollback(target)
	if err != nil {
		t.Fatalf("captureProposalRollback: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Errorf("expected file removed after rollback; stat err = %v", err)
	}
	// Calling restore again should be a no-op (file already gone).
	if err := restore(); err != nil {
		t.Errorf("second restore returned %v", err)
	}
}

func TestNewReviewCmd_Metadata(t *testing.T) {
	cmd := NewReviewCmd()
	if cmd.Use != "review" {
		t.Errorf("Use = %q", cmd.Use)
	}
	wantSubs := map[string]bool{"show": false, "approve": false, "reject": false}
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
	// The reject subcommand must expose a --reason flag.
	var rejectCmd interface{ Flags() interface{} }
	_ = rejectCmd
	for _, c := range cmd.Commands() {
		if c.Name() == "reject" {
			if c.Flag("reason") == nil {
				t.Error("reject subcommand should expose --reason flag")
			}
		}
	}
}
