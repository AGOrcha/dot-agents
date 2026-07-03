# session-handoff-journal — implementation results

**Completed:** 2026-06-29. All 11 tasks (p0, p1, p2, p3a, p3b, p4, p5, p6, p7, p8, p9)
merged to master with status=completed. This is the cross-task narrative the per-task
PRs do not capture (the plan was implemented via direct-dispatch workers producing PRs,
not `merge-back.md` artifacts, so this is the canonical closeout per the workflow rules).

## What shipped

The append-only, crash-survivable session-handoff journal — the episodic recovery view —
end to end:

| Task | PR | What |
|---|---|---|
| p0 interprocess lock | #205 | `agentslock.AcquireFileLock` exported; mkdir-as-lock with stale-TTL reclaim |
| p1 journal core | #206 | `internal/journal`: Envelope, schema registry, identity fingerprint, atomic appender, 16KiB cap |
| p2 command schemas | #208 | typed Input/Observed, `FieldDelta` (sha256+len, no bodies), `Locus` sum-type, `NewEvent` schema-typed |
| p3a emit workflow | #210 | deferred Tier-1 tail, `ok` flipped after the mutation lands, best-effort non-fatal |
| p3b emit kg/review | #212 | mirrors p3a for kg/review; journals only changes that actually landed |
| p4 snapshot | #209 | deterministic `SnapshotState` (sorted), `PendingUnblocked` projection |
| p5 recovery view | #211 | replay + re-verify vs reality, verified/changed/missing/unverified tags, collision quarantine |
| p6 command surface | #216 | `da workflow journal {snapshot,recover,show,prune,append}`; gh/git verification sources |
| p7 recovery hooks | #218 | PreCompact snapshot + SessionStart(compact) recover hook bundles, best-effort non-fatal |
| p8 agent-handoff skill | #217 | D6 cadence + verified readback via `journal recover` (trust gradient, quarantine) |
| p9 e2e test | #220 | `tests/test-session-handoff-journal.sh` — 12 assertions across the 5 Done criteria, hermetic |

## What the per-task records miss (cross-task narrative)

### 1. The cross-brain gate caught real bugs on every layer that green tests missed
Each impl PR went through an adversarial `codex exec` review against a fresh worktree at
origin HEAD before `--merge --admin`. The gate was not a rubber stamp — it found a real
defect on essentially every task: p2's FieldDelta value-receiver marshal bug, p5's
watermark/locus/collision ordering, p6's wrong-PR/coord-source handling, p7's recover hook
streaming a **partial/failed view** to the SessionStart re-injection channel (capture-then-
emit-on-success fix), p8's two cadence-doc overclaims (+ a third repeated in SKILL.md the
re-gate caught), and p9's **four false-green assertions** in the e2e itself (a `>=4` that a
double-journal would pass, a vacuous `|| true` config-exclusion, an incomplete recover
check, an overclaimed criterion-5 scope). Lesson reinforced: gate the *tests* too — a green
e2e can still assert nothing.

### 2. Closing the layer surfaced three pre-existing Windows/concurrency bugs
p7's CI run revealed **master had been red on `windows-latest` since p3b/p5** — caught only
because p7 was the first task whose CI someone actually watched to completion (windows is the
slowest job; macOS/ubuntu green minutes earlier). Three distinct concurrency defects in the
shared lock/fs primitives, each fixed and gated SOUND:
- **#219** — kg fault-injection seams matched only forward-slash paths (never fired on
  Windows backslash paths → `TestRunKGReweave_Partial…` false pass); + agentslock
  **acquire**-side delete-pending race (`ERROR_ACCESS_DENIED` ≠ `ALREADY_EXISTS`).
- **#221** — `fsops.RemoveAll` Windows **release**-side transient sharing-violation →
  bounded native-remove retry before the powershell fallback.

### 3. One race remains OPEN (deliberately not patched autonomously)
`TestEmitConcurrentNoTornLines` still flakes at a low rate on **all** platforms on the lock
**release** (rmdir ENOTEMPTY / sharing-violation) — a reclaim-vs-release lifecycle race.
**Data integrity is intact** (no torn lines; only lock-dir cleanup races). A correct fix
touches `lockIsStale`/reclaim/release ordering with mutual-exclusion and liveness in tension,
so it was captured for a careful fix rather than patched on a heartbeat tick. See
`.agents/active/fold-back/journal-lock-release-race.md`. p9 merged on a confirmed-clean
macOS re-run (its shell-test-only change is orthogonal to the flake).

## Process notes for the next chain
- **Direct dispatch + `workflow advance`** for a sequential single-scope chain — `fanout`'s
  stale-delegation lock blocks same-scope back-to-back tasks, and `delegation closeout`
  wants a merge-back artifact a PR flow does not produce.
- **Worktree isolation is mandatory** for code workers; non-isolated agents (and the setup
  phase of isolated ones) thrashed the main-tree branch repeatedly.
- **Check `windows-latest` specifically before merge** — see lesson
  `live-smoke-must-run-on-every-target-os` (recurrence note added this session).
- **Redirect codex gate stdin from `/dev/null`** in background runs — one gate hung ~14min
  on "Reading additional input from stdin…".

## Deferred
Plan archive (`da workflow plan archive`) is still the known no-persist issue, so the plan
was not archived; this `impl-results.md` is the durable record in its place.
