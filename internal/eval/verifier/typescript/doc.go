// Package tsverifier is the TypeScript-language adapter for the R4 eval
// verifier (R4 spec task t-verifier-iface). It is a thin wrapper over the
// shared run engine: [TSVerifier] embeds [verifier.BaseVerifier] and supplies
// the eval.LanguageTypeScript identity, so Language and Verify are promoted from
// the engine unchanged.
//
// # Seam and interface
//
// [verifier.Verifier] is the language-agnostic interface the R4 harness driver
// binds to; [TSVerifier] is the TypeScript adapter. The interface, its result
// types, and the generic build-then-test run engine ([verifier.BaseVerifier])
// all live in the neutral internal/eval/verifier package so the sibling
// verifier-go and verifier-python adapters reuse the same engine without
// importing this package or duplicating the run loop.
//
// # TypeScript specificity
//
// There is no TS-specific run logic here. The engine runs
// spec.Verification.BuildCmd then TestCmd, so a task's TypeScript-ness is
// carried entirely by Language() == eval.LanguageTypeScript plus the commands
// the TypeScript generator authors into the TaskSpec (e.g. build tsc --noEmit,
// test node --test / vitest). See the internal/eval/verifier package docs for
// the command execution, result, error, and testability models the engine
// implements.
package tsverifier
