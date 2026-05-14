package kg

import (
	"encoding/json"
	"os"
)

// IO seams. Tests in this package swap these to error-injecting stubs to
// cover error-return branches that cannot be triggered via filesystem
// fixtures alone (e.g. MkdirAll on a writable tmp dir always succeeds, and
// json.MarshalIndent only fails on unmarshalable types — not the types we
// pass at runtime).
//
// Each seam mirrors a real `os.<Name>` or `json.<Name>` function used in
// this package. Add a seam only when a test needs to fault-inject the
// corresponding call.
var (
	osMkdirAll        = os.MkdirAll
	osWriteFile       = os.WriteFile
	osReadFile        = os.ReadFile
	osOpenFile        = os.OpenFile
	osRename          = os.Rename
	osReadDir         = os.ReadDir
	jsonMarshalIndent = json.MarshalIndent
)
