package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

// TestResolveReviewOverallDecision covers the consolidation rules used by
// `workflow verify record --kind review`: derived overall must equal supplied
// overall (when supplied), and escalate requires a non-empty reason.
func TestResolveReviewOverallDecision(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		phase1      string
		phase2      string
		overallIn   string
		escalation  string
		want        string
		wantErr     bool
		errContains string
	}{
		{name: "derive_accept", phase1: "accept", phase2: "accept", want: "accept"},
		{name: "any_reject_pessimistic", phase1: "accept", phase2: "reject", want: "reject"},
		{name: "reject_takes_precedence_over_escalate", phase1: "escalate", phase2: "reject", want: "reject"},
		{name: "escalate_when_no_reject", phase1: "escalate", phase2: "accept", overallIn: "escalate", escalation: "review needed", want: "escalate"},
		{name: "supplied_overall_matches_derived", phase1: "accept", phase2: "accept", overallIn: "accept", want: "accept"},
		{name: "supplied_overall_disagrees", phase1: "accept", phase2: "accept", overallIn: "reject", wantErr: true, errContains: "disagrees"},
		{name: "escalate_without_reason_errors", phase1: "escalate", phase2: "accept", overallIn: "escalate", escalation: "", wantErr: true, errContains: "escalation-reason"},
		{name: "escalate_whitespace_reason_errors", phase1: "escalate", phase2: "accept", overallIn: "escalate", escalation: "   ", wantErr: true, errContains: "escalation-reason"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveReviewOverallDecision(tc.phase1, tc.phase2, tc.overallIn, tc.escalation)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (result=%q)", got)
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRunWorkflowVerifyRecordReview covers the full review-kind path: validates
// scope/phase decisions, writes review-decision.yaml, appends verification-log.jsonl
// with `kind=review` and status derived from the overall decision.
func TestRunWorkflowVerifyRecordReview(t *testing.T) {
	t.Run("accept_writes_decision_and_log", func(t *testing.T) {
		agentsHome := t.TempDir()
		t.Setenv("AGENTS_HOME", agentsHome)
		repo := setupTestProject(t)
		saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-task-001")

		oldwd, _ := os.Getwd()
		defer os.Chdir(oldwd)
		if err := os.Chdir(repo); err != nil {
			t.Fatal(err)
		}

		err := runWorkflowVerifyRecordReview(reviewRecordInputs{
			Command:  "self-review",
			Scope:    "repo",
			Summary:  "phase1+phase2 accept",
			Phase1In: "accept",
			Phase2In: "accept",
			TaskFlag: "task-001",
		})
		if err != nil {
			t.Fatalf("review record: %v", err)
		}

		// review-decision.yaml exists
		decisionPath := filepath.Join(repo, ".agents", "active", "verification", "task-001", "review-decision.yaml")
		if _, err := os.Stat(decisionPath); err != nil {
			t.Fatalf("expected review-decision.yaml: %v", err)
		}

		// verification log has 1 entry with kind=review, status=pass, artifact pointing to the decision
		projectName := filepath.Base(repo)
		records, err := readVerificationLog(projectName, 0)
		if err != nil {
			t.Fatalf("read log: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(records))
		}
		if records[0].Kind != "review" || records[0].Status != "pass" {
			t.Fatalf("unexpected record: %+v", records[0])
		}
		if len(records[0].Artifacts) != 1 || !strings.Contains(records[0].Artifacts[0], "review-decision.yaml") {
			t.Fatalf("expected artifact path, got %v", records[0].Artifacts)
		}
	})

	t.Run("reject_status_fail", func(t *testing.T) {
		agentsHome := t.TempDir()
		t.Setenv("AGENTS_HOME", agentsHome)
		repo := setupTestProject(t)
		saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-task-001")

		oldwd, _ := os.Getwd()
		defer os.Chdir(oldwd)
		if err := os.Chdir(repo); err != nil {
			t.Fatal(err)
		}
		err := runWorkflowVerifyRecordReview(reviewRecordInputs{
			Command:     "review",
			Scope:       "package",
			Summary:     "failed integration",
			Phase1In:    "reject",
			Phase2In:    "accept",
			FailedGates: []string{"integration"},
			TaskFlag:    "task-001",
		})
		if err != nil {
			t.Fatalf("review record: %v", err)
		}
		records, _ := readVerificationLog(filepath.Base(repo), 0)
		if len(records) != 1 || records[0].Status != "fail" {
			t.Fatalf("expected fail record, got %+v", records)
		}
	})

	t.Run("escalate_status_partial_requires_reason", func(t *testing.T) {
		agentsHome := t.TempDir()
		t.Setenv("AGENTS_HOME", agentsHome)
		repo := setupTestProject(t)
		saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-task-001")

		oldwd, _ := os.Getwd()
		defer os.Chdir(oldwd)
		if err := os.Chdir(repo); err != nil {
			t.Fatal(err)
		}
		// escalate without reason errors before mutation
		err := runWorkflowVerifyRecordReview(reviewRecordInputs{
			Command:   "review",
			Scope:     "repo",
			Summary:   "needs planning",
			Phase1In:  "escalate",
			Phase2In:  "accept",
			OverallIn: "escalate",
			TaskFlag:  "task-001",
		})
		if err == nil || !strings.Contains(err.Error(), "escalation-reason") {
			t.Fatalf("expected escalation-reason error, got %v", err)
		}

		err = runWorkflowVerifyRecordReview(reviewRecordInputs{
			Command:    "review",
			Scope:      "repo",
			Summary:    "needs planning",
			Phase1In:   "escalate",
			Phase2In:   "accept",
			OverallIn:  "escalate",
			Escalation: "spec contradiction",
			TaskFlag:   "task-001",
		})
		if err != nil {
			t.Fatalf("escalate with reason: %v", err)
		}
		records, _ := readVerificationLog(filepath.Base(repo), 0)
		if len(records) != 1 || records[0].Status != "partial" {
			t.Fatalf("expected partial status for escalate, got %+v", records)
		}
	})

	t.Run("rejects_invalid_scope", func(t *testing.T) {
		repo := setupTestProject(t)
		saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-task-001")
		oldwd, _ := os.Getwd()
		defer os.Chdir(oldwd)
		_ = os.Chdir(repo)
		err := runWorkflowVerifyRecordReview(reviewRecordInputs{
			Scope:    "module", // invalid
			Phase1In: "accept", Phase2In: "accept",
			TaskFlag: "task-001",
		})
		if err == nil || !strings.Contains(err.Error(), "invalid scope") {
			t.Fatalf("expected invalid scope error, got %v", err)
		}
	})

	t.Run("rejects_invalid_phase_decision", func(t *testing.T) {
		repo := setupTestProject(t)
		saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-task-001")
		oldwd, _ := os.Getwd()
		defer os.Chdir(oldwd)
		_ = os.Chdir(repo)
		err := runWorkflowVerifyRecordReview(reviewRecordInputs{
			Scope:    "repo",
			Phase1In: "bogus",
			Phase2In: "accept",
			TaskFlag: "task-001",
		})
		if err == nil || !strings.Contains(err.Error(), "phase1-decision") {
			t.Fatalf("expected phase1-decision parse error, got %v", err)
		}
	})

	t.Run("missing_task_with_no_contracts_errors", func(t *testing.T) {
		repo := setupTestProject(t)
		oldwd, _ := os.Getwd()
		defer os.Chdir(oldwd)
		_ = os.Chdir(repo)
		err := runWorkflowVerifyRecordReview(reviewRecordInputs{
			Scope:    "repo",
			Phase1In: "accept",
			Phase2In: "accept",
		})
		if err == nil || !strings.Contains(err.Error(), "delegation task id") {
			t.Fatalf("expected delegation contract error, got %v", err)
		}
	})
}

// TestAppendVerificationLog_AppendOnly ensures successive appends preserve
// prior records — i.e. the log is append-only.
func TestAppendVerificationLog_AppendOnly(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	project := "append-only-proj"

	first := VerificationRecord{
		SchemaVersion: 1, Timestamp: "2026-01-01T00:00:00Z",
		Kind: "test", Status: "pass", Scope: "repo", Summary: "one",
	}
	second := VerificationRecord{
		SchemaVersion: 1, Timestamp: "2026-01-02T00:00:00Z",
		Kind: "lint", Status: "fail", Scope: "package", Summary: "two",
	}
	if err := appendVerificationLog(project, first); err != nil {
		t.Fatal(err)
	}
	if err := appendVerificationLog(project, second); err != nil {
		t.Fatal(err)
	}
	records, err := readVerificationLog(project, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}
	if records[0].Summary != "one" || records[1].Summary != "two" {
		t.Fatalf("order/preservation broken: %+v", records)
	}
}

// TestReadVerificationLog_SkipsMalformedLines covers the resilience of
// readVerificationLog: malformed JSON lines must be silently skipped, not
// crash the read.
func TestReadVerificationLog_SkipsMalformedLines(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	project := "malformed-proj"

	if err := appendVerificationLog(project, VerificationRecord{
		SchemaVersion: 1, Kind: "test", Status: "pass", Scope: "repo", Summary: "valid",
	}); err != nil {
		t.Fatal(err)
	}
	// Inject a malformed line and a blank line.
	path := verificationLogPath(project)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{not json}\n\n"); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	if err := appendVerificationLog(project, VerificationRecord{
		SchemaVersion: 1, Kind: "build", Status: "pass", Scope: "repo", Summary: "valid2",
	}); err != nil {
		t.Fatal(err)
	}
	records, err := readVerificationLog(project, 0)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 valid records (malformed skipped), got %d: %+v", len(records), records)
	}
}

