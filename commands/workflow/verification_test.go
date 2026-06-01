package workflow

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
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
			assertReviewOverallDecision(t, got, err, tc.wantErr, tc.errContains, tc.want)
		})
	}
}

// assertReviewOverallDecision validates one resolveReviewOverallDecision
// outcome against the expected error/value for a test case.
func assertReviewOverallDecision(t *testing.T, got string, err error, wantErr bool, errContains, want string) {
	t.Helper()
	if wantErr {
		if err == nil {
			t.Fatalf("expected error, got nil (result=%q)", got)
		}
		if errContains != "" && !strings.Contains(err.Error(), errContains) {
			t.Fatalf("error %q does not contain %q", err.Error(), errContains)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// TestRunWorkflowVerifyRecordReview covers the full review-kind path: validates
// scope/phase decisions, writes review-decision.yaml, appends verification-log.jsonl
// with `kind=review` and status derived from the overall decision.
// reviewTestRepoWithContract sets AGENTS_HOME, builds a test project with a
// saved delegation contract for task-001, and chdirs into the repo (restored
// on cleanup). Returns the repo path.
func reviewTestRepoWithContract(t *testing.T) string {
	t.Helper()
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-task-001")
	chdirForTest(t, repo)
	return repo
}

// chdirForTest changes into dir and restores the prior working directory on
// test cleanup.
func chdirForTest(t *testing.T, dir string) {
	t.Helper()
	oldwd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldwd) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

func TestRunWorkflowVerifyRecordReview(t *testing.T) {
	t.Run("accept_writes_decision_and_log", testReviewAcceptWritesDecisionAndLog)
	t.Run("reject_status_fail", testReviewRejectStatusFail)
	t.Run("escalate_status_partial_requires_reason", testReviewEscalateRequiresReason)
	t.Run("rejects_invalid_scope", testReviewRejectsInvalidScope)
	t.Run("rejects_invalid_phase_decision", testReviewRejectsInvalidPhaseDecision)
	t.Run("missing_task_with_no_contracts_errors", testReviewMissingTaskErrors)
}

func testReviewAcceptWritesDecisionAndLog(t *testing.T) {
	repo := reviewTestRepoWithContract(t)

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
}

func testReviewRejectStatusFail(t *testing.T) {
	repo := reviewTestRepoWithContract(t)
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
}

func testReviewEscalateRequiresReason(t *testing.T) {
	repo := reviewTestRepoWithContract(t)
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
}

func testReviewRejectsInvalidScope(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-task-001")
	chdirForTest(t, repo)
	err := runWorkflowVerifyRecordReview(reviewRecordInputs{
		Scope:    "module", // invalid
		Phase1In: "accept", Phase2In: "accept",
		TaskFlag: "task-001",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid scope") {
		t.Fatalf("expected invalid scope error, got %v", err)
	}
}

func testReviewRejectsInvalidPhaseDecision(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "del-task-001")
	chdirForTest(t, repo)
	err := runWorkflowVerifyRecordReview(reviewRecordInputs{
		Scope:    "repo",
		Phase1In: "bogus",
		Phase2In: "accept",
		TaskFlag: "task-001",
	})
	if err == nil || !strings.Contains(err.Error(), "phase1-decision") {
		t.Fatalf("expected phase1-decision parse error, got %v", err)
	}
}

func testReviewMissingTaskErrors(t *testing.T) {
	repo := setupTestProject(t)
	chdirForTest(t, repo)
	err := runWorkflowVerifyRecordReview(reviewRecordInputs{
		Scope:    "repo",
		Phase1In: "accept",
		Phase2In: "accept",
	})
	if err == nil || !strings.Contains(err.Error(), "delegation task id") {
		t.Fatalf("expected delegation contract error, got %v", err)
	}
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
	t.Run("append_and_read_round_trip", testVerifyAppendReadRoundTrip)
	t.Run("read_empty_log_returns_empty_slice", testVerifyReadEmptyLog)
	t.Run("read_with_limit", testVerifyReadWithLimit)
	t.Run("validate_rejects_invalid_kind", testVerifyRejectsInvalidKind)
	t.Run("validate_rejects_invalid_status", testVerifyRejectsInvalidStatus)
	t.Run("validate_rejects_invalid_scope", testVerifyRejectsInvalidScope)
	t.Run("validate_accepts_valid_inputs", testVerifyAcceptsValidInputs)
	t.Run("validate_rejects_review_kind", testVerifyRejectsReviewKind)
	t.Run("verification_log_path_uses_context_dir", testVerifyLogPathUsesContextDir)
	t.Run("valid_verification_kinds", testVerifyValidKinds)
	t.Run("valid_verification_scopes", testVerifyValidScopes)
	t.Run("append_creates_context_dir", testVerifyAppendCreatesContextDir)
}

func testVerifyAppendReadRoundTrip(t *testing.T) {
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
}

func testVerifyReadEmptyLog(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	records, err := readVerificationLog("nonexistent-proj", 0)
	if err != nil {
		t.Fatalf("readVerificationLog: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected empty slice, got %d records", len(records))
	}
}

func testVerifyReadWithLimit(t *testing.T) {
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
}

func testVerifyRejectsInvalidKind(t *testing.T) {
	err := validateVerifyRecordInputs("invalid-kind", "pass", "repo")
	if err == nil {
		t.Fatal("expected error for invalid kind, got nil")
	}
	if !strings.Contains(err.Error(), "invalid kind") {
		t.Errorf("unexpected error: %v", err)
	}
}

func testVerifyRejectsInvalidStatus(t *testing.T) {
	err := validateVerifyRecordInputs("test", "invalid-status", "repo")
	if err == nil {
		t.Fatal("expected error for invalid status, got nil")
	}
	if !strings.Contains(err.Error(), "invalid status") {
		t.Errorf("unexpected error: %v", err)
	}
}

func testVerifyRejectsInvalidScope(t *testing.T) {
	err := validateVerifyRecordInputs("test", "pass", "invalid-scope")
	if err == nil {
		t.Fatal("expected error for invalid scope, got nil")
	}
	if !strings.Contains(err.Error(), "invalid scope") {
		t.Errorf("unexpected error: %v", err)
	}
}

func testVerifyAcceptsValidInputs(t *testing.T) {
	err := validateVerifyRecordInputs("test", "pass", "repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func testVerifyRejectsReviewKind(t *testing.T) {
	err := validateVerifyRecordInputs("review", "pass", "repo")
	if err == nil {
		t.Fatal("expected error for review kind via generic path, got nil")
	}
	if !strings.Contains(err.Error(), "use runWorkflowVerifyRecordReview") {
		t.Errorf("unexpected error: %v", err)
	}
}

func testVerifyLogPathUsesContextDir(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	path := verificationLogPath("my-project")
	expected := filepath.Join(config.ProjectContextDir("my-project"), "verification-log.jsonl")
	if path != expected {
		t.Errorf("path = %q, want %q", path, expected)
	}
}

func testVerifyValidKinds(t *testing.T) {
	for _, k := range []string{"test", "lint", "build", "format", "custom", "review"} {
		if !isValidVerificationKind(k) {
			t.Errorf("expected %q to be valid kind", k)
		}
	}
	if isValidVerificationKind("deploy") {
		t.Error("expected 'deploy' to be invalid kind")
	}
}

func testVerifyValidScopes(t *testing.T) {
	for _, s := range []string{"file", "package", "repo", "custom"} {
		if !isValidVerificationScope(s) {
			t.Errorf("expected %q to be valid scope", s)
		}
	}
	if isValidVerificationScope("module") {
		t.Error("expected 'module' to be invalid scope")
	}
}

func testVerifyAppendCreatesContextDir(t *testing.T) {
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
}

func TestIsValidVerificationKind(t *testing.T) {
	for _, k := range []string{"test", "lint", "build", "format", "custom", "review", "TEST", " custom "} {
		if !isValidVerificationKind(k) {
			t.Errorf("expected %q valid", k)
		}
	}
	if isValidVerificationKind("bogus") {
		t.Error("bogus should not be valid")
	}
}

func TestIsValidVerificationScope(t *testing.T) {
	for _, s := range []string{"file", "package", "repo", "custom"} {
		if !isValidVerificationScope(s) {
			t.Errorf("expected %q valid", s)
		}
	}
	if isValidVerificationScope("global") {
		t.Error("global should not be valid")
	}
}

func TestVerificationLogPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := verificationLogPath("p")
	if !strings.HasSuffix(p, "verification-log.jsonl") {
		t.Errorf("path = %s", p)
	}
}

func TestAppendAndReadVerificationLog(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	for i := 0; i < 3; i++ {
		rec := VerificationRecord{
			SchemaVersion: 1, Timestamp: time.Now().UTC().Format(time.RFC3339),
			Kind: "test", Status: "pass", Scope: "repo",
			Summary: "tests pass",
		}
		if err := appendVerificationLog("proj", rec); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := readVerificationLog("proj", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Errorf("expected 3 records, got %d", len(recs))
	}

	recs, _ = readVerificationLog("proj", 2)
	if len(recs) != 2 {
		t.Errorf("expected 2 with limit, got %d", len(recs))
	}

	recs, err = readVerificationLog("ghost", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Errorf("missing log returns 0, got %d", len(recs))
	}
}

func TestReadVerificationLog_SkipsMalformed(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	rec := VerificationRecord{SchemaVersion: 1, Kind: "test", Status: "pass", Summary: "x"}
	if err := appendVerificationLog("p2", rec); err != nil {
		t.Fatal(err)
	}

	path := verificationLogPath("p2")
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	_, _ = f.WriteString("not json\n")
	_ = f.Close()
	recs, _ := readVerificationLog("p2", 0)
	if len(recs) != 1 {
		t.Errorf("malformed line should be skipped, got %d records", len(recs))
	}
}

func TestRunWorkflowVerifyLog_Empty(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowVerifyLog(false) })
	if err != nil {
		t.Fatalf("runWorkflowVerifyLog: %v", err)
	}
	if !strings.Contains(out, "No verification records") {
		t.Errorf("expected empty marker, got: %s", out)
	}
}

func TestRunWorkflowVerifyLog_WithRecords(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	chdirForCov(t, repo)
	for _, status := range []string{"pass", "fail", "partial", "unknown"} {
		rec := VerificationRecord{
			SchemaVersion: 1, Timestamp: time.Now().UTC().Format(time.RFC3339),
			Kind: "test", Status: status, Scope: "repo",
			Summary: "summary-" + status, Command: "go test",
		}
		if err := appendVerificationLog("workflow-proj", rec); err != nil {
			t.Fatal(err)
		}
	}
	out, err := captureCovStdout(t, func() error { return runWorkflowVerifyLog(true) })
	if err != nil {
		t.Fatalf("runWorkflowVerifyLog: %v", err)
	}
	for _, want := range []string{"Verification Log", "summary-pass", "summary-fail", "summary-partial", "summary-unknown", "cmd: go test"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in output, got: %s", want, out)
		}
	}
}

func TestRunWorkflowVerifyLog_JSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("AGENTS_HOME", tmp)
	chdirForCov(t, repo)
	rec := VerificationRecord{
		SchemaVersion: 1, Timestamp: time.Now().UTC().Format(time.RFC3339),
		Kind: "test", Status: "pass", Scope: "repo", Summary: "ok",
	}
	if err := appendVerificationLog("workflow-proj", rec); err != nil {
		t.Fatal(err)
	}
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowVerifyLog(false) })
	if err != nil {
		t.Fatalf("runWorkflowVerifyLog: %v", err)
	}
	if !strings.Contains(out, "\"kind\": \"test\"") {
		t.Errorf("expected JSON output, got: %s", out)
	}
}

