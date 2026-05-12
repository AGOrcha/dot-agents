package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"
)

// DelegationGateDecision is the deterministic parent-gate readback for one task.
type DelegationGateDecision struct {
	SchemaVersion         int    `json:"schema_version"`
	TaskID                string `json:"task_id"`
	PlanID                string `json:"plan_id"`
	DelegationID          string `json:"delegation_id,omitempty"`
	MergeBackPresent      bool   `json:"merge_back_present"`
	ReviewDecisionPresent bool   `json:"review_decision_present"`
	ReviewOverallDecision string `json:"review_overall_decision,omitempty"`
	Outcome               string `json:"outcome"`
	CloseoutAllowed       bool   `json:"closeout_allowed"`
	PlanningRequired      bool   `json:"planning_required"`
	Reason                string `json:"reason"`
	EscalationReason      string `json:"escalation_reason,omitempty"`
}

func loadReviewDecisionYAML(projectPath, taskID string) (*ReviewDecisionDoc, error) {
	path, err := reviewDecisionYAMLPath(projectPath, taskID)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc ReviewDecisionDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse review decision %s: %w", taskID, err)
	}
	if err := validateReviewDecisionDoc(&doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func decisionReason(doc *ReviewDecisionDoc) string {
	if doc == nil {
		return ""
	}
	switch doc.OverallDecision {
	case "accept":
		if strings.TrimSpace(doc.ReviewerNotes) != "" {
			return strings.TrimSpace(doc.ReviewerNotes)
		}
		return "review decision accepted"
	case "reject":
		if len(doc.FailedGates) > 0 {
			gates := append([]string(nil), doc.FailedGates...)
			sort.Strings(gates)
			return fmt.Sprintf("review rejected: failed_gates=%s", strings.Join(gates, ","))
		}
		if strings.TrimSpace(doc.ReviewerNotes) != "" {
			return strings.TrimSpace(doc.ReviewerNotes)
		}
		return "review decision rejected closeout"
	case "escalate":
		if strings.TrimSpace(doc.EscalationReason) != "" {
			return strings.TrimSpace(doc.EscalationReason)
		}
		if strings.TrimSpace(doc.ReviewerNotes) != "" {
			return strings.TrimSpace(doc.ReviewerNotes)
		}
		return "review escalated; planning or human review required before closeout"
	default:
		return ""
	}
}

func evaluateDelegationGate(projectPath, taskID, planID string) (*DelegationGateDecision, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}

	contract, err := loadDelegationContract(projectPath, taskID)
	if err != nil {
		return nil, fmt.Errorf("load delegation contract for task %q: %w", taskID, err)
	}
	if planID = strings.TrimSpace(planID); planID != "" && planID != contract.ParentPlanID {
		return nil, fmt.Errorf("delegation plan_id %q does not match --plan %q", contract.ParentPlanID, planID)
	}

	if _, err := loadMergeBack(projectPath, taskID); err != nil {
		return nil, fmt.Errorf("merge-back for task %s is required before gate evaluation: %w", taskID, err)
	}

	out := &DelegationGateDecision{
		SchemaVersion:    1,
		TaskID:           taskID,
		PlanID:           contract.ParentPlanID,
		DelegationID:     contract.ID,
		MergeBackPresent: true,
		Outcome:          "reject",
		CloseoutAllowed:  false,
	}

	doc, err := loadReviewDecisionYAML(projectPath, taskID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			out.Reason = "review-decision.yaml missing; explicit accept evidence required before closeout"
			return out, nil
		}
		return nil, fmt.Errorf("load review decision for task %q: %w", taskID, err)
	}
	if strings.TrimSpace(doc.TaskID) != "" && doc.TaskID != taskID {
		return nil, fmt.Errorf("review decision task_id %q does not match task %q", doc.TaskID, taskID)
	}
	if strings.TrimSpace(doc.ParentPlanID) != "" && doc.ParentPlanID != contract.ParentPlanID {
		return nil, fmt.Errorf("review decision plan_id %q does not match delegation plan %q", doc.ParentPlanID, contract.ParentPlanID)
	}

	out.ReviewDecisionPresent = true
	out.ReviewOverallDecision = doc.OverallDecision
	out.EscalationReason = strings.TrimSpace(doc.EscalationReason)
	out.Reason = decisionReason(doc)

	switch doc.OverallDecision {
	case "accept":
		out.Outcome = "accept"
		out.CloseoutAllowed = true
	case "reject":
		out.Outcome = "reject"
	case "escalate":
		out.Outcome = "escalate"
		out.PlanningRequired = true
	default:
		return nil, fmt.Errorf("review decision overall_decision %q is invalid", doc.OverallDecision)
	}

	return out, nil
}

func runWorkflowDelegationGate(cmd *cobra.Command, _ []string) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	taskID, _ := cmd.Flags().GetString("task")
	planID, _ := cmd.Flags().GetString("plan")

	out, err := evaluateDelegationGate(project.Path, taskID, planID)
	if err != nil {
		return err
	}

	if deps.Flags.JSON() {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}

	writer := cmd.OutOrStdout()
	fmt.Fprintf(writer, "task: %s\n", out.TaskID)
	fmt.Fprintf(writer, "plan: %s\n", out.PlanID)
	fmt.Fprintf(writer, "outcome: %s\n", out.Outcome)
	fmt.Fprintf(writer, "closeout_allowed: %t\n", out.CloseoutAllowed)
	fmt.Fprintf(writer, "planning_required: %t\n", out.PlanningRequired)
	if out.ReviewDecisionPresent {
		fmt.Fprintf(writer, "review_overall_decision: %s\n", out.ReviewOverallDecision)
	} else {
		fmt.Fprintln(writer, "review_overall_decision: missing")
	}
	if strings.TrimSpace(out.Reason) != "" {
		fmt.Fprintf(writer, "reason: %s\n", out.Reason)
	}
	return nil
}
