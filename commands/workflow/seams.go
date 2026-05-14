package workflow

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

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
