package workflow

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.yaml.in/yaml/v3"
)

// IO seams. Tests in this package swap these to error-injecting stubs to
// cover the error-return branches that cannot be triggered via filesystem
// fixtures (e.g. MkdirAll on a writable tmp dir always succeeds).
//
// Each seam mirrors a real `os.<Name>` function used in this package. Add a
// seam only when a test needs to fault-inject the corresponding call.
var (
	osMkdirAll  = os.MkdirAll
	osWriteFile = os.WriteFile
	osOpenFile  = os.OpenFile
	osRemoveAll = os.RemoveAll
	osRename    = os.Rename
	// osGetwd lets tests inject failures into currentWorkflowProject — the
	// underlying os.Getwd never fails in TempDir-based tests, but every
	// handler's `project, err := currentWorkflowProject()` defensive branch
	// depends on it. Swap this in tests to drive those handler error paths.
	osGetwd = os.Getwd
	// osExit lets tests intercept process-terminating calls so the
	// surrounding render/load logic can be exercised end-to-end. Production
	// callers terminate as usual.
	osExit = os.Exit
	// workflowStdin is the input source for interactive prompts (e.g. sweep
	// confirmation). Tests swap this to a `strings.Reader` to drive the
	// decline/accept branches without touching the real stdin.
	workflowStdin io.Reader = os.Stdin
	// Marshal seams. yaml.Marshal and json.Marshal cannot realistically fail
	// for the well-typed structs we serialise (no chan/func fields, no
	// nil-map cycles), so the `if err != nil` branches that follow each
	// call are unreachable from production input. Tests swap these to
	// error-returning stubs to exercise those branches.
	yamlMarshal       = yaml.Marshal
	jsonMarshal       = json.Marshal
	jsonMarshalIndent = json.MarshalIndent
	// fprintfFunc wraps fmt.Fprintf for the append-log writers. The
	// Fprintf-after-OpenFile error branches in those writers can only fire
	// when the underlying file handle has been closed mid-stream; tests
	// inject a stub that returns an error on a chosen call to drive each
	// branch deterministically.
	fprintfFunc = fmt.Fprintf
)

// schemaCompiler abstracts the two fault-injectable steps of embedded JSON
// Schema compilation (parse + register). compileEmbeddedSchema's two
// defensive branches are unreachable from production input — the embedded
// *.schema.json blobs are valid JSON checked in at build time, and with
// jsonschema/v6 v6.0.0 AddResource never errors for the constant schema URL.
// Injecting this collaborator (interface-DI) lets tests prove those branches
// with a fake instead of a package-level func-var seam (the project's
// preferred test-seam shape — see plan seam-interface-di-migration).
type schemaCompiler interface {
	Unmarshal(data []byte, v any) error
	AddResource(c *jsonschema.Compiler, url string, doc any) error
}

// stdSchemaCompiler is the production schemaCompiler backed by encoding/json
// and jsonschema/v6. It is what every compiled<Name>Schema() passes.
type stdSchemaCompiler struct{}

func (stdSchemaCompiler) Unmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }

func (stdSchemaCompiler) AddResource(c *jsonschema.Compiler, url string, doc any) error {
	return c.AddResource(url, doc)
}

// compileEmbeddedSchema is the shared compile-and-register body for every
// embedded JSON Schema in this package. It replaces three byte-identical
// compiled<Name>Schema() once-bodies (composition over duplication): each
// compiled<Name>Schema() passes its own embedded bytes + schema URL constant
// plus the schemaCompiler to use, and is otherwise a thin sync.Once wrapper.
// Behaviour is identical to the prior inlined logic — same parse, same
// AddResource, same Compile, same wrapped error messages keyed off the
// caller-supplied schemaName.
func compileEmbeddedSchema(sc schemaCompiler, schemaJSON []byte, schemaURL, schemaName string) (*jsonschema.Schema, error) {
	var doc any
	if err := sc.Unmarshal(schemaJSON, &doc); err != nil {
		return nil, fmt.Errorf("parse embedded %s schema: %w", schemaName, err)
	}
	c := jsonschema.NewCompiler()
	if err := sc.AddResource(c, schemaURL, doc); err != nil {
		return nil, fmt.Errorf("register %s schema: %w", schemaName, err)
	}
	return c.Compile(schemaURL)
}