func TestRunWorkflowVerifyLog_JSON_Push5(t *testing.T) {
	repo := setupTestProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	rec := VerificationRecord{
		SchemaVersion: 1, Kind: "test", Status: "pass",
		Timestamp: "2026-05-12T00:00:00Z", Summary: "ok",
	}
	if err := appendVerificationLog(filepath.Base(repo), rec); err != nil {
		t.Fatal(err)
	}
	chdirForCov(t, repo)
	workflowTestJSON = true
	t.Cleanup(func() { workflowTestJSON = false })
	out, err := captureCovStdout(t, func() error { return runWorkflowVerifyLog(false) })
	if err != nil {
		t.Fatalf("verify log json: %v", err)
	}
	if !strings.Contains(out, `"kind"`) {
		t.Fatalf("expected JSON output, got: %s", out)
	}
}

func TestRunWorkflowVerifyLog_Empty_Push5(t *testing.T) {
	repo := setupTestProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowVerifyLog(true) })
	if err != nil {
		t.Fatalf("verify log: %v", err)
	}
	if !strings.Contains(out, "No verification records") {
		t.Fatalf("expected empty-log message, got: %s", out)
	}
}

func TestRunWorkflowVerifyLog_RenderAllIcons(t *testing.T) {
	repo := setupTestProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	for _, s := range []string{"pass", "fail", "partial", "unknown"} {
		rec := VerificationRecord{
			SchemaVersion: 1, Kind: "test", Status: s,
			Timestamp: "2026-05-12T00:00:00Z", Summary: "row-" + s,
			Command: "go test",
		}
		if err := appendVerificationLog(filepath.Base(repo), rec); err != nil {
			t.Fatal(err)
		}
	}
	chdirForCov(t, repo)
	out, err := captureCovStdout(t, func() error { return runWorkflowVerifyLog(true) })
	if err != nil {
		t.Fatalf("verify log: %v", err)
	}
	for _, marker := range []string{"row-fail", "row-partial", "row-unknown", "go test"} {
		if !strings.Contains(out, marker) {
			t.Fatalf("missing %q in output: %s", marker, out)
		}
	}
}

