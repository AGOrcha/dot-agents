package workflow

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDecisionReason covers the per-outcome reason fallback chain used when
// rendering DelegationGateDecision.Reason.
func TestDecisionReason(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		doc  *ReviewDecisionDoc
		want string
	}{
		{name: "nil", doc: nil, want: ""},
		{name: "accept_with_notes", doc: &ReviewDecisionDoc{OverallDecision: "accept", ReviewerNotes: "ok"}, want: "ok"},
		{name: "accept_no_notes", doc: &ReviewDecisionDoc{OverallDecision: "accept"}, want: "review decision accepted"},
		{name: "reject_with_failed_gates_sorted", doc: &ReviewDecisionDoc{OverallDecision: "reject", FailedGates: []string{"unit", "api"}}, want: "review rejected: failed_gates=api,unit"},
		{name: "reject_with_notes_only", doc: &ReviewDecisionDoc{OverallDecision: "reject", ReviewerNotes: "see notes"}, want: "see notes"},
		{name: "reject_default", doc: &ReviewDecisionDoc{OverallDecision: "reject"}, want: "review decision rejected closeout"},
		{name: "escalate_with_reason", doc: &ReviewDecisionDoc{OverallDecision: "escalate", EscalationReason: "spec conflict"}, want: "spec conflict"},
		{name: "escalate_with_notes_only", doc: &ReviewDecisionDoc{OverallDecision: "escalate", ReviewerNotes: "needs human"}, want: "needs human"},
		{name: "escalate_default", doc: &ReviewDecisionDoc{OverallDecision: "escalate"}, want: "review escalated; planning or human review required before closeout"},
		{name: "unknown_decision", doc: &ReviewDecisionDoc{OverallDecision: "bogus"}, want: ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := decisionReason(tc.doc); got != tc.want {
				t.Fatalf("decisionReason = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEvaluateDelegationGate_RequiresMergeBack ensures the gate errors when no
// merge-back artifact exists for the task.
func TestEvaluateDelegationGate_RequiresMergeBack(t *testing.T) {
	t.Parallel()
	repo := initWorkflowTestRepo(t)
	saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")

	_, err := evaluateDelegationGate(repo, "t1", "p1")
	if err == nil {
		t.Fatal("expected error when merge-back missing")
	}
	if !strings.Contains(err.Error(), "merge-back") {
		t.Fatalf("expected merge-back hint, got %v", err)
	}
}

// TestEvaluateDelegationGate_ContractMissing returns a load error when no
// delegation contract exists.
func TestEvaluateDelegationGate_ContractMissing(t *testing.T) {
	t.Parallel()
	repo := initWorkflowTestRepo(t)
	_, err := evaluateDelegationGate(repo, "no-such", "p1")
	if err == nil {
		t.Fatal("expected error for missing contract")
	}
	if !strings.Contains(err.Error(), "delegation contract") {
		t.Fatalf("unexpected err: %v", err)
	}
}

// TestEvaluateDelegationGate_EmptyTaskID validates the precondition.
func TestEvaluateDelegationGate_EmptyTaskID(t *testing.T) {
	t.Parallel()
	repo := initWorkflowTestRepo(t)
	_, err := evaluateDelegationGate(repo, "  ", "p1")
	if err == nil || !strings.Contains(err.Error(), "task_id is required") {
		t.Fatalf("expected task_id error, got %v", err)
	}
}

// TestEvaluateDelegationGate_InvalidOverallDecision ensures a corrupted
// decision yaml with an unknown overall_decision surfaces as a validation
// error (caught by schema validation in loadReviewDecisionYAML).
func TestEvaluateDelegationGate_DecisionTaskIDMismatch(t *testing.T) {
	t.Parallel()
	repo := initWorkflowTestRepo(t)
	saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")
	writeMergeBackFixture(t, repo, "t1", "p1")

	// Write review-decision.yaml at the t1 location but with a mismatched task_id
	// inside the document (simulating drift between filename and contents).
	decisionDir := filepath.Join(repo, ".agents", "active", "verification", "t1")
	if err := os.MkdirAll(decisionDir, 0755); err != nil {
		t.Fatal(err)
	}
	yamlBody := "schema_version: 1\n" +
		"task_id: other-task\n" +
		"parent_plan_id: p1\n" +
		"phase_1_decision: accept\n" +
		"phase_2_decision: accept\n" +
		"overall_decision: accept\n" +
		"failed_gates: []\n" +
		"recorded_at: \"2026-04-19T12:00:00Z\"\n"
	if err := os.WriteFile(filepath.Join(decisionDir, "review-decision.yaml"), []byte(yamlBody), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := evaluateDelegationGate(repo, "t1", "p1")
	if err == nil || !strings.Contains(err.Error(), "task_id") {
		t.Fatalf("expected task_id mismatch error, got %v", err)
	}
}

// TestEvaluateDelegationGate_MissingReviewDecision_OutcomeReject covers the
// outcome when merge-back exists but review-decision.yaml is absent.
func TestEvaluateDelegationGate_MissingReviewDecision_OutcomeReject(t *testing.T) {
	t.Parallel()
	repo := initWorkflowTestRepo(t)
	saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")
	writeMergeBackFixture(t, repo, "t1", "p1")

	got, err := evaluateDelegationGate(repo, "t1", "p1")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Outcome != "reject" || got.CloseoutAllowed {
		t.Fatalf("missing review-decision should yield reject+no closeout: %+v", got)
	}
	if got.ReviewDecisionPresent {
		t.Fatal("expected review_decision_present=false")
	}
	if !strings.Contains(got.Reason, "review-decision.yaml") {
		t.Fatalf("expected reason mentions review-decision.yaml, got %q", got.Reason)
	}
}

// TestLoadReviewDecisionYAML_NotFound ensures the missing-file path returns
// an os.ErrNotExist-style error usable by callers (errors.Is checks).
func TestLoadReviewDecisionYAML_NotFound(t *testing.T) {
	t.Parallel()
	repo := initWorkflowTestRepo(t)
	_, err := loadReviewDecisionYAML(repo, "absent-task")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

// TestWorkflowDelegationGateCommandJSON_RejectShape asserts JSON output shape
// for a reject decision via the CLI surface, complementing the accept JSON test.
func TestWorkflowDelegationGateCommandJSON_RejectShape(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")
	writeMergeBackFixture(t, repo, "t1", "p1")
	writeReviewDecisionFixture(t, repo, "t1", &ReviewDecisionDoc{
		SchemaVersion:   1,
		TaskID:          "t1",
		ParentPlanID:    "p1",
		Phase1Decision:  "reject",
		Phase2Decision:  "accept",
		OverallDecision: "reject",
		FailedGates:     []string{"unit"},
		RecordedAt:      "2026-04-19T12:00:00Z",
	})

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	out := executeWorkflowCommandOutput(t, repo, "delegation", "gate", "--plan", "p1", "--task", "t1")
	var got DelegationGateDecision
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, out)
	}
	if got.Outcome != "reject" || got.CloseoutAllowed {
		t.Fatalf("expected reject + no closeout: %+v", got)
	}
	if !strings.Contains(got.Reason, "failed_gates=unit") {
		t.Fatalf("expected failed_gates in reason, got %q", got.Reason)
	}
}

// TestReviewDecisionYAMLPath_RequiresTaskID validates the path helper guards
// against empty task IDs.
func TestReviewDecisionYAMLPath_RequiresTaskID(t *testing.T) {
	t.Parallel()
	if _, err := reviewDecisionYAMLPath("/repo", "  "); err == nil {
		t.Fatal("expected error for blank task")
	}
	if path, err := reviewDecisionYAMLPath("/repo", "t1"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	} else if !strings.Contains(filepath.ToSlash(path), "verification/t1/review-decision.yaml") {
		t.Fatalf("path = %q", path)
	}
}

func writeReviewDecisionFixture(t *testing.T, repo, taskID string, doc *ReviewDecisionDoc) {
	t.Helper()
	if err := writeReviewDecisionYAML(repo, doc); err != nil {
		t.Fatalf("write review decision: %v", err)
	}
}

func writeMergeBackFixture(t *testing.T, repo, taskID, planID string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339)
	if err := saveMergeBack(repo, &MergeBackSummary{
		SchemaVersion:      1,
		TaskID:             taskID,
		ParentPlanID:       planID,
		Title:              "test merge-back",
		Summary:            "done",
		VerificationResult: MergeBackVerification{Status: "pass", Summary: "ok"},
		IntegrationNotes:   "ok",
		CreatedAt:          now,
	}); err != nil {
		t.Fatalf("save merge-back: %v", err)
	}
}

func TestEvaluateDelegationGateDecisions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name               string
		doc                *ReviewDecisionDoc
		wantOutcome        string
		wantCloseout       bool
		wantPlanning       bool
		wantReviewDecision bool
	}{
		{
			name: "accept",
			doc: &ReviewDecisionDoc{
				SchemaVersion:   1,
				TaskID:          "t1",
				ParentPlanID:    "p1",
				Phase1Decision:  "accept",
				Phase2Decision:  "accept",
				OverallDecision: "accept",
				FailedGates:     []string{},
				RecordedAt:      "2026-04-19T12:00:00Z",
			},
			wantOutcome:        "accept",
			wantCloseout:       true,
			wantPlanning:       false,
			wantReviewDecision: true,
		},
		{
			name: "reject",
			doc: &ReviewDecisionDoc{
				SchemaVersion:   1,
				TaskID:          "t1",
				ParentPlanID:    "p1",
				Phase1Decision:  "reject",
				Phase2Decision:  "accept",
				OverallDecision: "reject",
				FailedGates:     []string{"unit"},
				RecordedAt:      "2026-04-19T12:00:00Z",
			},
			wantOutcome:        "reject",
			wantCloseout:       false,
			wantPlanning:       false,
			wantReviewDecision: true,
		},
		{
			name: "escalate",
			doc: &ReviewDecisionDoc{
				SchemaVersion:    1,
				TaskID:           "t1",
				ParentPlanID:     "p1",
				Phase1Decision:   "escalate",
				Phase2Decision:   "accept",
				OverallDecision:  "escalate",
				FailedGates:      []string{},
				EscalationReason: "planning review required",
				RecordedAt:       "2026-04-19T12:00:00Z",
			},
			wantOutcome:        "escalate",
			wantCloseout:       false,
			wantPlanning:       true,
			wantReviewDecision: true,
		},
		{
			name:               "missing review decision",
			doc:                nil,
			wantOutcome:        "reject",
			wantCloseout:       false,
			wantPlanning:       false,
			wantReviewDecision: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			repo := initWorkflowTestRepo(t)
			saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")
			writeMergeBackFixture(t, repo, "t1", "p1")
			if tc.doc != nil {
				writeReviewDecisionFixture(t, repo, "t1", tc.doc)
			}

			got, err := evaluateDelegationGate(repo, "t1", "p1")
			if err != nil {
				t.Fatalf("evaluateDelegationGate: %v", err)
			}
			assertDelegationGateDecision(t, got, tc.wantOutcome, tc.wantCloseout, tc.wantPlanning, tc.wantReviewDecision)
		})
	}
}

