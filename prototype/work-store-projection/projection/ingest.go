package projection

import (
	"fmt"
	"os"

	yaml "go.yaml.in/yaml/v3"
)

// IngestPlan parses a PLAN.yaml file into the typed model. Editing the file IS
// the act (the FS-is-the-interface principle): re-ingesting reflects whatever
// the file now says. Extra (non-schema) top-level keys are captured into
// ExtraKeys so the fidelity report can name exactly what the graph can't hold.
func IngestPlan(path string) (*Plan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read plan %s: %w", path, err)
	}
	return ParsePlan(data)
}

// ParsePlan is the in-memory form of IngestPlan (no disk dependency) so tests
// can drive the production parse path with literal bytes.
func ParsePlan(data []byte) (*Plan, error) {
	var p Plan
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse plan: %w", err)
	}
	extra, err := extraTopLevelKeys(data, planKnownKeys())
	if err != nil {
		return nil, err
	}
	p.ExtraKeys = extra
	return &p, nil
}

// IngestTasks parses a TASKS.yaml file into the typed model.
func IngestTasks(path string) (*TaskFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tasks %s: %w", path, err)
	}
	return ParseTasks(data)
}

// ParseTasks is the in-memory form of IngestTasks.
func ParseTasks(data []byte) (*TaskFile, error) {
	var tf TaskFile
	if err := yaml.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("parse tasks: %w", err)
	}
	return &tf, nil
}

// planKnownKeys is the set of top-level PLAN.yaml keys the canonical schema
// covers. Anything else found at the top level is non-projectable residue.
func planKnownKeys() map[string]bool {
	return map[string]bool{
		"schema_version":        true,
		"id":                    true,
		"title":                 true,
		"status":                true,
		"summary":               true,
		"created_at":            true,
		"updated_at":            true,
		"owner":                 true,
		"success_criteria":      true,
		"verification_strategy": true,
		"current_focus_task":    true,
		"default_app_type":      true,
	}
}

// extraTopLevelKeys returns the top-level mapping keys present in data that are
// not in known. It walks the raw node tree so comments-as-keys and schema drift
// surface, returning name->value for reporting. Comment-only lines (real YAML
// comments, e.g. "# Note:") are NOT keys and are reported separately via
// HasComments; they too are lost on regeneration but are not mapping entries.
func extraTopLevelKeys(data []byte, known map[string]bool) (map[string]any, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	out := map[string]any{}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return out, nil
	}
	m := root.Content[0]
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i].Value
		if !known[k] {
			out[k] = m.Content[i+1].Value
		}
	}
	return out, nil
}
