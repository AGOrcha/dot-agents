package workflow

import (
	"encoding/json"
	"os"
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

func TestDeriveScopeMode(t *testing.T) {
	// Positive: app_type always means code mode.
	task := &CanonicalTask{AppType: "go-cli"}
	if got := deriveScopeMode(task); got != "code" {
		t.Errorf("deriveScopeMode(app_type=go-cli) = %q, want code", got)
	}

	// Positive: notes with "research task" → research mode.
	task2 := &CanonicalTask{Notes: "Research task — no Go code."}
	if got := deriveScopeMode(task2); got != "research" {
		t.Errorf("deriveScopeMode(research task note) = %q, want research", got)
	}

	// Positive: default with Go write_scope → code mode.
	task3 := &CanonicalTask{WriteScope: []string{"commands/workflow/cmd.go"}}
	if got := deriveScopeMode(task3); got != "code" {
		t.Errorf("deriveScopeMode(go write_scope) = %q, want code", got)
	}

	// Negative: docs-only write_scope → doc mode.
	task4 := &CanonicalTask{WriteScope: []string{"docs/spec.md", "docs/notes.md"}}
	if got := deriveScopeMode(task4); got != "doc" {
		t.Errorf("deriveScopeMode(docs-only write_scope) = %q, want doc", got)
	}
}

func TestDeriveScopeConfidence(t *testing.T) {
	// Positive: code mode, both lanes ready, seeds provided, queries run → medium.
	if got := deriveScopeConfidence("code", true, true, true, 2); got != "medium" {
		t.Errorf("want medium, got %q", got)
	}

	// Positive: research mode, context-lane ready → medium.
	if got := deriveScopeConfidence("research", false, true, false, 0); got != "medium" {
		t.Errorf("want medium for research+context-ready, got %q", got)
	}

	// Negative: nothing ready → low.
	if got := deriveScopeConfidence("code", false, false, false, 0); got != "low" {
		t.Errorf("want low when nothing ready, got %q", got)
	}

	// Negative: research mode, no context lane → low.
	if got := deriveScopeConfidence("research", false, false, false, 0); got != "low" {
		t.Errorf("want low for research+no context, got %q", got)
	}
}

func TestRunWorkflowPlanDeriveScopeDegradesGracefully(t *testing.T) {
	repo := initWorkflowTestRepo(t)
	// Write a minimal plan+tasks fixture with a code task.
	planDir := repo + "/.agents/workflow/plans/test-derive-plan"
	if err := os.MkdirAll(planDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planDir+"/PLAN.yaml", []byte(`schema_version: 1
id: test-derive-plan
title: Test Derive Plan
status: active
created_at: "2026-01-01T00:00:00Z"
updated_at: "2026-01-01T00:00:00Z"
`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(planDir+"/TASKS.yaml", []byte(`schema_version: 1
plan_id: test-derive-plan
tasks:
  - id: my-task
    title: My Task
    status: pending
    depends_on: []
    blocks: []
    owner: test
    app_type: go-cli
    write_scope:
      - commands/workflow/cmd.go
    verification_required: true
    notes: "Implement the feature"
`), 0644); err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	// Positive: command must succeed and write a sidecar with confidence:low (no graph available).
	if err := runWorkflowPlanDeriveScope("test-derive-plan", "my-task", []string{"MySymbol"}, nil); err != nil {
		t.Fatalf("runWorkflowPlanDeriveScope: %v", err)
	}

	sidecarPath := planDir + "/evidence/my-task.scope.yaml"
	data, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}
	var ev ScopeEvidence
	if err := yaml.Unmarshal(data, &ev); err != nil {
		t.Fatalf("unmarshal sidecar: %v", err)
	}
	if ev.PlanID != "test-derive-plan" {
		t.Errorf("PlanID = %q, want test-derive-plan", ev.PlanID)
	}
	if ev.TaskID != "my-task" {
		t.Errorf("TaskID = %q, want my-task", ev.TaskID)
	}
	if ev.Confidence != "low" {
		t.Errorf("Confidence = %q, want low (no graph)", ev.Confidence)
	}
	if ev.Mode != "code" {
		t.Errorf("Mode = %q, want code", ev.Mode)
	}
	if ev.Seeds == nil || len(ev.Seeds.Symbols) == 0 {
		t.Error("Seeds.Symbols not populated from --seed-symbol")
	}
	if len(ev.RequiredPaths) == 0 {
		t.Error("RequiredPaths empty, expected at least write_scope paths")
	}
	// Verify warnings captured in open_gaps.
	if len(ev.OpenGaps) == 0 {
		t.Error("OpenGaps should contain graph-not-ready warnings")
	}

	// Negative: nonexistent task must return an error, not write a sidecar.
	if err := runWorkflowPlanDeriveScope("test-derive-plan", "nonexistent", nil, nil); err == nil {
		t.Error("expected error for nonexistent task, got nil")
	}
}
