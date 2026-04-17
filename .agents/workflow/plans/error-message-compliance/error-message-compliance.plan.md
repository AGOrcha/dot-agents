# Error Message Compliance Plan

Status: Proposed

## Problem

The CLI now has a partial shared UX for command failures in [`commands/ux.go`](../../../../commands/ux.go): `CLIError`, `ErrorWithHints`, `UsageError`, root flag-parse handling, and centralized rendering via `RenderCommandError`.

That infrastructure is valuable, but compliance is uneven:

- some commands use `UsageError` and include targeted recovery hints
- some validation paths still return raw `fmt.Errorf(...)` strings
- some errors enumerate valid values cleanly while others only say "invalid"
- error rendering is human-first today, and automation cannot assume a stable machine-readable failure envelope even on commands whose success path supports `--json`

The result is the same kind of drift the repo already tracked under global flag compliance: operators get uneven guidance, and agents/scripts cannot predict whether a failure will be actionable or opaque.

## Immediate Baseline

Current shared behavior already present in [`commands/ux.go`](../../../../commands/ux.go):

- `CLIError` carries `Message`, `Hints`, `ShowUsage`, and `Cause`
- `ErrorWithHints(...)` creates actionable errors without usage
- `UsageError(...)` creates actionable errors that also render command usage
- root flag parse failures are wrapped through `SetFlagErrorFunc(...)`
- `RenderCommandError(...)` centralizes formatting with a primary message, optional hint list, and optional usage block

This plan is not introducing the pattern from scratch. It is documenting and expanding it into a repo-wide contract.

## Follow-Up Scope

1. Inventory current failure paths across top-level and nested commands.
2. Decide which categories of failures must use `UsageError`, `ErrorWithHints`, or plain wrapped errors.
3. Normalize validation messages so constrained fields enumerate valid values explicitly.
4. Add regression tests for representative error classes.
5. Document what automation can rely on today, especially where `--json` success paths still have human-only failure paths.

## Initial Contract Areas

- argument-count and flag-shape failures
- enum / constrained-value validation errors
- recovery-heavy environment errors (missing `.agents/`, uninitialized machine, unknown project, missing source)
- command-family-specific "do this next" hints
- root parse errors and unknown command handling
- automation-facing note: error rendering is still human-first unless an explicit machine-readable error contract is added later

## Exit Criteria

- every major command family has a deliberate error-message contract
- validation errors list valid values where the domain is finite
- recoverable failures include next-step hints instead of only raw causes
- usage is shown only for genuine usage errors, not every runtime failure
- regression tests lock the intended behavior

## Inventory Seed (2026-04-17)

**Canonical contract (prose):** [`docs/ERROR_MESSAGE_CONTRACT.md`](../../../../docs/ERROR_MESSAGE_CONTRACT.md)

Observed shared patterns:

- `ConfigureRootCommandUX(...)` wraps flag parse errors into `CLIError{ShowUsage: true}`
- positional arg helpers (`ExactArgsWithHints`, `NoArgsWithHints`, `MaximumNArgsWithHints`, `RangeArgsWithHints`) already produce command-scoped usage errors
- `enrichCLIError(...)` adds targeted hints for common workflow / install / preference failures
- `RenderCommandError(...)` always renders human-facing text with hints and optional usage; it does not emit JSON error envelopes

Known drift / likely targets:

- command handlers that still return plain `fmt.Errorf("invalid ...")` without valid-value guidance
- runtime failures that are recoverable but do not include next-step hints
- inconsistent usage decisions across validation vs execution failures
- automation-facing commands whose success path is structured but whose failure path remains prose-only

## Next Task

Use this plan and [`docs/ERROR_MESSAGE_CONTRACT.md`](../../../../docs/ERROR_MESSAGE_CONTRACT.md) to audit command families, decide the contract per error class, then implement only the mismatches with focused regression coverage.