func TestRunWorkflowVerifyLog_ReadError(t *testing.T) {
	repo := setupTestProject(t)
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)

	ctx := filepath.Join(agentsHome, "context", filepath.Base(repo))
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(ctx, "verification-log.jsonl")
	if err := os.WriteFile(logPath, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, logPath)
	chdirForCov(t, repo)
	if err := runWorkflowVerifyLog(false); err == nil {
		t.Fatal("expected read error to propagate")
	}
}

func TestValidateVerifyRecordInputs_ReviewKindRejected(t *testing.T) {
	err := validateVerifyRecordInputs("review", "pass", "package")
	if err == nil || !strings.Contains(err.Error(), "use runWorkflowVerifyRecordReview") {
		t.Fatalf("expected internal-use-review-fn error, got %v", err)
	}
}

func TestResolveReviewOverallDecision_OverallDisagreesWithDerived(t *testing.T) {
	_, err := resolveReviewOverallDecision("accept", "accept", "reject", "")
	if err == nil || !strings.Contains(err.Error(), "disagrees with phases") {
		t.Fatalf("expected mismatch error, got %v", err)
	}
}

func TestResolveReviewOverallDecision_EscalateWithoutReason(t *testing.T) {
	_, err := resolveReviewOverallDecision("escalate", "accept", "escalate", "")
	if err == nil || !strings.Contains(err.Error(), "escalation-reason is empty") {
		t.Fatalf("expected escalation-empty error, got %v", err)
	}
}

