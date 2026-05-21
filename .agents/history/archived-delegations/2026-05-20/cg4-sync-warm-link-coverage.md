# Delegation: cg4 — lift sync_code_warm_link.go to >=95%

- task: coverage-gate-per-file / cg4-pr3c-comply
- worktree: `/Users/nikashp/Documents/dot-agents/.claude/worktrees/pr3c`
  (branch `pr3c-rebased` → pushes `pr3c/kg` → PR #18; rebased on new
  master, VERSION 0.3.2)
- status: delegated

## Goal

`commands/kg/sync_code_warm_link.go` is the originating coverage
offender (~61% line cov; the per-file gate's worst real file). Bring it
to **>=95% Go statement coverage** with REAL tests so #18 complies and
v0.3.2 is unblocked. It is the originating offender — **do NOT
allowlist it**; raise it.

## Scope

- Write scope ONLY: `commands/kg/` test files. Add tests to the
  source-mirroring file (`sync_code_warm_link_test.go`; create if
  absent — obey the test-file-naming convention: mirror source, NO
  grab-bag/`_extra`/numbered names).
- Do NOT change production code except to fix a genuine bug you find
  (call it out explicitly if so). Do NOT touch VERSION, CHANGELOG,
  other files, `.agents/**`.

## How

1. Measure first: `cd <worktree> && go test ./commands/kg/
   -coverprofile=/tmp/kg.cov -count=1 && go tool cover
   -func=/tmp/kg.cov | grep sync_code_warm_link` — identify the exact
   uncovered funcs/branches.
2. Write **behavior-asserting** tests for the uncovered paths (warm /
   code-graph / link sync + their error branches). No coverage padding
   that doesn't assert outcomes — the project mandate is exhaustive,
   meaningful tests. Use existing kg test helpers/fixtures
   (newTempKG, fake CRG stubs, seams) — match existing kg test style.
3. Re-measure until `sync_code_warm_link.go` >= 95.0% statements.
   Run the gate locally as a check:
   `COVERAGE_FILE=/tmp/kg.cov COVERAGE_PKG_MODE=off
   COVERAGE_FILE_MODE=warn bash scripts/coverage-gate.sh` and confirm
   sync_code_warm_link.go is no longer in the FAIL list (single-OS
   profile is fine for this local check).

## Verify

- `go build ./...` clean; `go test ./commands/kg/ -count=1` green;
  `gofmt -l` clean; `go vet ./commands/kg/` clean.
- `git status --porcelain` shows only `commands/kg/` test file(s).

## Closeout

- Commit on `pr3c-rebased`, `git push --force-with-lease origin
  pr3c-rebased:pr3c/kg` (updates PR #18). Do NOT merge.
- Final message: before/after coverage % for sync_code_warm_link.go,
  what was tested, any production bug found, anything left.
