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

func TestEvaluateDelegationGate_EmptyTaskID_Push7(t *testing.T) {
	_, err := evaluateDelegationGate(t.TempDir(), "", "")
	if err == nil || !strings.Contains(err.Error(), "task_id is required") {
		t.Fatalf("expected task_id required, got %v", err)
	}
}

func TestEvaluateDelegationGate_MissingContract(t *testing.T) {
	_, err := evaluateDelegationGate(t.TempDir(), "no-task", "")
	if err == nil || !strings.Contains(err.Error(), "load delegation contract") {
		t.Fatalf("expected load-contract error, got %v", err)
	}
}

func TestEvaluateDelegationGate_PlanIDMismatch(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-a", "plan-a", "d-a")
	_, err := evaluateDelegationGate(repo, "task-a", "plan-other")
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected plan-mismatch error, got %v", err)
	}
}

func TestEvaluateDelegationGate_MissingMergeback(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-b", "plan-b", "d-b")
	_, err := evaluateDelegationGate(repo, "task-b", "")
	if err == nil || !strings.Contains(err.Error(), "merge-back") {
		t.Fatalf("expected mergeback-required error, got %v", err)
	}
}

func TestEvaluateDelegationGate_ReviewDecisionMissing(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-c", "plan-c", "d-c")
	if err := saveMergeBack(repo, &MergeBackSummary{TaskID: "task-c", SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	out, err := evaluateDelegationGate(repo, "task-c", "")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if out.Outcome != "reject" || out.CloseoutAllowed {
		t.Fatalf("expected reject + no closeout, got %+v", out)
	}
	if !strings.Contains(out.Reason, "review-decision.yaml missing") {
		t.Fatalf("expected missing reason, got %q", out.Reason)
	}
}

func TestEvaluateDelegationGate_AcceptDecision(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-d", "plan-d", "d-d")
	if err := saveMergeBack(repo, &MergeBackSummary{TaskID: "task-d", SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	doc := newValidReviewDecisionDoc()
	doc.TaskID = "task-d"
	doc.ParentPlanID = "plan-d"
	doc.OverallDecision = "accept"
	doc.Phase1Decision = "accept"
	doc.Phase2Decision = "accept"
	if err := writeReviewDecisionYAML(repo, doc); err != nil {
		t.Fatal(err)
	}
	out, err := evaluateDelegationGate(repo, "task-d", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != "accept" || !out.CloseoutAllowed {
		t.Fatalf("expected accept + closeout, got %+v", out)
	}
}

func TestEvaluateDelegationGate_RejectDecision(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-r", "plan-r", "d-r")
	if err := saveMergeBack(repo, &MergeBackSummary{TaskID: "task-r", SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	doc := newValidReviewDecisionDoc()
	doc.TaskID = "task-r"
	doc.ParentPlanID = "plan-r"
	doc.OverallDecision = "reject"
	doc.Phase1Decision = "reject"
	doc.Phase2Decision = "reject"
	doc.FailedGates = []string{"test-coverage"}
	if err := writeReviewDecisionYAML(repo, doc); err != nil {
		t.Fatal(err)
	}
	out, err := evaluateDelegationGate(repo, "task-r", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != "reject" {
		t.Fatalf("expected reject outcome, got %s", out.Outcome)
	}
	if !strings.Contains(out.Reason, "failed_gates") {
		t.Fatalf("expected failed_gates reason, got %q", out.Reason)
	}
}

func TestEvaluateDelegationGate_EscalateDecision(t *testing.T) {
	repo := t.TempDir()
	saveTestDelegationContract(t, repo, "task-e", "plan-e", "d-e")
	if err := saveMergeBack(repo, &MergeBackSummary{TaskID: "task-e", SchemaVersion: 1}); err != nil {
		t.Fatal(err)
	}
	doc := newValidReviewDecisionDoc()
	doc.TaskID = "task-e"
	doc.ParentPlanID = "plan-e"
	doc.OverallDecision = "escalate"
	doc.Phase1Decision = "escalate"
	doc.Phase2Decision = "escalate"
	doc.EscalationReason = "needs planning review"
	if err := writeReviewDecisionYAML(repo, doc); err != nil {
		t.Fatal(err)
	}
	out, err := evaluateDelegationGate(repo, "task-e", "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Outcome != "escalate" || !out.PlanningRequired {
		t.Fatalf("expected escalate + planning_required, got %+v", out)
	}
	if !strings.Contains(out.Reason, "needs planning review") {
		t.Fatalf("expected escalation reason, got %q", out.Reason)
	}
}

func TestDecisionReason_AcceptWithReviewerNotes(t *testing.T) {
	doc := &ReviewDecisionDoc{
		OverallDecision: "accept",
		ReviewerNotes:   "looks good",
	}
	if got := decisionReason(doc); got != "looks good" {
		t.Fatalf("expected reviewer notes, got %q", got)
	}
}

func TestDecisionReason_RejectWithReviewerNotes(t *testing.T) {
	doc := &ReviewDecisionDoc{
		OverallDecision: "reject",
		ReviewerNotes:   "needs work",
	}
	if got := decisionReason(doc); got != "needs work" {
		t.Fatalf("expected reviewer notes, got %q", got)
	}
}

func TestDecisionReason_EscalateFallsBackToNotes(t *testing.T) {
	doc := &ReviewDecisionDoc{
		OverallDecision:  "escalate",
		EscalationReason: "",
		ReviewerNotes:    "ask the lead",
	}
	if got := decisionReason(doc); got != "ask the lead" {
		t.Fatalf("expected fallback notes, got %q", got)
	}
}

func TestDecisionReason_EscalateDefaultMessage(t *testing.T) {
	doc := &ReviewDecisionDoc{OverallDecision: "escalate"}
	if got := decisionReason(doc); !strings.Contains(got, "review escalated") {
		t.Fatalf("expected default escalate text, got %q", got)
	}
}

func TestDecisionReason_NilDoc(t *testing.T) {
	if got := decisionReason(nil); got != "" {
		t.Fatalf("expected empty for nil doc, got %q", got)
	}
}

func TestDecisionReason_UnknownDecision(t *testing.T) {
	doc := &ReviewDecisionDoc{OverallDecision: "weird"}
	if got := decisionReason(doc); got != "" {
		t.Fatalf("expected empty for unknown decision, got %q", got)
	}
}

func TestLoadReviewDecisionYAML_MalformedYAML(t *testing.T) {
	repo := t.TempDir()
	path, err := reviewDecisionYAMLPath(repo, "task-x")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(":\n  - bad: ["), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = loadReviewDecisionYAML(repo, "task-x")
	if err == nil || !strings.Contains(err.Error(), "parse review decision") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

// TestEvaluateDelegationGate_ReviewDecisionInvalidSchema covers loadReviewDecisionYAML's
// schema-validation error branch and evaluateDelegationGate's non-NotExist load-error
// propagation: a review-decision.yaml that parses but fails verification-decision.schema.json.
func TestEvaluateDelegationGate_ReviewDecisionInvalidSchema(t *testing.T) {
	t.Parallel()
	repo := initWorkflowTestRepo(t)
	saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")
	writeMergeBackFixture(t, repo, "t1", "p1")
	dir := filepath.Join(repo, ".agents", "active", "verification", "t1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "schema_version: 1\n" +
		"task_id: t1\n" +
		"parent_plan_id: p1\n" +
		"phase_1_decision: accept\n" +
		"phase_2_decision: accept\n" +
		"overall_decision: not-a-valid-decision\n" +
		"failed_gates: []\n" +
		"recorded_at: \"2026-04-19T12:00:00Z\"\n"
	if err := os.WriteFile(filepath.Join(dir, "review-decision.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := evaluateDelegationGate(repo, "t1", "p1"); err == nil {
		t.Fatal("expected schema-validation error to propagate")
	}
}

// TestEvaluateDelegationGate_ReviewDecisionPlanMismatch covers the review-decision
// parent_plan_id vs delegation plan mismatch branch.
func TestEvaluateDelegationGate_ReviewDecisionPlanMismatch(t *testing.T) {
	t.Parallel()
	repo := initWorkflowTestRepo(t)
	saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")
	writeMergeBackFixture(t, repo, "t1", "p1")
	writeReviewDecisionFixture(t, repo, "t1", &ReviewDecisionDoc{
		SchemaVersion:   1,
		TaskID:          "t1",
		ParentPlanID:    "wrong-plan",
		Phase1Decision:  "accept",
		Phase2Decision:  "accept",
		OverallDecision: "accept",
		FailedGates:     []string{},
		RecordedAt:      "2026-04-19T12:00:00Z",
	})
	if _, err := evaluateDelegationGate(repo, "t1", "p1"); err == nil || !strings.Contains(err.Error(), "plan") {
		t.Fatalf("expected plan_id mismatch error, got %v", err)
	}
}

// TestWorkflowDelegationGateCommand_EvaluateError covers the cobra wrapper's
// error-return path when evaluateDelegationGate fails (no merge-back present).
func TestWorkflowDelegationGateCommand_EvaluateError(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	saveTestDelegationContract(t, repo, "t1", "p1", "del-t1")
	if err := executeWorkflowCommand(t, repo, "delegation", "gate", "--plan", "p1", "--task", "t1"); err == nil {
		t.Fatal("expected delegation gate command to return the evaluate error")
	}
}