func TestResolveReviewOverallDecision_DerivedFromEmpty(t *testing.T) {
	out, err := resolveReviewOverallDecision("accept", "accept", "", "")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if out != "accept" {
		t.Fatalf("expected accept derived, got %q", out)
	}
}

func TestResolveReviewDelegationContract_NoContractsHint(t *testing.T) {
	repo := t.TempDir()
	_, _, err := resolveReviewDelegationContract(repo, "")
	if err == nil || !strings.Contains(err.Error(), "needs a delegation task id") {
		t.Fatalf("expected hint error, got %v", err)
	}
}

func TestWriteVerifyResultArtifact_InvalidStem(t *testing.T) {
	_, err := writeVerifyResultArtifact(verifyResultArtifactInputs{
		ProjectPath: t.TempDir(), TaskID: "t1", Kind: "test",
		VerifierType: "BAD-STEM!", Now: "2026-05-12T00:00:00Z",
	})
	if err == nil || !strings.Contains(err.Error(), "not a valid artifact stem") {
		t.Fatalf("expected invalid-stem error, got %v", err)
	}
}

func TestWriteVerifyResultArtifact_ContractMissing(t *testing.T) {
	repo := t.TempDir()
	_, err := writeVerifyResultArtifact(verifyResultArtifactInputs{
		ProjectPath: repo, TaskID: "no-such-task", Kind: "test",
		VerifierType: "unit", Now: "2026-05-12T00:00:00Z",
	})
	if err == nil || !strings.Contains(err.Error(), "load delegation contract") {
		t.Fatalf("expected load-contract error, got %v", err)
	}
}

