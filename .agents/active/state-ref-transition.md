# Workflow-state ref backend — CUTOVER COMPLETE (dot-agents)

**Status:** `git-ref` is the ACTIVE work-tracking backend for this repo as of 2026-07-18. The migration gate is satisfied and the cutover is done + validated + user-authorized.

## Active configuration

`.agentsrc.json`:

```json
"work_tracking": { "backend": "git-ref" }
```

- **Reads** — `da workflow` projects canonical `TASKS.yaml`/`PLAN.yaml` from `refs/agents/state` (per-task blobs), with graceful fallback to the working copy for any plan not yet on the ref. Reading the LOCAL ref is read-your-writes safe (transitions CAS-update it in-process) — unlike the reverted `read_from: master`, which read a stale remote ref and clobbered sequential writes (`.agents/lessons/read-from-master-clobbers-sequential-writes/`).
- **Writes** — `backend=git-ref` implies the state-ref write mirror; every transition CAS-writes the ref. The per-worktree working copy is STILL written every time (additive) for projection fidelity (§3B) and rollback safety; the ref is never merged into a code branch (D10).
- **Commits** — under git-ref, `da workflow commit` does NOT stage `.agents/workflow/**` plan-state into code-branch commits (it lives on the ref); `.agents/history/**` archives and explicit `--include` paths still stage.
- **Surfacing** — `da workflow status` shows `backend: git-ref` + `coordination SOT: refs/agents/state (live)`.

## Cutover validation (2026-07-18)
- Gate satisfied: `document-and-default-git-ref` complete; `controlled-multiplan-validation` green; read / CAS / per-task / decouple / adoption tests merged (#418, #419, #424, #425, #426).
- Projection compare: the ref projection == the working-copy projection for the migrated plan (byte-faithful, §3B).
- Rollback exercised: reverting `work_tracking` to `{write_to: state-ref}` (or unsetting `backend`) returned reads to the working copy with no divergence / data loss.

## Rollback
Set `work_tracking.backend` back to `local` (or unset it). Reads return to the working copy — always kept current by the additive write — and the ref remains as a durable audit trail. No data migration needed.

## Coordination
`refs/agents/state` is SHARED with a co-located session (lane claims under `active/coordination/`). Always compare-and-swap; never force-update.

## Scope + follow-on
Covers workflow **coordination state** (plan `TASKS.yaml`/`PLAN.yaml` per-task blobs). Extending ref-backing to the managed `.agents/active` artifacts (delegation / merge-back, verification, review) and adding a proposal `scope` flag (route into the available diff-scopes/sources) is the follow-on `workstore-git-ref-backend` extension.
