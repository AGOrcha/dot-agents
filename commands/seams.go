package commands

import "os"

// IO seams. Tests in this package swap these to error-injecting stubs to
// cover the error-return branches that cannot be triggered via filesystem
// fixtures (e.g. MkdirAll on a writable tmp dir always succeeds, executable
// resolution always succeeds in `go test`).
//
// Each seam mirrors a real `os.<Name>` function used in this package. Add a
// seam only when a test needs to fault-inject the corresponding call.
var (
	osMkdirAll   = os.MkdirAll
	osWriteFile  = os.WriteFile
	osRemove     = os.Remove
	osExecutable = os.Executable
	osGetwd      = os.Getwd
	osSymlink    = os.Symlink
)
