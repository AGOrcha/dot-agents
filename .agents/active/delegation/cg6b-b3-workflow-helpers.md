# Delegation: cg6b B3 — commands/workflow helpers to >=95% + prefs.go verify-or-raise

- plan/task: coverage-gate-per-file / cg6b-ratchet-loop (iteration B3)
- worktree: create `/Users/nikashp/Documents/dot-agents/.claude/worktrees/cg6b-b3`
  on branch `cg6b-b3-workflow-helpers` off **origin/master** (the spawn
  hands: `git fetch origin` first, branch off origin/master AFTER PR #35
  is merged — this bundle spawns ONLY post-#35-merge per
  one-PR-in-flight). Per-file gate is ENFORCE.
- mode: bypassPermissions
- status: authored, spawn-gated on #35 merge

## Goal

Raise these 4 `commands/workflow/` allowlisted files to **>=95% Go
statement coverage** with real, behavior-asserting tests, then DELETE
their allowlist entries:

- `commands/workflow/fs.go`      (93.27%)
- `commands/workflow/drift.go`   (94.30%)
- `commands/workflow/health.go`  (94.67%)
- `commands/workflow/graph.go`   (93.28%)

Cohesive group (workflow filesystem / drift / health / graph helpers,
one package, same `seams.go` + `testutil_test.go` patterns proven in
B2's PR #35).

## Plus: prefs.go verify-or-raise (folded in by orchestrator)

`commands/workflow/prefs.go` is **NOT in the allowlist** (pre-existing,
unlisted). B2's worker observed it at 94.53% on a single-OS local gate
(FILE_MODE=warn) but master CI (merged multi-OS enforce) is green — the
usual single-OS-undercount divergence, NOT a regression.

Action (in THIS batch, since prefs.go is in the same package):
1. Measure prefs.go on the **merged multi-OS profile** (or, if you
   cannot reproduce the merged profile locally, run the package
   coverage and reason explicitly about the OS-tagged lines).
2. If prefs.go is **>=95% on the merged profile**: no code change —
   state the measured number and that it is a single-OS-undercount
   artifact, no allowlist entry added (it stays correctly unlisted).
3. If prefs.go is **genuinely <95% on the merged profile**: raise it
   to >=95% with real behavior-asserting tests in
   `prefs_test.go` (source-mirroring). Do NOT add an allowlist entry
   either way (standing policy: never add a coverage-exceptions entry
   for testable code).

## Scope (strict)

- Write scope:
  - `commands/workflow/` TEST files for the 4 targets
    (`fs_test.go`, `drift_test.go`, `health_test.go`, `graph_test.go`;
    extend existing mirror files, do NOT create numbered siblings).
  - `commands/workflow/prefs_test.go` ONLY if step 3 applies.
  - `commands/workflow/seams.go` ONLY if a target has a genuinely
    unreachable defensive branch needing the sanctioned seam pattern
    (B2 added `compileEmbeddedSchema` there — same doctrine: minimal,
    behavior-preserving, real fault-injecting test; NEVER an allowlist
    grace). If you add a seam, also cover it in `seams_test.go`.
  - Remove EXACTLY the 4 matching lines from
    `scripts/coverage-exceptions.txt` (fs.go, drift.go, health.go,
    graph.go). Do not touch any other allowlist line.
- Touch NOTHING else: no VERSION, no `.agents/**`, no other production
  files, and **no `internal/graphstore/**`** — that is FENCED (a
  parallel stream owns gcc2/gcc4; its 3 allowlist entries are not a
  cg6b target until gcc2->gcc3 complete).

## Standing policies (non-negotiable)

- Test the real seam, never game the allowlist. Cover the genuine
  uncovered branches with outcome-asserting tests; no new
  coverage-exceptions entry; do not ride a pre-existing entry.
- Behavior-preserving only. If you must change production code beyond
  an additive sanctioned seam, STOP and report (a bug found = call it
  out explicitly, do not silently fix-and-pad).
- Never merge. Push + open PR; user gates every merge.
- 0 Sonar issues at PR end (project `NikashPrakash_dot-agents`, by
  pullRequestId). Match B2's clean result.

## How

1. `go test ./commands/workflow/ -coverprofile=/tmp/b3.cov -count=1 &&
   go tool cover -func=/tmp/b3.cov | grep -E
   'fs\.go|drift\.go|health\.go|graph\.go|prefs\.go'`.
2. Write behavior-asserting tests for the uncovered paths (match B2's
   seams.go / testutil_test.go style).
3. Re-measure until the 4 targets are >=95.0%; resolve prefs.go per
   the verify-or-raise steps above.
4. Delete the 4 allowlist lines. Local gate check:
   `COVERAGE_FILE=/tmp/b3.cov COVERAGE_PKG_MODE=off
   COVERAGE_FILE_MODE=warn bash scripts/coverage-gate.sh` — confirm the
   4 are not FAIL and STALE-ALLOWLIST confirms the pruned entries.

## Verify

- `go build ./...` clean; `go test ./commands/workflow/ -count=1`
  green; `gofmt -l .` empty; `go vet ./commands/workflow/` clean.
- `git status --porcelain` shows ONLY the allowed paths above.

## Closeout

- Commit on `cg6b-b3-workflow-helpers`, push, open PR to master titled
  `test(workflow): cg6b B3 — workflow helpers to >=95% + prefs verify
  (ratchet)`. Body: before/after % per file, prefs.go merged-profile
  finding + action taken, any sanctioned seam added + why, the 4
  allowlist entries deleted, verification commands. Do NOT merge.
- Final message: before/after per file, prefs.go verdict, seam/dedupe
  if any, Sonar status. Then STOP — do not start B4.
