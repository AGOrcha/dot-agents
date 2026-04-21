package workflow

import (
	"encoding/json"
	"testing"

	"go.yaml.in/yaml/v3"
)

const illustrativeScopeYAML = `
schema_version: 1
plan_id: loop-agent-pipeline
task_id: p6-fanout-dispatch
status: draft
mode: code
goal: Consume persisted app_type and verifier_sequence at runtime
confidence: medium
decision_locks:
  - Keep one canonical delegated task per workflow task
  - Extend existing workflow verify record surfaces
required_reads:
  - path: .agents/workflow/plans/loop-agent-pipeline/loop-agent-pipeline.plan.md
    why: active canonical plan and runtime boundary
seeds:
  symbols:
    - commands.workflow.RunFanout
  paths:
    - commands/workflow/delegation.go
  rationale:
    - fanout metadata exists but runtime dispatch is incomplete
queries:
  - tool: kg
    kind: bridge_query
    intent: symbol_lookup
    subject: commands.workflow.RunFanout
    summary:
      files:
        - commands/workflow/delegation.go
required_paths:
  - path: commands/workflow/delegation.go
    because:
      - symbol definition
optional_paths:
  - path: docs/LOOP_ORCHESTRATION_SPEC.md
    because:
      - contract wording may need alignment
excluded_paths:
  - path: bin/tests/ralph-pipeline
    rationale:
      - related runtime but not in this slice
provides:
  - runtime consumption of persisted verifier routing metadata
consumes:
  - task app_type metadata
final_write_scope:
  - commands/workflow/delegation.go
  - commands/workflow/cmd.go
verification_focus:
  - workflow fanout and bundle tests prove runtime-visible verifier routing metadata
allowed_local_choices:
  - helper extraction inside commands/workflow/*
stop_conditions:
  - if implementing requires a new top-level delegation contract stop and fold back
open_gaps:
  - graph has no direct coverage mapping for shell harnesses
`

func TestScopeEvidenceUnmarshalRoundTrip(t *testing.T) {
	// Positive: unmarshal valid YAML and verify all fields are populated.
	var ev ScopeEvidence
	if err := yaml.Unmarshal([]byte(illustrativeScopeYAML), &ev); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	if ev.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", ev.SchemaVersion)
	}
	if ev.PlanID != "loop-agent-pipeline" {
		t.Errorf("PlanID = %q, want loop-agent-pipeline", ev.PlanID)
	}
	if ev.TaskID != "p6-fanout-dispatch" {
		t.Errorf("TaskID = %q, want p6-fanout-dispatch", ev.TaskID)
	}
	if ev.Confidence != "medium" {
		t.Errorf("Confidence = %q, want medium", ev.Confidence)
	}
	if len(ev.DecisionLocks) != 2 {
		t.Errorf("DecisionLocks len = %d, want 2", len(ev.DecisionLocks))
	}
	if len(ev.RequiredReads) != 1 {
		t.Errorf("RequiredReads len = %d, want 1", len(ev.RequiredReads))
	}
	if ev.RequiredReads[0].Path != ".agents/workflow/plans/loop-agent-pipeline/loop-agent-pipeline.plan.md" {
		t.Errorf("RequiredReads[0].Path = %q", ev.RequiredReads[0].Path)
	}
	if ev.Seeds == nil {
		t.Fatal("Seeds is nil, want non-nil")
	}
	if len(ev.Seeds.Symbols) != 1 {
		t.Errorf("Seeds.Symbols len = %d, want 1", len(ev.Seeds.Symbols))
	}
	if len(ev.Queries) != 1 {
		t.Errorf("Queries len = %d, want 1", len(ev.Queries))
	}
	if ev.Queries[0].Summary == nil || len(ev.Queries[0].Summary.Files) != 1 {
		t.Error("Queries[0].Summary.Files not populated")
	}
	if len(ev.RequiredPaths) != 1 {
		t.Errorf("RequiredPaths len = %d, want 1", len(ev.RequiredPaths))
	}
	if len(ev.OptionalPaths) != 1 {
		t.Errorf("OptionalPaths len = %d, want 1", len(ev.OptionalPaths))
	}
	if len(ev.ExcludedPaths) != 1 {
		t.Errorf("ExcludedPaths len = %d, want 1", len(ev.ExcludedPaths))
	}
	if len(ev.Provides) != 1 {
		t.Errorf("Provides len = %d, want 1", len(ev.Provides))
	}
	if len(ev.Consumes) != 1 {
		t.Errorf("Consumes len = %d, want 1", len(ev.Consumes))
	}
	if len(ev.FinalWriteScope) != 2 {
		t.Errorf("FinalWriteScope len = %d, want 2", len(ev.FinalWriteScope))
	}
	if len(ev.StopConditions) != 1 {
		t.Errorf("StopConditions len = %d, want 1", len(ev.StopConditions))
	}
	if len(ev.OpenGaps) != 1 {
		t.Errorf("OpenGaps len = %d, want 1", len(ev.OpenGaps))
	}

	// Round-trip through JSON.
	data, err := json.Marshal(&ev)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var ev2 ScopeEvidence
	if err := json.Unmarshal(data, &ev2); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if ev2.PlanID != ev.PlanID {
		t.Errorf("round-trip PlanID = %q, want %q", ev2.PlanID, ev.PlanID)
	}
	if ev2.Confidence != ev.Confidence {
		t.Errorf("round-trip Confidence = %q, want %q", ev2.Confidence, ev.Confidence)
	}
}

