# Delegation: gcc2-fix — Windows SQLite single-conn deadlock (architectural re-decide)

- plan/task: graphstore-concurrency-contract / gcc2-path-a-impl (fix pass 3)
- worktree: existing `/Users/nikashp/Documents/dot-agents/.claude/worktrees/gcc2`
  (branch `gcc2-path-a`, PR #34). `git fetch origin` then
  `git pull --ff-only origin gcc2-path-a` first (currently at 67fafe76).
- mode: bypassPermissions
- status: delegated (re-authored 2026-05-18 after TWO non-converging
  Windows patch passes — orchestrator architectural re-decision below)

## Why the prior two fixes failed (root cause — read this)

Two passes (read-path guard; watchdog + loadEdgeAdjacency defer-Close)
both treated symptoms. CI #34 @67fafe76 (run 26067775596): ubuntu/macos
GREEN, **windows-latest FAILURE**, coverage-gate SKIPPED.

Real failure: `goroutine 27 [select, 4 minutes]` hung in
`SQLiteStore.UpsertEdge` (sqlite.go:154) →
`modernc.org/sqlite (*conn).QueryContext → … → _sqlite3Step →
_sqlite3VdbeExec` during gcc2's own
`TestSQLiteImpactRadiusBoundsAreClampedAndUniform`.

**Root cause (high confidence):** modernc's `_sqlite3Step` is a
**non-preemptible translated-C VM loop**; on Windows a ctx deadline or
a watchdog force-Close from another goroutine CANNOT interrupt an
in-progress step. Combined with `db.SetMaxOpenConns(1)` (sqlite.go:32),
ANY single wedged/slow step holds the only connection and the next op
(here UpsertEdge) can never acquire it → whole-store deadlock. The
single-conn cap is the disease; the watchdog has nowhere to fail over.

## Decision (orchestrator, gcc2 brain — NOT a maintainer escalation)

**OPTION 1: relax the SQLite Go-side connection-pool cap so a
wedged/abandoned conn cannot deadlock the store.**

Why this does NOT change decision-C / Path-A semantics (I checked the
spec + CONTRACT.md before deciding — do not re-litigate this):

- The contract's *guarantees* are: enforced bounds, enforced request
  timeout, a **single-goroutine** handle, and **cross-process writer
  serialization**. CONTRACT.md L104-108 explicitly assigns
  connection-pool / write-serialization mechanism to *"the
  **provider's**"* discretion.
- Cross-process write serialization is delivered by **SQLite WAL +
  `busy_timeout=5000` at the file/OS level**, NOT by the Go
  `*sql.DB` pool cap. `SetMaxOpenConns(1)` is the *current incidental
  implementation* of "low write concurrency" (spec L12-14, CONTRACT.md
  L129), not a contract clause. modernc/SQLite supports multiple
  independent connections fine (file locking + WAL).
- Therefore changing the provider-internal pool cap while preserving
  the documented observable behavior (WAL, busy_timeout=5000,
  low-write-concurrency guidance, bounds, timeout) is squarely within
  gcc2's Path-A *implementation* mandate. The contract was designed to
  let provider internals change without escalation. **Do NOT change
  CONTRACT.md's guarantees; do NOT alter the documented cross-process
  serialization story.**

### What to do

1. **Remove the `SetMaxOpenConns(1)` whole-store chokepoint.** Use a
   small bounded pool (e.g. `SetMaxOpenConns(N)` with a sane small N,
   or the default) PLUS `SetMaxIdleConns`/`SetConnMaxLifetime` tuned so
   conns are cheap and short-lived (Path A = ephemeral + cheap). Keep
   WAL + `busy_timeout=5000` PRAGMAs exactly as-is — those, not the Go
   cap, deliver the documented cross-process write-serialization.
2. **Make the SQLite request timeout an "abandon-and-fail" not an
   "interrupt-mid-step".** Since modernc's step is non-interruptible on
   Windows: on timeout, the request returns a timeout error and the
   wedged conn is abandoned/closed-out-of-band; the orphaned modernc
   step runs to completion on its now-abandoned conn and does NOT block
   the next op (this is ONLY safe because the pool is no longer capped
   at 1). Preserve the bounded-result enforcement (the hard node/depth
   cap) — that part already works. The timeout *guarantee* (caller sees
   a deadline-bounded error) is preserved; only its mechanism for
   SQLite changes from "cancel the step" to "abandon the conn + fail".
   Document this SQLite-specific timeout mechanism in a code comment
   (not CONTRACT.md — the contract guarantee is unchanged).
3. Audit every SQLite `Query/QueryContext/Exec/ExecContext` for
   deterministic conn release (`defer rows.Close()` immediately after
   the err check) so an early return cannot strand a conn even in the
   relaxed pool.
4. Keep `requestContext(nil)` intact (nil→Background, parent wired by
   gcc3 — correct, do not "fix").

If, while implementing, you find that relaxing the pool DOES observably
change cross-process write serialization (e.g. WAL+busy_timeout alone
no longer serialise writers as the contract documents), STOP and
report — that would be a contract-semantic change requiring escalation.
You are NOT expected to hit that; the analysis says WAL+busy_timeout is
the real serialization mechanism.

## Issue B (still DEFERRED to gcc4 — do NOT touch postgres.go)

postgres.go's ~20 raw `context.Background()` sites / timeout
non-uniformity remain a tracked gcc4 obligation (recorded in
`gcc4-regression-close-od1` notes). Do NOT fix here. Keep the
"Known gap (deferred to gcc4)" paragraph in the PR #34 body.

## Scope (strict)

- Write scope: `internal/graphstore/` ONLY — `sqlite.go` + its
  source-mirroring test file. No `postgres.go`, no `commands/`, no
  `CONTRACT.md` change, no allowlist, no VERSION, no `.agents/**`.
- No new `scripts/coverage-exceptions.txt` entry — the regression test
  must genuinely cover the changed paths (test the seam, never game
  the allowlist).

## Regression test (must prove the disease is cured)

Add/extend a sqlite-mirroring test that, on the modernc path, proves:
the store does NOT deadlock when one operation is slow/wedged while
another op runs — i.e. a second SQLite operation completes (or the
request returns a timeout error) while a first long/stuck operation is
outstanding, under the relaxed pool. Make it fast and deterministic
(inject a slow/blocking step via the package's sanctioned seam pattern
— do NOT rely on a real multi-minute wait, and do NOT write a test
that itself hangs CI). The test must fail on the old
`SetMaxOpenConns(1)` code and pass after the fix; state how you
confirmed both directions.

## Standing policies (non-negotiable)

- Never merge. Push to `gcc2-path-a` (updates PR #34); user gates.
- 0 Sonar issues at PR end (project `NikashPrakash_dot-agents`, by
  pullRequestId for #34).
- Trust in-worktree `go build ./...` + CI over gopls worktree noise.

## Verify

- `go build ./...` clean; `go test ./internal/graphstore/ -count=1`
  green (pre-existing CRG-python env failure acceptable);
  `go vet ./internal/graphstore/` clean; `gofmt -l .` empty.
- Push; confirm PR #34 `Test on windows-latest` goes GREEN and the
  previously-SKIPPED merged-OS coverage gate now actually RUNS
  (the orchestrator gates on its result next).

## Closeout

- Commit on `gcc2-path-a`, push `origin gcc2-path-a` (updates PR #34).
  Update PR body: the architectural root cause (single-conn cap +
  non-preemptible modernc step), the pool-relaxation fix, the
  SQLite-specific abandon-and-fail timeout mechanism, why the contract
  guarantee is unchanged, the regression test, and keep the
  Issue-B/gcc4 deferral paragraph. Do NOT merge.
- Final message: root cause, exact fix (pool settings chosen + why),
  timeout-mechanism change for SQLite, regression-test evidence (both
  directions), Sonar status, and explicitly confirm whether you hit
  any cross-process-serialization contract pressure (you should not).