func assertDelegationGateDecision(t *testing.T, got *DelegationGateDecision, wantOutcome string, wantCloseout, wantPlanning, wantReviewDecision bool) {
	t.Helper()
	if got.Outcome != wantOutcome {
		t.Fatalf("outcome = %q, want %q", got.Outcome, wantOutcome)
	}
	if got.CloseoutAllowed != wantCloseout {
		t.Fatalf("closeout_allowed = %t, want %t", got.CloseoutAllowed, wantCloseout)
	}
	if got.PlanningRequired != wantPlanning {
		t.Fatalf("planning_required = %t, want %t", got.PlanningRequired, wantPlanning)
	}
	if got.ReviewDecisionPresent != wantReviewDecision {
		t.Fatalf("review_decision_present = %t, want %t", got.ReviewDecisionPresent, wantReviewDecision)
	}
}

func TestEvaluateDelegationGatePlanMismatch(t *testing.T) {
	t.Parallel()

	repo := initWorkflowTestRepo(t)
	saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")
	writeMergeBackFixture(t, repo, "t1", "p1")

	if _, err := evaluateDelegationGate(repo, "t1", "wrong-plan"); err == nil {
		t.Fatal("expected plan mismatch error")
	}
}

func TestWorkflowDelegationGateCommandJSON(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")
	writeMergeBackFixture(t, repo, "t1", "p1")
	writeReviewDecisionFixture(t, repo, "t1", &ReviewDecisionDoc{
		SchemaVersion:   1,
		TaskID:          "t1",
		ParentPlanID:    "p1",
		Phase1Decision:  "accept",
		Phase2Decision:  "accept",
		OverallDecision: "accept",
		FailedGates:     []string{},
		RecordedAt:      "2026-04-19T12:00:00Z",
	})

	workflowTestJSON = true
	defer func() { workflowTestJSON = false }()

	out := executeWorkflowCommandOutput(t, repo, "delegation", "gate", "--plan", "p1", "--task", "t1")
	var got DelegationGateDecision
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("parse JSON: %v\n%s", err, out)
	}
	if got.Outcome != "accept" || !got.CloseoutAllowed {
		t.Fatalf("unexpected JSON gate output: %+v", got)
	}
}

func TestLoadReviewDecisionYAMLParseError(t *testing.T) {
	t.Parallel()

	repo := initWorkflowTestRepo(t)
	path := filepath.Join(repo, ".agents", "active", "verification", "t1", "review-decision.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(":\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := loadReviewDecisionYAML(repo, "t1"); err == nil {
		t.Fatal("expected parse error")
	}
}
