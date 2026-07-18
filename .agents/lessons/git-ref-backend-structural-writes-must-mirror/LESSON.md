# Lesson: git-ref backend must mirror ALL canonical writes, not just transitions

**Date:** 2026-07-18
**Surfaced by:** dogfooding the `work_tracking.backend=git-ref` cutover (`da workflow task add` clobber).

## Pattern

`backend=git-ref` makes `da workflow` READ canonical `TASKS.yaml`/`PLAN.yaml` from
`refs/agents/state`. But the ref mirror (`mirrorTransitionToStateRef`) fires only on
STATUS TRANSITIONS (`runWorkflowAdvance`). Structural writes — `task add`, `task update`,
plan create, merge-back status edits — go through `saveCanonicalTasks`/`saveCanonicalPlan`
and write ONLY the working copy; they do **not** mirror to the ref.

So under `backend=git-ref` the ref goes stale for any non-transition write, and a
read→modify→write structural op re-reads the stale ref and **clobbers** a prior write.
Observed live: two sequential `da workflow task add` calls each read the ref (7 tasks),
appended their own task, and wrote the working copy — the second lost the first
(working copy 8, ref 7, reads diverged). Same read-your-writes class as the reverted
`read_from=master` footgun.

## Rule

- The git-ref mirror MUST trigger at the canonical-write CHOKE POINT
  (`saveCanonicalTasks` / `saveCanonicalPlan`), so EVERY canonical write (add / update /
  plan-create / advance / merge-back) is immediately ref-visible — not patched per command.
- Do NOT cut over to `backend=git-ref` reads until all canonical writes mirror.
- The cutover validation MUST exercise STRUCTURAL writes (add/update) under git-ref, not
  only status-transition reads. A projection-compare of already-transitioned state does
  not catch this — the projection looks faithful precisely because transitions DO mirror.
- Regression test: after ANY canonical write under `backend=git-ref`, a fresh read (which
  resolves from the ref) returns the just-written state.
