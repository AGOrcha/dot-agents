package workflow

import (
	"io"
	"os"
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
)
