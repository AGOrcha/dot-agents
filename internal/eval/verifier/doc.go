// Package verifier defines the language-agnostic verifier contract for the R4
// eval harness. It holds the shared types — [Verifier], [VerifyResult],
// [VerifyError], and [Phase] with its constants — that every per-language
// verifier implements.
//
// # Seam and interface
//
// [Verifier] is the interface the R4 harness driver binds to; concrete
// per-language adapters (the goverifier package under golang/, plus the
// sibling verifier-python and verifier-typescript packages) implement it
// without importing one another. Housing the contract here lets those
// implementations mirror the [VerifyResult] shape without depending on the
// Go-specific package.
//
// # Result and error model
//
// A non-zero exit code is NOT returned as an error; it is encoded in
// [VerifyResult.Passed] and [VerifyResult.ExitCode] so the scoring bridge can
// record failure outcomes rather than treating them as harness faults. A
// [VerifyError] is returned only when a step could not start at all (context
// cancelled, binary not found, OS-level failure); it carries the [Phase] that
// failed and unwraps to the underlying cause.
package verifier
