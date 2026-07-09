// Package pyverifier is the Python-language adapter for the R4 eval verifier (R4
// spec task t-verifier-iface). It is a thin wrapper over the shared run engine:
// [PyVerifier] embeds [verifier.BaseVerifier] and supplies the eval.LanguagePython
// identity, so Language and Verify are promoted from the engine unchanged.
//
// # Python specificity
//
// The engine runs spec.Verification.BuildCmd then TestCmd, so the only Python
// specificity is Language() == python plus the generator-supplied commands: the
// Python task generator authors the build step (e.g. python -m py_compile) and
// the test step (e.g. pytest or unittest) into the TaskSpec. No Python toolchain
// logic is duplicated in this adapter.
//
// # Seam and interface
//
// [verifier.Verifier] is the language-agnostic interface the R4 harness driver
// binds to; [PyVerifier] is the Python adapter. The interface, its result types,
// and the generic build-then-test run engine ([verifier.BaseVerifier]) all live
// in the neutral internal/eval/verifier package so the sibling verifier-go and
// verifier-typescript adapters reuse the same engine without importing this
// package or duplicating the run loop.
//
// See the internal/eval/verifier package docs for the command execution,
// result, error, and testability models the engine implements.
package pyverifier
