// Package goverifier is the Go-language adapter for the R4 eval verifier (R4
// spec task t-verifier-iface). It is a thin wrapper over the shared run engine:
// [GoVerifier] embeds [verifier.BaseVerifier] and supplies the eval.LanguageGo
// identity, so Language and Verify are promoted from the engine unchanged.
//
// # Seam and interface
//
// [verifier.Verifier] is the language-agnostic interface the R4 harness driver
// binds to; [GoVerifier] is the Go adapter. The interface, its result types,
// and the generic build-then-test run engine ([verifier.BaseVerifier]) all live
// in the neutral internal/eval/verifier package so the sibling verifier-python
// and verifier-typescript adapters reuse the same engine without importing this
// package or duplicating the run loop.
//
// See the internal/eval/verifier package docs for the command execution,
// result, error, and testability models the engine implements.
package goverifier
