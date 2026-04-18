package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NikashPrakash/dot-agents/internal/config"
	"github.com/NikashPrakash/dot-agents/internal/ui"
)

// ── Verification log ──────────────────────────────────────────────────────────

func isValidVerificationKind(k string) bool {
	switch strings.TrimSpace(strings.ToLower(k)) {
	case "test", "lint", "build", "format", "custom", "review":
		return true
	default:
		return false
	}
}

func isValidVerificationScope(s string) bool {
	switch s {
	case "file", "package", "repo", "custom":
		return true
	default:
		return false
	}
}

func verificationLogPath(project string) string {
	return filepath.Join(config.ProjectContextDir(project), "verification-log.jsonl")
}

func appendVerificationLog(project string, rec VerificationRecord) error {
	if err := os.MkdirAll(config.ProjectContextDir(project), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(verificationLogPath(project), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(f, "%s\n", line)
	return err
}

func readVerificationLog(project string, limit int) ([]VerificationRecord, error) {
	content, err := os.ReadFile(verificationLogPath(project))
	if err != nil {
		if os.IsNotExist(err) {
			return []VerificationRecord{}, nil
		}
		return nil, err
	}
	var records []VerificationRecord
	for _, line := range strings.Split(strings.TrimSpace(string(content)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec VerificationRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue // skip malformed lines
		}
		records = append(records, rec)
	}
	if limit > 0 && len(records) > limit {
		records = records[len(records)-limit:]
	}
	return records, nil
}

func runWorkflowVerifyRecordReview(command, scope, summary, phase1In, phase2In, overallIn, escalation, reviewerNotes, taskFlag string, failedGatesInput []string) error {
	if !isValidVerificationScope(scope) {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid scope %q", scope),
			"Valid verification scopes: `file`, `package`, `repo`, `custom`.",
		)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	phase1, err := parseReviewPhaseDecision("--phase1-decision", phase1In)
	if err != nil {
		return err
	}
	phase2, err := parseReviewPhaseDecision("--phase2-decision", phase2In)
	if err != nil {
		return err
	}
	derived := deriveOverallReviewDecision(phase1, phase2)
	overall := strings.TrimSpace(strings.ToLower(overallIn))
	if overall == "" {
		overall = derived
	} else if overall != derived {
		return deps.ErrorWithHints(
			fmt.Sprintf("overall decision %q disagrees with phases (derived %q from phase_1=%s phase_2=%s)", overall, derived, phase1, phase2),
			"Omit --overall-decision to use derived consolidation, or adjust phase flags so the derived value matches.",
		)
	}
	if overall == "escalate" && strings.TrimSpace(escalation) == "" {
		return deps.ErrorWithHints(
			"overall decision is escalate but --escalation-reason is empty",
			"Provide a non-empty --escalation-reason whenever the consolidated decision is escalate.",
		)
	}

	taskID := strings.TrimSpace(taskFlag)
	var contract *DelegationContract
	if taskID == "" {
		contract = firstReadableDelegationContract(project.Path)
		if contract == nil {
			return deps.ErrorWithHints(
				"review verify record needs a delegation task id",
				"Pass --task <task_id> matching `.agents/active/delegation/<task_id>.yaml`, or keep a single readable active delegation contract.",
			)
		}
		taskID = contract.ParentTaskID
	} else {
		contract, err = loadDelegationContract(project.Path, taskID)
		if err != nil {
			return fmt.Errorf("load delegation contract for task %q: %w", taskID, err)
		}
	}

	failedGates := trimStringSlice(failedGatesInput)
	if failedGates == nil {
		failedGates = []string{}
	}

	now := time.Now().UTC().Format(time.RFC3339)
	doc := &ReviewDecisionDoc{
		SchemaVersion:    1,
		TaskID:           taskID,
		ParentPlanID:     contract.ParentPlanID,
		DelegationID:     contract.ID,
		Phase1Decision:   phase1,
		Phase2Decision:   phase2,
		OverallDecision:  overall,
		FailedGates:      failedGates,
		EscalationReason: strings.TrimSpace(escalation),
		ReviewerNotes:    strings.TrimSpace(reviewerNotes),
		RecordedAt:       now,
		RecordedBy:       "dot-agents workflow verify record",
	}
	if err := writeReviewDecisionYAML(project.Path, doc); err != nil {
		return err
	}

	artifactRel := iterLogReviewDecisionPath(taskID)
	rec := VerificationRecord{
		SchemaVersion: 1,
		Timestamp:     now,
		Kind:          "review",
		Status:        overallDecisionToVerificationStatus(overall),
		Command:       strings.TrimSpace(command),
		Scope:         scope,
		Summary:       strings.TrimSpace(summary),
		Artifacts:     []string{artifactRel},
		RecordedBy:    "dot-agents workflow verify record",
	}
	if err := appendVerificationLog(project.Name, rec); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Review decision recorded for task %s: overall=%s (%s)", taskID, overall, strings.TrimSpace(summary)))
	return nil
}

func runWorkflowVerifyRecord(kind, status, command, scope, summary string) error {
	if strings.TrimSpace(strings.ToLower(kind)) == "review" {
		return fmt.Errorf("internal error: use runWorkflowVerifyRecordReview for kind review")
	}
	if !isValidVerificationKind(kind) {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid kind %q", kind),
			"Valid verification kinds: `test`, `lint`, `build`, `format`, `custom`, `review`.",
		)
	}
	if !isValidVerificationStatus(status) {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid status %q", status),
			"Valid verification statuses: `pass`, `fail`, `partial`, `unknown`.",
		)
	}
	if !isValidVerificationScope(scope) {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid scope %q", scope),
			"Valid verification scopes: `file`, `package`, `repo`, `custom`.",
		)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	rec := VerificationRecord{
		SchemaVersion: 1,
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Kind:          kind,
		Status:        status,
		Command:       command,
		Scope:         scope,
		Summary:       summary,
		Artifacts:     []string{},
		RecordedBy:    "dot-agents workflow verify record",
	}
	if err := appendVerificationLog(project.Name, rec); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Verification recorded: %s %s (%s)", kind, status, summary))
	return nil
}

func runWorkflowVerifyLog(all bool) error {
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	limit := 10
	if all {
		limit = 0
	}
	records, err := readVerificationLog(project.Name, limit)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Fprintln(os.Stdout, "No verification records found.")
		return nil
	}
	if deps.Flags.JSON() {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(records)
	}
	ui.Header("Verification Log")
	for _, r := range records {
		icon := "✓"
		if r.Status == "fail" {
			icon = "✗"
		} else if r.Status == "partial" {
			icon = "~"
		} else if r.Status == "unknown" {
			icon = "?"
		}
		fmt.Fprintf(os.Stdout, "  %s [%s] %s  %s\n", icon, r.Kind, r.Timestamp, r.Summary)
		if r.Command != "" {
			fmt.Fprintf(os.Stdout, "    cmd: %s\n", r.Command)
		}
	}
	fmt.Fprintln(os.Stdout)
	return nil
}
