package workflow

import "testing"

func TestCompiledWorkflowIterLogSchema(t *testing.T) {
	sch, err := compiledWorkflowIterLogSchema()
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
