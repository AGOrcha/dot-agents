# Lesson: test files must mirror source, never iteration-numbered

## Pattern observed

The pr3b/pr3c coverage-push slices produced 15 grab-bag test files in
`commands/workflow` (`coverage_push{,2..10}_test.go`,
`integration_harness{,2..5}_test.go` — 610 test funcs / 41% of the
package's test files) plus `commands/ci_drift{,2,3}_test.go`. Filenames
encoded *which coverage round wrote them*, not *what they test*. Result:
a function's test is unfindable by filename, and every new coverage push
spawned another numbered file — the anti-pattern self-perpetuated.

## Root cause

Treating "raise coverage this iteration" as a filing system. Each
slice/round created `coverage_pushN_test.go` as a scratch bucket for
"whatever was still uncovered," because there was no enforced rule that a
test's home is determined by the code it exercises.

## Rule (prevention)

- A test file mirrors the **source file or cohesive feature** it
  exercises. `foo.go` → `foo_test.go`; cross-file behavior →
  `<feature>_test.go` (e.g. `lifecycle_e2e_test.go`).
- **Iteration/round numbers in test filenames are forbidden.** No
  `coverage_pushN`, `integration_harnessN`, `ci_driftN`, or any
  `<x>N_test.go` bucket.
- A new test goes in the source-mirroring file; if it does not exist,
  create it by source name. Never open a numbered grab-bag.
- Coverage is an *outcome*. When a coverage push needs a test for an
  uncovered branch in `bar.go`, it goes in `bar_test.go`.

Encoded authoritatively in `~/.agents/rules/dot-agents/agents.md`
(Testing Guidelines). Remediation tracked by the `test-file-structure`
canonical plan.

## How to apply

When asked to "raise coverage" or running a coverage slice: for each
uncovered symbol, add its test to the file mirroring the symbol's source
file. If you catch yourself naming a file with a number or "push"/"round"
/"harness2", stop — that is this lesson firing.
