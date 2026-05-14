package workflow

import "os"

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
)
