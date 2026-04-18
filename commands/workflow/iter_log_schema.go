package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed static/workflow-iter-log.schema.json
var workflowIterLogSchemaJSON []byte

var (
	workflowIterLogCompiled     *jsonschema.Schema
	workflowIterLogCompiledOnce sync.Once
	workflowIterLogCompiledErr  error
)

func compiledWorkflowIterLogSchema() (*jsonschema.Schema, error) {
	workflowIterLogCompiledOnce.Do(func() {
		var doc any
		if err := json.Unmarshal(workflowIterLogSchemaJSON, &doc); err != nil {
			workflowIterLogCompiledErr = fmt.Errorf("parse embedded workflow-iter-log schema: %w", err)
			return
		}
		c := jsonschema.NewCompiler()
		const schemaURL = "./schemas/workflow-iter-log.schema.json"
		if err := c.AddResource(schemaURL, doc); err != nil {
			workflowIterLogCompiledErr = fmt.Errorf("register workflow-iter-log schema: %w", err)
			return
		}
		workflowIterLogCompiled, workflowIterLogCompiledErr = c.Compile(schemaURL)
	})
	return workflowIterLogCompiled, workflowIterLogCompiledErr
}

// validateWorkflowIterLogEntry validates entry against the embedded
// schemas/workflow-iter-log.schema.json before writing iter-N.yaml.
// Uses JSON marshal for the validation round-trip since jsonschema/v6
// operates on JSON-compatible types.
func validateWorkflowIterLogEntry(entry *iterLogEntry) error {
	sch, err := compiledWorkflowIterLogSchema()
	if err != nil {
		return err
	}
	b, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal iteration log for schema validation: %w", err)
	}
	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		return fmt.Errorf("remap iteration log for schema validation: %w", err)
	}
	if err := sch.Validate(doc); err != nil {
		return fmt.Errorf("iteration log does not satisfy workflow-iter-log.schema.json: %w", err)
	}
	return nil
}
