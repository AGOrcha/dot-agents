package workflow

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestComputeWorkflowHealth_CompletedPlansPendingArchive verifies the new field is
// populated from LocalDrift and that status thresholds are unaffected.
func TestComputeWorkflowHealth_CompletedPlansPendingArchive(t *testing.T) {
	t.Run("positive: count reflects LocalDrift.CompletedPlanIDs", func(t *testing.T) {
		state := &workflowOrientState{
			Git: workflowGitSummary{Branch: "main"},
			LocalDrift: &RepoDriftReport{
				CompletedPlanIDs: []string{"plan-a", "plan-b"},
			},
		}
		h := computeWorkflowHealth(state)
		if h.Workflow.CompletedPlansPendingArchive != 2 {
			t.Errorf("CompletedPlansPendingArchive = %d, want 2", h.Workflow.CompletedPlansPendingArchive)
		}
		// Status must not change — informational only
		if h.Status == "partial" || h.Status == "degraded" {
			t.Errorf("status changed to %q due to pending archive count; should not affect thresholds", h.Status)
		}
	})

	t.Run("negative: zero when LocalDrift is nil", func(t *testing.T) {
		state := &workflowOrientState{
			Git:        workflowGitSummary{Branch: "main"},
			LocalDrift: nil,
		}
		h := computeWorkflowHealth(state)
		if h.Workflow.CompletedPlansPendingArchive != 0 {
			t.Errorf("CompletedPlansPendingArchive = %d, want 0 when no drift", h.Workflow.CompletedPlansPendingArchive)
		}
	})

	t.Run("json: field present in marshaled output", func(t *testing.T) {
		state := &workflowOrientState{
			Git: workflowGitSummary{Branch: "main"},
			LocalDrift: &RepoDriftReport{
				CompletedPlanIDs: []string{"plan-x"},
			},
		}
		h := computeWorkflowHealth(state)
		data, err := json.Marshal(h)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), `"completed_plans_pending_archive":1`) {
			t.Errorf("JSON output missing completed_plans_pending_archive:1, got: %s", string(data))
		}
	})
}
