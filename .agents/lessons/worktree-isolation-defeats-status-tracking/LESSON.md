# Worktree-isolated workers can't track status through the main-repo scout

## Pattern

When an orchestrator fans out parallel workers in **isolated git worktrees**, a worker's task-status
update (`workflow merge-back` / `advance`) writes to **that worktree's** copy of `TASKS.yaml` — it
never reaches the main repo. The orchestrator/scout reads the **main repo's** TASKS.yaml each wave, so
it keeps seeing the same task as `pending` and **re-dispatches it every wave**.

## Root cause

Coordination state (task status) is tracked in the same per-branch/per-worktree git filesystem used
to isolate code. Isolation is correct for code safety but guarantees the **status plane diverges from
what the scout reads**. There is no shared source of truth for "what is already in-flight."

Evidence: the first ultracode wave-engine run produced **5 duplicate PRs for one task (`p1c`), 2 for
`cj`, and a duplicate of an already-merged `cg`** — the engine churned the same ~5 tasks across 6
waves instead of progressing, because none ever left the scout's `pending` view.

## Rule

- Never assume a worker's in-worktree status update is visible to the orchestrator. It is not.
- A fan-out scout MUST derive "already in-flight" from a **shared signal**, not from per-worktree
  files. In order of robustness:
  1. **Open-PR query** (`gh pr list --state open`) — a task whose id is in an open PR branch/title is
     in review; exclude it. (Cross-run safe.)
  2. **In-orchestrator dispatched-set** across waves — never re-dispatch a task fanned out earlier in
     the same run. (Within-run safe.)
  3. The real fix: a **shared coordination backend** the scout reads authoritatively (see
     `[[work-tracking-storage-abstraction]]` spec).
- Apply 1+2 together as the minimum; treat the backend as the structural cure.
- Also: guard the scout result for `null` (a scout that dies on a session/capacity limit returns
  null) — treat it as a dry wave and stop cleanly, never `someVar.field` on it.

## How to apply

Building or reviewing a fan-out loop over isolated worktrees:
1. The scout prompt excludes tasks with an open PR AND the engine keeps a `dispatched` Set across
   waves.
2. Status transitions that must gate the scout (claimed / awaiting-review) belong in a shared store,
   not a worktree file — until that exists, the open-PR + dispatched-set guards are mandatory.
3. Null-guard every cross-agent result before dereferencing.

## Cross-references

- `[[work-tracking-storage-abstraction]]` — the spec for the shared-backend structural fix.
- `[[concurrent-workers-one-worktree]]` — one active writer per worktree; related isolation hazard.
- `[[validate-bundle-against-head]]` — TASKS.yaml status decays between snapshots; HEAD-validate.
