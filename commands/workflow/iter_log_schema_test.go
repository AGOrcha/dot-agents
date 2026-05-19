package workflow

import (
	"strings"
	"testing"
)

func TestCompiledWorkflowIterLogSchema(t *testing.T) {
	sch, err := compiledWorkflowIterLogSchema(stdSchemaCompiler{})
	if err != nil {
		t.Fatalf("compiledWorkflowIterLogSchema: %v", err)
	}
	if sch == nil {
		t.Error("expected non-nil schema")
	}
}

func TestValidateWorkflowIterLogEntry_Invalid(t *testing.T) {
	if err := validateWorkflowIterLogEntry(&iterLogEntry{}); err == nil {
		t.Error("expected validation failure for empty entry")
	}
}

func TestValidateWorkflowIterLogEntry_Valid(t *testing.T) {
	if err := validateWorkflowIterLogEntry(newValidIterLogEntry()); err != nil {
		t.Fatalf("valid entry rejected: %v", err)
	}
}

// TestValidateWorkflowIterLogEntry_RemapUnmarshalError drives the second
// json.Unmarshal ("remap") error branch: jsonMarshal is seam-swapped to
// return syntactically invalid JSON bytes (not an error), so the marshal
// succeeds but the remap unmarshal fails.
func TestValidateWorkflowIterLogEntry_RemapUnmarshalError(t *testing.T) {
	withJSONMarshalStub(t, func(any) ([]byte, error) { return []byte("{not-json"), nil })

	err := validateWorkflowIterLogEntry(newValidIterLogEntry())
	if err == nil {
		t.Fatal("expected remap unmarshal error, got nil")
	}
	if !strings.Contains(err.Error(), "remap iteration log for schema validation") {
		t.Errorf("expected remap error message, got %q", err.Error())
	}
}

func TestValidateWorkflowIterLogEntry_CompileError(t *testing.T) {
	resetCompiledSchemaOnce(t, &workflowIterLogCompiledOnce,
		&workflowIterLogCompiled, &workflowIterLogCompiledErr)

	// Prime the once-block with an injected failing compiler so the cached
	// CompiledErr is non-nil; validateWorkflowIterLogEntry then exercises
	// its compiled-schema error-propagation guard on the real (std) call.
	if _, err := compiledWorkflowIterLogSchema(addResourceErrCompiler()); err == nil {
		t.Fatal("precondition: primed compile should have failed")
	}

	if err := validateWorkflowIterLogEntry(newValidIterLogEntry()); err == nil {
		t.Fatal("expected compiled-schema error to propagate, got nil")
	}
}
