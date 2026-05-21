# Delegation: cg6b B2 — commands/workflow schema files to >=95%

- plan/task: coverage-gate-per-file / cg6b-ratchet-loop (iteration B2)
- worktree: `/Users/nikashp/Documents/dot-agents/.claude/worktrees/cg6b-b2`
  (branch `cg6b-b2-workflow-schema` off origin/master `f38912ff`; per-file
  gate is ENFORCE)
- mode: bypassPermissions
- status: delegated (re-authored 2026-05-18 after worker scope-conflict
  finding — orchestrator decision: OPTION 1+2, seam + dedupe)

## Goal

Raise these 4 `commands/workflow/` files to **>=95% Go statement
coverage** with real, behavior-asserting tests, then DELETE all 4 of
their allowlist entries (the ratchet tightening):

- `commands/workflow/iter_log_schema.go`              (75.00%)
- `commands/workflow/iter_log.go`                     (94.27%)
- `commands/workflow/review_decision_schema.go`       (88.52%)
- `commands/workflow/verification_result_schema.go`   (88.71%)

## Decided approach (NOT optional — this resolves the scope conflict)

A prior worker correctly found: all 4 files share an identical
structurally-DEAD branch — `if err := c.AddResource(schemaURL, doc);
err != nil { ...CompiledErr=...; return }`. With jsonschema/v6 v6.0.0,
`AddResource` never errors for the hardcoded constant `schemaURL`
(only `Compile` validates, and it panics rather than returning err).
`iter_log_schema.go` therefore CANNOT reach 95% test-only (the dead
2-stmt branch caps it at 22/24 = 91.67%).

Standing policy forbids riding/adding an allowlist grace and mandates
the narrow sanctioned seam for genuinely-unreachable defensive lines
(memory `prefer-test-seam-over-untestable`). So:

**OPTION 1+2 — minimal production seam + dedupe (decided):**

1. **Seam (option 1):** add a minimal, behavior-preserving
   compiler/schema-resource seam to the package's *existing sanctioned
   seam file* `commands/workflow/seams.go` (it already exists for
   exactly this fault-injection purpose — follow its in-file doctrine
   and comment style; add the seam var(s), do not invent a new file).
   The seam must let a test fault-inject the `AddResource` path so the
   defensive `CompiledErr` branch is genuinely exercised, with NO
   behavior change on the real path (real path still uses the live
   `AddResource`).
2. **Dedupe (option 2):** the 3 schema files contain identical
   `compiled*Schema()` once-bodies (same dead branch triplicated).
   Collapse the shared compile-and-add-resource logic into ONE shared
   helper (e.g. in `seams.go` or a small same-package helper) so the
   defensive branch exists and is seam-tested ONCE, not 3×. Each
   `compiled*Schema()` becomes a thin call into the shared helper with
   its own schema bytes + URL constant. Behavior identical; net surface
   shrinks; the triplication smell is eliminated.

This is the maintainer's consistent composition-over-duplication
direction and is contained entirely within this package's sanctioned
seam mechanism — no contract/package-boundary impact.

## Scope (now includes a bounded production seam — strict)

- Write scope:
  - `commands/workflow/seams.go` — add the schema/compiler seam +
    (option 2) the shared compile helper. Minimal, behavior-preserving.
  - `commands/workflow/iter_log_schema.go`,
    `review_decision_schema.go`, `verification_result_schema.go` —
    refactor `compiled*Schema()` to call the shared helper (NO
    behavior change; this is the dedupe).
  - `commands/workflow/iter_log.go` — test-only target; cover its +3
    reachable lines via the package's existing seams.go pattern
    (no production change expected; if a minimal seam is unavoidable,
    same sanctioned `seams.go` rule applies — flag it).
  - The 4 source-mirroring test files (`iter_log_schema_test.go`,
    `iter_log_test.go`, `review_decision_schema_test.go`,
    `verification_result_schema_test.go`) + `seams_test.go` if the new
    seam needs its own direct coverage.
  - Remove exactly the 4 matching lines from
    `scripts/coverage-exceptions.txt`.
- Touch NOTHING else: no other allowlist lines, no VERSION, no
  `.agents/**`, no `internal/graphstore/**` (a parallel stream owns
  that — fenced even if tempting), no other production files.
- Test files: mirror source, NO grab-bag / `_extra` / numbered names.
  Extend an existing mirror file rather than creating a numbered
  sibling.

## Standing policies (non-negotiable)

- **Test the real seam, never game the allowlist.** The new seam must
  be exercised by a real fault-injecting test that asserts the
  `CompiledErr` outcome; the real (non-injected) path must keep using
  live `AddResource`. Do NOT add any new coverage-exceptions entry.
- **Behavior-preserving only.** The dedupe and seam must not change
  any compiled-schema behavior or any error message/shape. If you find
  the dead branch is actually reachable, or the contract/jsonschema
  version makes a clean seam impossible, STOP and report — do not
  pad or allowlist.
- **Never merge.** Push + open PR only; user gates every merge.
- **0 Sonar issues** at PR end (project `NikashPrakash_dot-agents`,
  by pullRequestId). Clean tests + the dedupe should *reduce* smells;
  expect a bounce on any new issue.

## How

1. Measure baseline:
   `go test ./commands/workflow/ -coverprofile=/tmp/b2.cov -count=1 &&
   go tool cover -func=/tmp/b2.cov | grep -E
   'iter_log_schema\.go|iter_log\.go|review_decision_schema\.go|verification_result_schema\.go'`.
2. Implement option 1+2: add the sanctioned `seams.go` seam + shared
   compile helper; refactor the 3 schema files to the helper (no
   behavior change); add the fault-injecting test that drives the
   defensive branch through the seam.
3. Cover `iter_log.go`'s reachable +3 via the existing seam pattern.
4. Re-measure until all four are >=95.0% statements; delete the 4
   allowlist lines.
5. Local gate check:
   `COVERAGE_FILE=/tmp/b2.cov COVERAGE_PKG_MODE=off
   COVERAGE_FILE_MODE=warn bash scripts/coverage-gate.sh` — confirm
   the four are not FAIL and STALE-ALLOWLIST confirms the pruned
   entries.

## Verify

- `go build ./...` clean; `go test ./commands/workflow/ -count=1`
  green; `gofmt -l .` empty; `go vet ./commands/workflow/` clean.
- `git status --porcelain` shows ONLY: `commands/workflow/seams.go`,
  the 3 schema `.go` files, `iter_log.go` (only if a flagged seam was
  needed), the 4+1 test files, and `scripts/coverage-exceptions.txt`.
- Diff-confirm the schema-file changes are pure refactor (call into
  shared helper) with zero behavior delta.

## Closeout

- Commit on `cg6b-b2-workflow-schema`, push
  `origin cg6b-b2-workflow-schema`, open a PR to master titled
  `test(workflow): cg6b B2 — commands/workflow schema files to >=95%
  (seam + dedupe ratchet)`. Body: before/after % per file; the
  sanctioned `seams.go` seam added + why (dead AddResource branch);
  the 3-way dedupe (composition-over-duplication, behavior-preserving);
  the 4 allowlist entries deleted; verification commands run. Do NOT
  merge (user gates).
- Final message: before/after coverage per file, the seam + dedupe
  shape, proof the schema refactor is behavior-preserving, Sonar
  status, any surprise. Then STOP — do not start B3.
