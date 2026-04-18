package workflow

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

// VerifierTypeMergeBack is the verifier_type / filename stem for worker merge-back results
// (`.agents/active/verification/<task_id>/merge-back.result.yaml`).
const VerifierTypeMergeBack = "merge-back"

//go:embed static/verification-result.schema.json
var verificationResultSchemaJSON []byte

var (
	verificationResultCompiled     *jsonschema.Schema
	verificationResultCompiledOnce sync.Once
	verificationResultCompiledErr  error
)

func compiledVerificationResultSchema() (*jsonschema.Schema, error) {
	verificationResultCompiledOnce.Do(func() {
		var doc any
		if err := json.Unmarshal(verificationResultSchemaJSON, &doc); err != nil {
			verificationResultCompiledErr = fmt.Errorf("parse embedded verification-result schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		const schemaURL = "./schemas/verification-result.schema.json"
		if err := c.AddResource(schemaURL, doc); err != nil {
			verificationResultCompiledErr = fmt.Errorf("register verification-result schema: %w", err)
			return
		}
		verificationResultCompiled, verificationResultCompiledErr = c.Compile(schemaURL)
	})
	return verificationResultCompiled, verificationResultCompiledErr
}

// VerificationResultDoc is the typed payload for
// `.agents/active/verification/<task_id>/<verifier_type>.result.yaml`.
type VerificationResultDoc struct {
	SchemaVersion int      `json:"schema_version" yaml:"schema_version"`
	TaskID        string   `json:"task_id" yaml:"task_id"`
	ParentPlanID  string   `json:"parent_plan_id" yaml:"parent_plan_id"`
	VerifierType  string   `json:"verifier_type" yaml:"verifier_type"`
	Status        string   `json:"status" yaml:"status"`
	Summary       string   `json:"summary" yaml:"summary"`
	RecordedAt    string   `json:"recorded_at" yaml:"recorded_at"`
	DelegationID  string   `json:"delegation_id,omitempty" yaml:"delegation_id,omitempty"`
	RecordedBy    string   `json:"recorded_by,omitempty" yaml:"recorded_by,omitempty"`
	Commands      []string `json:"commands,omitempty" yaml:"commands,omitempty"`
	ArtifactPaths []string `json:"artifact_paths,omitempty" yaml:"artifact_paths,omitempty"`
}

// validateVerificationResultDoc checks doc against schemas/verification-result.schema.json.
func validateVerificationResultDoc(doc *VerificationResultDoc) error {
	if doc == nil {
		return fmt.Errorf("verification result: nil document")
	}
	sch, err := compiledVerificationResultSchema()
	if err != nil {
		return err
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal verification result for schema validation: %w", err)
	}
	var payload any
	if err := json.Unmarshal(b, &payload); err != nil {
		return fmt.Errorf("remap verification result for schema validation: %w", err)
	}
	if err := sch.Validate(payload); err != nil {
		return fmt.Errorf("verification result does not satisfy verification-result.schema.json: %w", err)
	}
	return nil
}

func verificationResultFilePath(projectPath, taskID, verifierType string) (string, error) {
	verifierType = strings.TrimSpace(verifierType)
	if verifierType == "" {
		return "", fmt.Errorf("verifier_type is required")
	}
	if !validVerificationVerifierTypeStem(verifierType) {
		return "", fmt.Errorf("invalid verifier_type %q", verifierType)
	}
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return "", fmt.Errorf("task_id is required")
	}
	dir := filepath.Join(projectPath, ".agents", "active", "verification", taskID)
	return filepath.Join(dir, verifierType+".result.yaml"), nil
}

// validVerificationVerifierTypeStem matches JSON Schema pattern ^[a-z][a-z0-9_-]*$.
func validVerificationVerifierTypeStem(s string) bool {
	if len(s) == 0 {
		return false
	}
	if s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '_' || c == '-':
		default:
			return false
		}
	}
	return true
}

// writeVerificationResultYAML writes doc to `.agents/active/verification/<task_id>/<verifier_type>.result.yaml`
// after schema validation.
func writeVerificationResultYAML(projectPath string, doc *VerificationResultDoc) error {
	if doc == nil {
		return fmt.Errorf("verification result: nil document")
	}
	if strings.TrimSpace(doc.VerifierType) == "" {
		return fmt.Errorf("verification result: verifier_type is required")
	}
	path, err := verificationResultFilePath(projectPath, doc.TaskID, doc.VerifierType)
	if err != nil {
		return err
	}
	if err := validateVerificationResultDoc(doc); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("prepare verification dir: %w", err)
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return fmt.Errorf("marshal verification result yaml: %w", err)
	}
	if err := os.WriteFile(path, out, 0644); err != nil {
		return fmt.Errorf("write verification result: %w", err)
	}
	return nil
}