// TestRunWorkflowVerify_CoreBehavior verifies that appendVerificationLog +
// readVerificationLog round-trips correctly and that validation rejects
// invalid inputs.
func TestRunWorkflowVerify_CoreBehavior(t *testing.T) {
	t.Run("append_and_read_round_trip", func(t *testing.T) {
		agentsHome := t.TempDir()
		t.Setenv("AGENTS_HOME", agentsHome)

		project := "verify-test-proj"
		rec := VerificationRecord{
			SchemaVersion: 1,
			Timestamp:     "2026-05-01T10:00:00Z",
			Kind:          "test",
			Status:        "pass",
			Command:       "go test ./...",
			Scope:         "repo",
			Summary:       "all tests passed",
			Artifacts:     []string{},
			RecordedBy:    "test-harness",
		}
		if err := appendVerificationLog(project, rec); err != nil {
			t.Fatalf("appendVerificationLog: %v", err)
		}

		records, err := readVerificationLog(project, 0)
		if err != nil {
			t.Fatalf("readVerificationLog: %v", err)
		}
		if len(records) != 1 {
			t.Fatalf("expected 1 record, got %d", len(records))
		}
		if records[0].Kind != "test" || records[0].Status != "pass" {
			t.Errorf("unexpected record: %+v", records[0])
		}
		if records[0].Summary != "all tests passed" {
			t.Errorf("summary = %q, want 'all tests passed'", records[0].Summary)
		}
	})

	t.Run("read_empty_log_returns_empty_slice", func(t *testing.T) {
		agentsHome := t.TempDir()
		t.Setenv("AGENTS_HOME", agentsHome)

		records, err := readVerificationLog("nonexistent-proj", 0)
		if err != nil {
			t.Fatalf("readVerificationLog: %v", err)
		}
		if len(records) != 0 {
			t.Errorf("expected empty slice, got %d records", len(records))
		}
	})

	t.Run("read_with_limit", func(t *testing.T) {
		agentsHome := t.TempDir()
		t.Setenv("AGENTS_HOME", agentsHome)

		project := "limit-proj"
		for i := 0; i < 5; i++ {
			rec := VerificationRecord{
				SchemaVersion: 1,
				Timestamp:     "2026-05-01T10:00:00Z",
				Kind:          "test",
				Status:        "pass",
				Scope:         "repo",
				Summary:       "run",
			}
			if err := appendVerificationLog(project, rec); err != nil {
				t.Fatalf("append %d: %v", i, err)
			}
		}
		records, err := readVerificationLog(project, 3)
		if err != nil {
			t.Fatalf("readVerificationLog: %v", err)
		}
		if len(records) != 3 {
			t.Errorf("expected 3 records with limit=3, got %d", len(records))
		}
	})

	t.Run("validate_rejects_invalid_kind", func(t *testing.T) {
		err := validateVerifyRecordInputs("invalid-kind", "pass", "repo")
		if err == nil {
			t.Fatal("expected error for invalid kind, got nil")
		}
		if !strings.Contains(err.Error(), "invalid kind") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("validate_rejects_invalid_status", func(t *testing.T) {
		err := validateVerifyRecordInputs("test", "invalid-status", "repo")
		if err == nil {
			t.Fatal("expected error for invalid status, got nil")
		}
		if !strings.Contains(err.Error(), "invalid status") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("validate_rejects_invalid_scope", func(t *testing.T) {
		err := validateVerifyRecordInputs("test", "pass", "invalid-scope")
		if err == nil {
			t.Fatal("expected error for invalid scope, got nil")
		}
		if !strings.Contains(err.Error(), "invalid scope") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("validate_accepts_valid_inputs", func(t *testing.T) {
		err := validateVerifyRecordInputs("test", "pass", "repo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("validate_rejects_review_kind", func(t *testing.T) {
		err := validateVerifyRecordInputs("review", "pass", "repo")
		if err == nil {
			t.Fatal("expected error for review kind via generic path, got nil")
		}
		if !strings.Contains(err.Error(), "use runWorkflowVerifyRecordReview") {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("verification_log_path_uses_context_dir", func(t *testing.T) {
		agentsHome := t.TempDir()
		t.Setenv("AGENTS_HOME", agentsHome)

		path := verificationLogPath("my-project")
		expected := filepath.Join(config.ProjectContextDir("my-project"), "verification-log.jsonl")
		if path != expected {
			t.Errorf("path = %q, want %q", path, expected)
		}
	})

	t.Run("valid_verification_kinds", func(t *testing.T) {
		for _, k := range []string{"test", "lint", "build", "format", "custom", "review"} {
			if !isValidVerificationKind(k) {
				t.Errorf("expected %q to be valid kind", k)
			}
		}
		if isValidVerificationKind("deploy") {
			t.Error("expected 'deploy' to be invalid kind")
		}
	})

	t.Run("valid_verification_scopes", func(t *testing.T) {
		for _, s := range []string{"file", "package", "repo", "custom"} {
			if !isValidVerificationScope(s) {
				t.Errorf("expected %q to be valid scope", s)
			}
		}
		if isValidVerificationScope("module") {
			t.Error("expected 'module' to be invalid scope")
		}
	})

	t.Run("append_creates_context_dir", func(t *testing.T) {
		agentsHome := t.TempDir()
		t.Setenv("AGENTS_HOME", agentsHome)

		project := "fresh-proj"
		contextDir := config.ProjectContextDir(project)
		if _, err := os.Stat(contextDir); !os.IsNotExist(err) {
			t.Fatal("context dir should not exist before append")
		}

		rec := VerificationRecord{
			SchemaVersion: 1,
			Kind:          "build",
			Status:        "pass",
			Scope:         "repo",
		}
		if err := appendVerificationLog(project, rec); err != nil {
			t.Fatalf("appendVerificationLog: %v", err)
		}
		if _, err := os.Stat(contextDir); err != nil {
			t.Errorf("context dir should exist after append: %v", err)
		}
	})
}
