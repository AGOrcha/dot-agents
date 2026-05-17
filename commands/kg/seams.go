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
//
// Concurrency caveat: these are process-global mutable vars. They are safe
// today only because every swapping test restores via t.Cleanup AND no kg
// test calls t.Parallel(). Do NOT add t.Parallel() to a kg test that swaps
// a seam (or to one running alongside one) — parallel tests would race and
// corrupt each other's stubs. If parallelism is ever needed here, move
// these seams into a per-call/struct dependency instead of package vars.
var (
	osMkdirAll        = os.MkdirAll
	osWriteFile       = os.WriteFile
	osReadFile        = os.ReadFile
	osOpenFile        = os.OpenFile
	osRename          = os.Rename
	osReadDir         = os.ReadDir
	jsonMarshalIndent = json.MarshalIndent
)
