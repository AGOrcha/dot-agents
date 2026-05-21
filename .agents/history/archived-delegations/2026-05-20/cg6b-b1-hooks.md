# Delegation: cg6b B1 — commands/hooks to >=95%

- plan/task: coverage-gate-per-file / cg6b-ratchet-loop (iteration B1)
- worktree: `/Users/nikashp/Documents/dot-agents/.claude/worktrees/cg6b-b1`
  (branch `cg6b-b1-hooks` off latest master `ab637b35`; per-file gate is
  now ENFORCE)
- status: delegated

## Goal

Raise these 3 files to **>=95% Go statement coverage** with real,
behavior-asserting tests, then DELETE their allowlist entries (the
ratchet tightening):

- `commands/hooks/cmd.go`    (92.86%)
- `commands/hooks/remove.go` (89.74%)
- `commands/hooks/spec.go`   (93.75%)

## Scope (strict)

- Write scope: `commands/hooks/` test files ONLY + remove exactly the 3
  lines for these files from `scripts/coverage-exceptions.txt`
  (currently lines 25-27). Touch nothing else — no production code
  unless you find a genuine bug (call it out explicitly), no VERSION,
  no other files, no `.agents/**`.
- Tests go in the source-mirroring file (`cmd_test.go`,
  `remove_test.go`, `spec_test.go`; obey the test-file-naming
  convention — mirror source, NO grab-bag/`_extra`/numbered names).

## How

1. Measure: `go test ./commands/hooks/ -coverprofile=/tmp/h.cov
   -count=1 && go tool cover -func=/tmp/h.cov | grep -E
   'cmd\.go|remove\.go|spec\.go'` — identify exact uncovered branches.
2. Write behavior-asserting tests for the uncovered paths (assert
   outcomes, not coverage padding). Match existing `commands/hooks`
   test style/helpers.
3. Re-measure until all three are >=95.0% statements. Then delete their
   3 lines from `scripts/coverage-exceptions.txt`.
4. Local gate check: `COVERAGE_FILE=/tmp/h.cov COVERAGE_PKG_MODE=off
   COVERAGE_FILE_MODE=warn bash scripts/coverage-gate.sh` — confirm the
   three are not FAIL (single-OS profile fine for this local check; CI's
   merged profile is authoritative).

## Verify

- `go build ./...` clean; `go test ./commands/hooks/ -count=1` green;
  `gofmt -l` clean; `go vet ./commands/hooks/` clean.
- `git status --porcelain` shows only `commands/hooks/` test files +
  `scripts/coverage-exceptions.txt`.

## Closeout

- Commit on `cg6b-b1-hooks`, push `origin cg6b-b1-hooks`, open a PR to
  master titled `test(hooks): cg6b B1 — commands/hooks to >=95%
  (ratchet)`. Body: before/after % per file, what was tested, confirm
  the 3 allowlist entries deleted. Do NOT merge (user gates).
- Final message: before/after coverage, what was tested, any bug found.