func TestNewScopeEvidenceSlicesNotNil(t *testing.T) {
	// Positive: NewScopeEvidence returns an instance with no nil slices.
	ev := NewScopeEvidence("my-plan", "my-task")
	if ev.DecisionLocks == nil {
		t.Error("DecisionLocks is nil, want []string{}")
	}
	if ev.RequiredReads == nil {
		t.Error("RequiredReads is nil")
	}
	if ev.Queries == nil {
		t.Error("Queries is nil")
	}
	if ev.RequiredPaths == nil {
		t.Error("RequiredPaths is nil")
	}
	if ev.OptionalPaths == nil {
		t.Error("OptionalPaths is nil")
	}
	if ev.ExcludedPaths == nil {
		t.Error("ExcludedPaths is nil")
	}
	if ev.Provides == nil {
		t.Error("Provides is nil")
	}
	if ev.Consumes == nil {
		t.Error("Consumes is nil")
	}
	if ev.FinalWriteScope == nil {
		t.Error("FinalWriteScope is nil")
	}
	if ev.VerificationFocus == nil {
		t.Error("VerificationFocus is nil")
	}
	if ev.AllowedLocalChoices == nil {
		t.Error("AllowedLocalChoices is nil")
	}
	if ev.StopConditions == nil {
		t.Error("StopConditions is nil")
	}
	if ev.OpenGaps == nil {
		t.Error("OpenGaps is nil")
	}

	// Negative: JSON marshaling of empty slices should produce [] not null.
	data, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	js := string(data)
	// Spot-check two fields that must not appear as null.
	if containsNullSlice(js, "decision_locks") {
		t.Error("decision_locks marshaled as null, want []")
	}
	if containsNullSlice(js, "provides") {
		t.Error("provides marshaled as null, want []")
	}
}

// containsNullSlice checks that a JSON string does NOT contain `"<field>":null`.
func containsNullSlice(js, field string) bool {
	needle := `"` + field + `":null`
	for i := 0; i+len(needle) <= len(js); i++ {
		if js[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func TestScopeEvidenceUnmarshalInvalid(t *testing.T) {
	// Negative: unmarshal completely invalid YAML should fail gracefully.
	bad := `{not valid yaml: [`
	var ev ScopeEvidence
	err := yaml.Unmarshal([]byte(bad), &ev)
	if err == nil {
		t.Error("expected error on malformed YAML, got nil")
	}
}
