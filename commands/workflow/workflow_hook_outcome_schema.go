package workflow

import (
	// _ "embed": pull in static/workflow-hook-outcome.schema.json via go:embed.
	_ "embed"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// WorkflowHookOutcomeSchemaJSON is the embedded twin of
// schemas/workflow-hook-outcome.schema.json (v1, per r1-5-hook-enforcement-telemetry
// design D2). The byte-equal guarantee between this embedded copy and the
// canonical file is enforced by
// TestWorkflowHookOutcomeEmbeddedSchemaMatchesCanonical so editors and
// JSON-schema tooling read the same contract the Go binary validates against.
//
// Exported so the parallel t1-capture-outcomes worker (commands/workflow/hook_outcome.go)
// can reach the bytes if it ever needs the raw schema (e.g. for documentation
// emission). The expected callsite is WorkflowHookOutcomeSchema below, which
// returns a compiled validator instead of the raw bytes.
//
//go:embed static/workflow-hook-outcome.schema.json
var WorkflowHookOutcomeSchemaJSON []byte

var (
	workflowHookOutcomeCompiled     *jsonschema.Schema
	workflowHookOutcomeCompiledOnce sync.Once
	workflowHookOutcomeCompiledErr  error
)

// compiledWorkflowHookOutcomeSchema compiles the embedded
// workflow-hook-outcome.schema.json once and returns the cached
// *jsonschema.Schema. The schemaCompiler seam mirrors the pattern in
// compiledWorkflowIterLogSchema / compiledWorkflowHookSentinelSchema so
// tests can drive the compile-error branch via addResourceErrCompiler.
func compiledWorkflowHookOutcomeSchema(sc schemaCompiler) (*jsonschema.Schema, error) {
	workflowHookOutcomeCompiledOnce.Do(func() {
		const schemaURL = "./schemas/workflow-hook-outcome.schema.json"
		workflowHookOutcomeCompiled, workflowHookOutcomeCompiledErr = compileEmbeddedSchema(
			sc, WorkflowHookOutcomeSchemaJSON, schemaURL, "workflow-hook-outcome")
	})
	return workflowHookOutcomeCompiled, workflowHookOutcomeCompiledErr
}

// WorkflowHookOutcomeSchema returns the compiled validator for
// workflow-hook-outcome.schema.json (the sidecar at
// .agents/active/iteration-log/iter-N.hook-outcomes.yaml).
//
// Exported for the parallel t1-capture-outcomes worker: t1's
// `da workflow hook-outcome write` CLI calls this to validate the
// sidecar payload before atomic-rename publish. Returning the compiled
// *jsonschema.Schema (rather than a doc-specific validate helper) keeps
// this slice free of any HookOutcomeDoc type — t1 owns that struct and
// the marshal/validate plumbing per the anti-scope split.
//
// Cold-path callers should pass stdSchemaCompiler{}; tests can substitute
// a faulting compiler to drive error branches.
func WorkflowHookOutcomeSchema() (*jsonschema.Schema, error) {
	return compiledWorkflowHookOutcomeSchema(stdSchemaCompiler{})
}
