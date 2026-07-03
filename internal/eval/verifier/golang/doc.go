// Package goverifier implements the Go-language eval verifier for the R4
// eval harness (R4 spec task t-verifier-iface). It runs the build_cmd and
// test_cmd from an [eval.TaskSpec] inside a sandbox working directory,
// captures pass/fail + stdout/stderr + elapsed duration, and returns a typed
// [VerifyResult].
//
// # Seam and interface
//
// [Verifier] is the language-agnostic interface the R4 harness driver binds
// to; [GoVerifier] is the Go adapter. The sibling verifier-python and
// verifier-typescript packages mirror this shape — sequenced after this
// package so the [VerifyResult] shape stabilizes first.
//
// # Command execution model
//
// build_cmd runs first when present; a non-zero build exit short-circuits
// the test step so the harness does not spin up a test run against unbuilt
// code. test_cmd always runs if the build passes. The
// [eval.Verification.TimeoutSeconds] field, when non-zero, is applied as a
// context deadline over the combined build + test wall time. Both commands
// run in the sandbox workdir with the sandbox environment appended to the
// host environment so the agent's HOME and USERPROFILE are pinned to the
// scratch directory provisioned by the sandbox.
//
// A non-zero exit code is NOT returned as an error; it is encoded in
// [VerifyResult.Passed] and [VerifyResult.ExitCode] so the scoring bridge
// can record failure outcomes rather than treating them as harness faults
// (R4 spec done-criterion 8: "a failed run still emits a score sidecar").
// A [VerifyError] is returned only when a step could not start at all
// (context cancelled, binary not found, OS-level failure).
//
// # Testability
//
// [GoVerifier] holds a runCmd seam (unexported function-variable field) so
// tests can inject a deterministic command runner without invoking the real
// Go toolchain. Integration tests for [runProcess] exercise the real exec
// path using a test-helper subprocess pattern.
package goverifier