func TestWriteVerifyResultArtifact_WriteYAMLErr(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-x", "plan-x", "deleg-x")

	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))

	_, err := writeVerifyResultArtifact(verifyResultArtifactInputs{
		ProjectPath: repo, TaskID: "task-x", Kind: "test", Status: "pass",
		Summary: "ok", VerifierType: "unit", Now: "2026-05-12T00:00:00Z",
	})
	if err == nil {
		t.Fatal("expected error from yaml stub")
	}

	if !strings.Contains(err.Error(), sentinel.Error()) {
		t.Fatalf("expected sentinel %q in error, got %v", sentinel, err)
	}
}

func TestRunWorkflowVerifyRecord_WriteArtifactErrPropagates(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	err := runWorkflowVerifyRecord(verifyRecordInputs{
		Kind: "test", Status: "pass", Scope: "package",
		Summary: "x", TaskID: "task-001",
	})
	if err == nil || !strings.Contains(err.Error(), "load delegation contract") {
		t.Fatalf("expected propagated load-contract error, got %v", err)
	}
}

func TestRunWorkflowVerifyRecord_AppendLogErr(t *testing.T) {
	repo := setupTestProject(t)
	chdirForCov(t, repo)
	sentinel := errors.New("mkdir boom")
	withMkdirAllStub(t, func(string, os.FileMode) error { return sentinel })
	err := runWorkflowVerifyRecord(verifyRecordInputs{
		Kind: "test", Status: "pass", Scope: "package", Summary: "x",
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected mkdir sentinel from append log, got %v", err)
	}
}

func TestRunWorkflowVerifyRecordReview_WriteYAMLErr(t *testing.T) {
	repo := setupTestProject(t)
	saveTestDelegationContract(t, repo, "task-001", "plan-001", "deleg-r")
	chdirForCov(t, repo)
	sentinel := errors.New("yaml boom")
	withYAMLMarshalStub(t, yamlMarshalErrStub(sentinel))
	err := runWorkflowVerifyRecordReview(reviewRecordInputs{
		Command: "review", Scope: "repo", Summary: "ok",
		Phase1In: "accept", Phase2In: "accept", OverallIn: "accept",
		TaskFlag: "task-001",
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected yaml sentinel, got %v", err)
	}
}

func TestRunWorkflowVerifyRecordReview_InvalidScope(t *testing.T) {
	err := runWorkflowVerifyRecordReview(reviewRecordInputs{Scope: "BAD"})
	if err == nil || !strings.Contains(err.Error(), "invalid scope") {
		t.Fatalf("expected invalid-scope error, got %v", err)
	}
}

func TestIsValidVerificationScope_Invalid(t *testing.T) {
	if isValidVerificationScope("garbage") {
		t.Fatal("expected false for unknown scope")
	}
}

func TestIsValidVerificationKind_Invalid(t *testing.T) {
	if isValidVerificationKind("garbage") {
		t.Fatal("expected false for unknown kind")
	}
}

func TestResolveReviewDelegationContract_BadTaskID(t *testing.T) {
	repo := t.TempDir()
	_, _, err := resolveReviewDelegationContract(repo, "no-such")
	if err == nil || !strings.Contains(err.Error(), "load delegation contract") {
		t.Fatalf("expected load-contract error, got %v", err)
	}
}

func TestReadVerificationLog_SkipsMalformedLine(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	rec := `{"schema_version":1,"kind":"test","status":"pass","summary":"a","timestamp":"2026-05-12T00:00:00Z"}` + "\n" +
		`not-json` + "\n"
	if err := os.WriteFile(filepath.Join(ctx, "verification-log.jsonl"), []byte(rec), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := readVerificationLog("p", 0)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 record (malformed skipped), got %d", len(out))
	}
}

func TestReadVerificationLog_LimitTrim(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	for i := 0; i < 5; i++ {
		buf.WriteString(fmt.Sprintf(`{"schema_version":1,"kind":"test","status":"pass","summary":"r%d","timestamp":"2026-05-12T00:00:0%dZ"}`+"\n", i, i))
	}
	if err := os.WriteFile(filepath.Join(ctx, "verification-log.jsonl"), []byte(buf.String()), 0644); err != nil {
		t.Fatal(err)
	}
	out, err := readVerificationLog("p", 2)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 records after limit, got %d", len(out))
	}
}

func TestReadVerificationLog_ReadError(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	ctx := filepath.Join(agentsHome, "context", "p")
	if err := os.MkdirAll(ctx, 0755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(ctx, "verification-log.jsonl")
	if err := os.WriteFile(logPath, []byte("{}\n"), 0644); err != nil {
		t.Fatal(err)
	}
	chmodUnreadable(t, logPath)
	if _, err := readVerificationLog("p", 0); err == nil {
		t.Fatal("expected read error to propagate")
	}
}

func TestAppendVerificationLog_RoundTrip(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	rec := VerificationRecord{
		SchemaVersion: 1, Kind: "test", Status: "pass",
		Timestamp: "2026-05-12T00:00:00Z", Summary: "ok",
	}
	if err := appendVerificationLog("p", rec); err != nil {
		t.Fatal(err)
	}
	out, err := readVerificationLog("p", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Summary != "ok" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}

func TestWriteVerifyResultArtifact_HappyPath(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-h", "plan-h", "d-h")
	rel, err := writeVerifyResultArtifact(verifyResultArtifactInputs{
		ProjectPath: repo, TaskID: "task-h", Kind: "test", Status: "pass",
		Summary: "ok", Command: "go test", VerifierType: "unit",
		Now: "2026-05-12T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(rel, "unit.result.yaml") {
		t.Fatalf("unexpected rel path: %s", rel)
	}
}

func TestAppendVerificationLog_Multiline(t *testing.T) {
	agentsHome := t.TempDir()
	t.Setenv("AGENTS_HOME", agentsHome)
	for i := 0; i < 3; i++ {
		rec := VerificationRecord{
			SchemaVersion: 1, Kind: "test", Status: "pass",
			Timestamp: fmt.Sprintf("2026-05-12T00:00:0%dZ", i),
			Summary:   fmt.Sprintf("row-%d", i),
		}
		if err := appendVerificationLog("p", rec); err != nil {
			t.Fatal(err)
		}
	}
	out, err := readVerificationLog("p", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 {
		t.Fatalf("expected 3 records, got %d", len(out))
	}
}

func TestRunWorkflowVerifyRecordReview_StatusForbidden(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	err := executeWorkflowCommand(t, repo,
		"verify", "record",
		"--kind", "review", "--status", "pass",
		"--task", "t1", "--summary", "review approved",
	)
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected status-forbidden error, got %v", err)
	}
}

func TestVerifyRecord_MissingSummaryRejected(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo,
		"verify", "record",
		"--kind", "test",
		"--status", "pass",
	)
	if err == nil {
		t.Fatal("expected error for missing summary")
	}
}

func TestVerifyRecord_HappyPath_NoTask(t *testing.T) {
	repo := setupTestProject(t)
	if err := executeWorkflowCommand(t, repo,
		"verify", "record",
		"--kind", "test", "--status", "pass",
		"--summary", "all tests passed",
	); err != nil {
		t.Fatalf("verify record: %v", err)
	}
}

func TestVerifyRecordReview_PhaseDecisionRecorded(t *testing.T) {
	repo := setupFanoutSliceProject(t, "in_progress")
	if err := executeWorkflowCommand(t, repo, "fanout", "--plan", "p1", "--slice", "s1", "--owner", "w"); err != nil {
		t.Fatal(err)
	}
	if err := executeWorkflowCommand(t, repo,
		"verify", "record",
		"--kind", "review",
		"--task", "t1",
		"--phase1-decision", "accept",
		"--phase2-decision", "accept",
		"--summary", "LGTM",
	); err != nil {
		t.Fatalf("verify record review: %v", err)
	}
}

func TestVerifyRecord_InvalidStatusRejected(t *testing.T) {
	repo := setupTestProject(t)
	err := executeWorkflowCommand(t, repo,
		"verify", "record",
		"--kind", "test",
		"--status", "bogus-status",
		"--summary", "x",
	)
	if err == nil || !strings.Contains(err.Error(), "status") {
		t.Fatalf("expected invalid status error, got %v", err)
	}
}
