package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWorkflowHookOutcomeEmbeddedSchemaMatchesCanonical mirrors the iter-log
// and hook-sentinel dual-write guards: the embedded twin under
// commands/workflow/static/ must be byte-equal to schemas/<name>.schema.json.
// Drift between them is invisible at runtime — the Go binary reads the
// embedded copy while editors and external JSON-schema tooling read the
// canonical one. Catching the drift here keeps the contract referenced in
// r1-5-hook-enforcement-telemetry design D2 honest.
func TestWorkflowHookOutcomeEmbeddedSchemaMatchesCanonical(t *testing.T) {
	root := dotAgentsRepoRoot(t)
	want, err := os.ReadFile(filepath.Join(root, "schemas", "workflow-hook-outcome.schema.json"))
	if err != nil {
		t.Fatalf("read canonical schema: %v", err)
	}
	if string(want) != string(WorkflowHookOutcomeSchemaJSON) {
		t.Fatal("commands/workflow/static/workflow-hook-outcome.schema.json is out of sync with schemas/workflow-hook-outcome.schema.json — copy the canonical file after editing")
	}
}

// TestCompiledWorkflowHookOutcomeSchema verifies the embedded schema parses
// and compiles cleanly via the std schemaCompiler — the same shape used by
// TestCompiledWorkflowIterLogSchema.
func TestCompiledWorkflowHookOutcomeSchema(t *testing.T) {
	sch, err := compiledWorkflowHookOutcomeSchema(stdSchemaCompiler{})
	if err != nil {
		t.Fatalf("compiledWorkflowHookOutcomeSchema: %v", err)
	}
	if sch == nil {
		t.Error("expected non-nil schema")
	}
}

// TestWorkflowHookOutcomeSchema_Exported exercises the public helper the
// parallel t1-capture-outcomes worker imports. Returning a non-nil compiled
// schema is the contract t1's HookOutcomeDoc validator depends on.
func TestWorkflowHookOutcomeSchema_Exported(t *testing.T) {
	sch, err := WorkflowHookOutcomeSchema()
	if err != nil {
		t.Fatalf("WorkflowHookOutcomeSchema: %v", err)
	}
	if sch == nil {
		t.Fatal("expected non-nil compiled schema from WorkflowHookOutcomeSchema")
	}
}

// TestCompiledWorkflowHookOutcomeSchema_CompileError drives the
// error-propagation branch by injecting a faulting compiler — same
// pattern as iter_log_schema_test.go.
func TestCompiledWorkflowHookOutcomeSchema_CompileError(t *testing.T) {
	resetCompiledSchemaOnce(t, &workflowHookOutcomeCompiledOnce,
		&workflowHookOutcomeCompiled, &workflowHookOutcomeCompiledErr)

	if _, err := compiledWorkflowHookOutcomeSchema(addResourceErrCompiler()); err == nil {
		t.Fatal("expected compile error from addResourceErrCompiler, got nil")
	}
}
