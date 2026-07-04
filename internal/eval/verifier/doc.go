// Package verifier defines the language-agnostic verifier contract for the R4
// eval harness and houses the shared run engine every per-language verifier
// reuses. It holds the shared types — [Verifier], [VerifyResult], [VerifyError],
// and [Phase] with its constants — plus [BaseVerifier], the generic engine that
// runs a TaskSpec's verification commands.
//
// # Seam and interface
//
// [Verifier] is the interface the R4 harness driver binds to; concrete
// per-language adapters (the goverifier package under golang/, plus the
// sibling verifier-python and verifier-typescript packages) implement it
// without importing one another. Each adapter embeds [BaseVerifier] and
// contributes only its Language() identity via [NewBase], so the run loop lives
// here once instead of being duplicated per language.
//
// # Command execution model
//
// [BaseVerifier.Verify] runs build_cmd first when present; a non-zero build exit
// short-circuits the test step so the harness does not spin up a test run
// against unbuilt code. test_cmd always runs if the build passes. The
// [eval.Verification.TimeoutSeconds] field, when non-zero, is applied as a
// context deadline over the combined build + test wall time. Both commands run
// in the sandbox workdir with the sandbox environment appended to the host
// environment so the agent's HOME and USERPROFILE are pinned to the scratch
// directory provisioned by the sandbox.
//
// # Result and error model
//
// A non-zero exit code is NOT returned as an error; it is encoded in
// [VerifyResult.Passed] and [VerifyResult.ExitCode] so the scoring bridge can
// record failure outcomes rather than treating them as harness faults. A
// [VerifyError] is returned only when a step could not start at all (context
// cancelled, binary not found, OS-level failure); it carries the [Phase] that
// failed and unwraps to the underlying cause.
//
// # Testability
//
// [BaseVerifier] holds a runCmd seam (unexported function-variable field) so
// tests can inject a deterministic command runner without invoking a real
// toolchain. Integration tests for the runProcess runner exercise the real
// exec path using a test-helper subprocess pattern.
package verifier
