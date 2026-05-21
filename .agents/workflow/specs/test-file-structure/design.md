# Spec: Test-file structure & naming convention

## Problem

The slice-driven coverage pushes (pr3b/pr3c) accreted iteration-numbered
test files with no semantic mapping to the code they exercise:

- `commands/workflow`: 39 test files — `coverage_push{,2..10}_test.go` (11)
  + `integration_harness{,2..5}_test.go` (5) = 16/39 (41%) are grab-bags.
- `commands`: 31 test files — `ci_drift{,2,3}_test.go` (3).

`coverage_push7_test.go` conveys nothing about what it tests. A developer
cannot locate the test for a function by filename, and new tests have no
obvious home, so the pattern self-perpetuates.

This violates the existing repo convention (AGENTS.md): *"Place Go tests
next to implementation files as `*_test.go`."*

## Decision (the convention)

**A test file mirrors the source file or cohesive feature it exercises.**

- Primary rule: tests for `foo.go` live in `foo_test.go`.
- When a test spans multiple source files for one behavior (E2E /
  integration), name it for the *feature*, not an iteration number:
  `lifecycle_e2e_test.go`, `delegation_fanout_test.go` — never
  `coverage_pushN` / `integration_harnessN` / `ci_driftN`.
- Shared test helpers: `<area>_testutil_test.go` or an existing
  `testutil_test.go`, never a numbered bucket.
- Iteration/round numbers are forbidden in test filenames. Coverage is an
  outcome, not a file-organizing axis.

Rationale: filename = discoverability. Mirroring source is the Go-standard,
already-documented convention; the numbered files are pure entropy from
treating "raise coverage this round" as a filing system. Alternatives
rejected: (a) one big `package_test.go` per package — loses
co-location; (b) leave as-is — entropy compounds with every future push.

## Requirements

1. The convention is documented authoritatively (AGENTS.md testing
   section) and the recurring root cause captured as a lesson.
2. Every `Test*`/`Benchmark*`/helper currently in a grab-bag file is
   relocated to a `*_test.go` whose name maps to the source file or
   feature it covers. **Pure moves** — no test body, assertion, name, or
   `t.Run` subtest changes.
3. No iteration-numbered test filenames remain in the repo.
4. A full audit of *all* packages' test files against the convention is
   performed; any other drift found is corrected or explicitly recorded
   as an accepted exception with rationale.
5. The mapping (old grab-bag file → which Test funcs → new file) is
   recorded as a reviewable artifact before moves are executed.

## Done criteria (verifiable)

- `git ls-files '*_test.go' | grep -E '(coverage_push|integration_harness|ci_drift)[0-9]*_test\.go'`
  returns nothing on every touched branch.
- `go test ./...` passes on each touched worktree.
- The 95%-per-package coverage gate (`scripts/coverage-gate.sh`) stays
  green for every package whose test files moved (no statement
  gained/lost — moves only).
- `gofmt` clean; `go vet ./...` clean.
- AGENTS.md states the convention; a `.agents/lessons/<name>/LESSON.md`
  records the slice-coverage-push root cause.
- A test→destination mapping artifact exists and was reviewed.

## Scope

- **In:** `commands/workflow/*_test.go` (pr3b worktree),
  `commands/*_test.go` ci_drift cluster (root; lands on the branch that
  owns those files — pr3a is merged, so root `commands` ci_drift files
  are on master/pr3b's base — confirm ownership during planning),
  any `commands/kg` drift (pr3c worktree). Audit covers all packages.
- **Target branches:** the stacked open PRs — pr3b (`pr3b/workflow`) for
  workflow files, pr3c (`pr3c/kg`) for kg, restructure shipped with
  those PRs (user decision).

## Deferred / out of scope

- Root `commands` package decomposition (composition-root vs
  lifecycle-command implementation split) — separate unowned concern;
  captured in an architecture note + a `root-command-decomposition`
  draft plan. NOT this spec.
- The seam-globals→Deps DI migration — owned by `di-refactor-rollout`.
  This spec must not touch seam mechanics; if a moved test references a
  seam, it moves verbatim.
- Renaming/splitting *source* files — only test files are in scope.

## Relationship to other artifacts

- Consumes the AGENTS.md test convention; makes it enforceable.
- Sequenced before `di-refactor-rollout` touches the same packages would
  be ideal (fewer re-churns) but not a hard dependency — moves are
  independent of seam mechanics.
- Boundary finding → `root-command-decomposition` (separate plan stub).
