package workflow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AGOrcha/dot-agents/internal/config"
	"github.com/AGOrcha/dot-agents/internal/journal"
	"github.com/AGOrcha/dot-agents/internal/ui"
)

const verifyRecordedByLabel = "da workflow verify record"

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
	if err := osMkdirAll(config.ProjectContextDir(project), 0755); err != nil {
		return err
	}
	f, err := osOpenFile(verificationLogPath(project), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	line, err := jsonMarshal(rec)
	if err != nil {
		return err
	}
	_, err = fprintfFunc(f, "%s\n", line)
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

func resolveReviewOverallDecision(phase1, phase2, overallIn, escalation string) (string, error) {
	derived := deriveOverallReviewDecision(phase1, phase2)
	overall := strings.TrimSpace(strings.ToLower(overallIn))
	if overall == "" {
		overall = derived
	} else if overall != derived {
		return "", deps.ErrorWithHints(
			fmt.Sprintf("overall decision %q disagrees with phases (derived %q from phase_1=%s phase_2=%s)", overall, derived, phase1, phase2),
			"Omit --overall-decision to use derived consolidation, or adjust phase flags so the derived value matches.",
		)
	}
	if overall == "escalate" && strings.TrimSpace(escalation) == "" {
		return "", deps.ErrorWithHints(
			"overall decision is escalate but --escalation-reason is empty",
			"Provide a non-empty --escalation-reason whenever the consolidated decision is escalate.",
		)
	}
	return overall, nil
}

func resolveReviewDelegationContract(projectPath, taskFlag string) (string, *DelegationContract, error) {
	taskID := strings.TrimSpace(taskFlag)
	if taskID == "" {
		contract := firstReadableDelegationContract(projectPath)
		if contract == nil {
			return "", nil, deps.ErrorWithHints(
				"review verify record needs a delegation task id",
				"Pass --task <task_id> matching `.agents/active/delegation/<task_id>.yaml`, or keep a single readable active delegation contract.",
			)
		}
		return contract.ParentTaskID, contract, nil
	}
	contract, err := loadDelegationContract(projectPath, taskID)
	if err != nil {
		return "", nil, fmt.Errorf("load delegation contract for task %q: %w", taskID, err)
	}
	return taskID, contract, nil
}

// reviewRecordInputs bundles inputs for runWorkflowVerifyRecordReview so the
// call site stays under the function-parameter limit while keeping each field
// individually addressable from the caller.
type reviewRecordInputs struct {
	Command       string
	Scope         string
	Summary       string
	Phase1In      string
	Phase2In      string
	OverallIn     string
	Escalation    string
	ReviewerNotes string
	TaskFlag      string
	FailedGates   []string
}

func runWorkflowVerifyRecordReview(in reviewRecordInputs) error {
	if !isValidVerificationScope(in.Scope) {
		return deps.ErrorWithHints(
			fmt.Sprintf("invalid scope %q", in.Scope),
			"Valid verification scopes: `file`, `package`, `repo`, `custom`.",
		)
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	phase1, err := parseReviewPhaseDecision("--phase1-decision", in.Phase1In)
	if err != nil {
		return err
	}
	phase2, err := parseReviewPhaseDecision("--phase2-decision", in.Phase2In)
	if err != nil {
		return err
	}
	overall, err := resolveReviewOverallDecision(phase1, phase2, in.OverallIn, in.Escalation)
	if err != nil {
		return err
	}

	taskID, contract, err := resolveReviewDelegationContract(project.Path, in.TaskFlag)
	if err != nil {
		return err
	}

	failedGates := trimStringSlice(in.FailedGates)
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
		EscalationReason: strings.TrimSpace(in.Escalation),
		ReviewerNotes:    strings.TrimSpace(in.ReviewerNotes),
		RecordedAt:       now,
		RecordedBy:       verifyRecordedByLabel,
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
		Command:       strings.TrimSpace(in.Command),
		Scope:         in.Scope,
		Summary:       strings.TrimSpace(in.Summary),
		Artifacts:     []string{artifactRel},
		RecordedBy:    verifyRecordedByLabel,
	}
	if err := appendVerificationLog(project.Name, rec); err != nil {
		return err
	}
	publishLatestIterationBestEffort(project.Path)
	ui.Success(fmt.Sprintf("Review decision recorded for task %s: overall=%s (%s)", taskID, overall, strings.TrimSpace(in.Summary)))
	return nil
}

func validateVerifyRecordInputs(kind, status, scope string) error {
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
	return nil
}

// verifyResultArtifactInputs bundles the fields needed by writeVerifyResultArtifact
// so the function stays under the parameter limit while the call site keeps
// each field individually addressable.
type verifyResultArtifactInputs struct {
	ProjectPath  string
	TaskID       string
	Kind         string
	Status       string
	Command      string
	Summary      string
	VerifierType string
	Now          string
}

// writeVerifyResultArtifact writes the typed <verifier_type>.result.yaml when
// taskID is non-empty and returns the slash-joined relative artifact path.
func writeVerifyResultArtifact(in verifyResultArtifactInputs) (string, error) {
	if in.TaskID == "" {
		return "", nil
	}
	vt := strings.TrimSpace(in.VerifierType)
	if vt == "" {
		vt = strings.TrimSpace(strings.ToLower(in.Kind))
	}
	if !validVerificationVerifierTypeStem(vt) {
		return "", deps.ErrorWithHints(
			fmt.Sprintf("verifier-type %q is not a valid artifact stem (must match ^[a-z][a-z0-9_-]*$)", vt),
			"Use a profile id like `unit`, `api`, `batch`, or omit --verifier-type to derive it from --kind.",
		)
	}
	contract, cerr := loadDelegationContract(in.ProjectPath, in.TaskID)
	if cerr != nil {
		return "", fmt.Errorf("load delegation contract for task %q: %w", in.TaskID, cerr)
	}
	doc := &VerificationResultDoc{
		SchemaVersion: 1,
		TaskID:        in.TaskID,
		ParentPlanID:  contract.ParentPlanID,
		VerifierType:  vt,
		Status:        in.Status,
		Summary:       strings.TrimSpace(in.Summary),
		RecordedAt:    in.Now,
		DelegationID:  contract.ID,
		RecordedBy:    verifyRecordedByLabel,
	}
	if strings.TrimSpace(in.Command) != "" {
		doc.Commands = []string{in.Command}
	}
	if err := writeVerificationResultYAML(in.ProjectPath, doc); err != nil {
		return "", fmt.Errorf("write verification result artifact: %w", err)
	}
	return filepath.ToSlash(filepath.Join(".agents", "active", "verification", in.TaskID, vt+".result.yaml")), nil
}

// verifyRecordInputs bundles inputs for runWorkflowVerifyRecord so the signature
// stays under the parameter limit while keeping each field individually addressable.
type verifyRecordInputs struct {
	Kind         string
	Status       string
	Command      string
	Scope        string
	Summary      string
	TaskID       string
	VerifierType string
}

func runWorkflowVerifyRecord(in verifyRecordInputs) error {
	if err := validateVerifyRecordInputs(in.Kind, in.Status, in.Scope); err != nil {
		return err
	}
	project, err := currentWorkflowProject()
	if err != nil {
		return err
	}
	input := &journal.VerifyRecordInput{Kind: in.Kind, Status: in.Status, Scope: in.Scope, Summary: in.Summary, Task: strings.TrimSpace(in.TaskID)}
	observed := &journal.VerifyRecordObserved{}
	ok := false
	defer func() { journalTier1(project.Path, journal.CmdVerifyRecord, input, observed, ok) }()
	now := time.Now().UTC().Format(time.RFC3339)
	taskID := strings.TrimSpace(in.TaskID)

	artifactRel, err := writeVerifyResultArtifact(verifyResultArtifactInputs{
		ProjectPath:  project.Path,
		TaskID:       taskID,
		Kind:         in.Kind,
		Status:       in.Status,
		Command:      in.Command,
		Summary:      in.Summary,
		VerifierType: in.VerifierType,
		Now:          now,
	})
	if err != nil {
		return err
	}

	artifacts := []string{}
	if artifactRel != "" {
		artifacts = append(artifacts, artifactRel)
	}
	rec := VerificationRecord{
		SchemaVersion: 1,
		Timestamp:     now,
		Kind:          in.Kind,
		Status:        in.Status,
		Command:       in.Command,
		Scope:         in.Scope,
		Summary:       in.Summary,
		Artifacts:     artifacts,
		RecordedBy:    verifyRecordedByLabel,
	}
	if err := appendVerificationLog(project.Name, rec); err != nil {
		return err
	}
	observed.VerificationLogID = now
	observed.ResultArtifactPath = artifactRel
	ok = true
	publishLatestIterationBestEffort(project.Path)
	ui.Success(fmt.Sprintf("Verification recorded: %s %s (%s)", in.Kind, in.Status, in.Summary))
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
