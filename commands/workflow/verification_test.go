package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/NikashPrakash/dot-agents/internal/config"
)

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
