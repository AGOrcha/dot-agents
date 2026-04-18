package commands

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

//go:embed static/verification-decision.schema.json
var verificationDecisionSchemaJSON []byte

var (
	verificationDecisionCompiled     *jsonschema.Schema
	verificationDecisionCompiledOnce sync.Once
	verificationDecisionCompiledErr  error
)

func compiledVerificationDecisionSchema() (*jsonschema.Schema, error) {
	verificationDecisionCompiledOnce.Do(func() {
		var doc any
		if err := json.Unmarshal(verificationDecisionSchemaJSON, &doc); err != nil {
			verificationDecisionCompiledErr = fmt.Errorf("parse embedded verification-decision schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		const schemaURL = "./schemas/verification-decision.schema.json"
		if err := c.AddResource(schemaURL, doc); err != nil {
			verificationDecisionCompiledErr = fmt.Errorf("register verification-decision schema: %w", err)
			return
		}
		verificationDecisionCompiled, verificationDecisionCompiledErr = c.Compile(schemaURL)
	})
	return verificationDecisionCompiled, verificationDecisionCompiledErr
}

// ReviewDecisionDoc is the typed payload for
// `.agents/active/verification/<task_id>/review-decision.yaml`.
type ReviewDecisionDoc struct {
	SchemaVersion    int      `json:"schema_version" yaml:"schema_version"`
	TaskID           string   `json:"task_id" yaml:"task_id"`
	ParentPlanID     string   `json:"parent_plan_id" yaml:"parent_plan_id"`
	DelegationID     string   `json:"delegation_id,omitempty" yaml:"delegation_id,omitempty"`
	Phase1Decision   string   `json:"phase_1_decision" yaml:"phase_1_decision"`
	Phase2Decision   string   `json:"phase_2_decision" yaml:"phase_2_decision"`
	OverallDecision  string   `json:"overall_decision" yaml:"overall_decision"`
	FailedGates      []string `json:"failed_gates" yaml:"failed_gates"`
	EscalationReason string   `json:"escalation_reason,omitempty" yaml:"escalation_reason,omitempty"`
	ReviewerNotes    string   `json:"reviewer_notes,omitempty" yaml:"reviewer_notes,omitempty"`
	RecordedAt       string   `json:"recorded_at" yaml:"recorded_at"`
	RecordedBy       string   `json:"recorded_by,omitempty" yaml:"recorded_by,omitempty"`
}

func parseReviewPhaseDecision(flagName, v string) (string, error) {
	s := strings.TrimSpace(strings.ToLower(v))
	switch s {
	case "accept", "reject", "escalate":
		return s, nil
	default:
		return "", fmt.Errorf("%s must be accept, reject, or escalate (got %q)", flagName, strings.TrimSpace(v))
	}
}

// deriveOverallReviewDecision applies pessimistic consolidation: any reject → reject;
// else any escalate → escalate; else accept.
func deriveOverallReviewDecision(phase1, phase2 string) string {
	if phase1 == "reject" || phase2 == "reject" {
		return "reject"
	}
	if phase1 == "escalate" || phase2 == "escalate" {
		return "escalate"
	}
	return "accept"
}

func overallDecisionToVerificationStatus(overall string) string {
	switch overall {
	case "accept":
		return "pass"
	case "reject":
		return "fail"
	default:
		return "partial"
	}
}

// validateReviewDecisionDoc checks doc against schemas/verification-decision.schema.json.
func validateReviewDecisionDoc(doc *ReviewDecisionDoc) error {
	if doc == nil {
		return fmt.Errorf("review decision: nil document")
	}
	sch, err := compiledVerificationDecisionSchema()
	if err != nil {
		return err
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal review decision for schema validation: %w", err)
	}
	var payload any
	if err := json.Unmarshal(b, &payload); err != nil {
		return fmt.Errorf("remap review decision for schema validation: %w", err)
	}
	if err := sch.Validate(payload); err != nil {
		return fmt.Errorf("review decision does not satisfy verification-decision.schema.json: %w", err)
	}
	return nil
}

func reviewDecisionYAMLPath(projectPath, taskID string) (string, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task_id is required")
	}
	rel := iterLogReviewDecisionPath(taskID)
	if rel == "" {
		return "", fmt.Errorf("task_id is required")
	}
	return filepath.Join(projectPath, filepath.FromSlash(rel)), nil
}

// writeReviewDecisionYAML writes doc to review-decision.yaml after schema validation.
func writeReviewDecisionYAML(projectPath string, doc *ReviewDecisionDoc) error {
	if doc == nil {
		return fmt.Errorf("review decision: nil document")
	}
	path, err := reviewDecisionYAMLPath(projectPath, doc.TaskID)
	if err != nil {
		return err
	}
	if err := validateReviewDecisionDoc(doc); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("prepare verification dir: %w", err)
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal review decision yaml: %w", err)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("write review decision: %w", err)
	}
	return nil
}
