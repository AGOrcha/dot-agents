package workflow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
			if got.Outcome != tc.wantOutcome {
				t.Fatalf("outcome = %q, want %q", got.Outcome, tc.wantOutcome)
			}
			if got.CloseoutAllowed != tc.wantCloseout {
				t.Fatalf("closeout_allowed = %t, want %t", got.CloseoutAllowed, tc.wantCloseout)
			}
			if got.PlanningRequired != tc.wantPlanning {
				t.Fatalf("planning_required = %t, want %t", got.PlanningRequired, tc.wantPlanning)
			}
			if got.ReviewDecisionPresent != tc.wantReviewDecision {
				t.Fatalf("review_decision_present = %t, want %t", got.ReviewDecisionPresent, tc.wantReviewDecision)
			}
		})
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
