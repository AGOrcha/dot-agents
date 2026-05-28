# Pre-flight: Orchestrator Session Start

Run these checks **before** `workflow orient/status/next`. They prevent redundant fanouts, wasted spawns on already-shipped work, and surface in-flight state that the workflow commands won't show.

## 1. Pending proposals

```bash
ls ~/.agents/proposals/ 2>/dev/null
ls .agents/proposals/ 2>/dev/null
```

Global proposals (`~/.agents/proposals/<id>.yaml`) and project-local proposals (`.agents/proposals/<id>.md`) both qualify. If any unreviewed files exist (not under `archived/`), review and decide on each before starting new work:

```bash
da review show <id>
da review approve <id>   # or reject
```

Do not pick a new task while unreviewed proposals sit in the queue — they may change the shape of the work.

## 2. Active delegation bundles and contracts

```bash
ls .agents/active/delegation-bundles/ 2>/dev/null
ls .agents/active/delegation/ 2>/dev/null
ls .agents/active/merge-back/ 2>/dev/null
```

For each bundle, note its `task_id`. If `workflow next` later selects a task that already has a bundle, **do not re-fanout** — go directly to `delegation-lifecycle` with the existing bundle path.

A contract file without a matching bundle (or vice versa) is a stale artifact — note it before proceeding. Merge-back files without a matching closeout indicate an incomplete loop — run `workflow delegation closeout --decision accept` (after reviewing the merge-back) before fanning out new work for that task.

## 3. Stale-status drift check

`workflow eligible` reports tasks by their TASKS.yaml `status` field, which drifts behind merged PRs after parallel-worker batches. Before treating any "pending" or "in_progress" task as truly active, spot-check against your forge:

```bash
gh pr list --state merged --search "<task-id>" --limit 3
git log --oneline --all | grep -iE "<task-id>" | head -3
```

If recent eligible tasks all show merged PRs, the project needs a status sweep: run `workflow delegation closeout --decision accept` on each shipped-but-not-advanced task before picking new work.

## 4. Worker loop context

Skim `.agents/active/loop-state.md` (and `.agents/active/active.loop.md` if present) — read only:

- `## Current Position` — what the worker last reported as in flight
- Last 1–2 entries in `## Iteration Log` — what was committed and what's left
- `## Next Iteration Playbook` — whether the worker left a clear next action

This is read-only context. The orchestrator does not modify `loop-state.md`; that belongs to the worker. Use it to avoid selecting work the worker already has in progress.

## 5. Verifier output queue

```bash
ls .agents/active/verification/ 2>/dev/null
```

If verifier_profiles are registered for this project, each task with active verification has a directory here. A `<task-id>/<profile>.result.yaml` with a terminal `READY` status is the parent's cue to run `workflow delegation gate` + `workflow delegation closeout`. A non-terminal verifier result means the verifier is still running — do not pick more work that conflicts with it.

## 6. Fold-back queue

```bash
ls .agents/active/fold-back/ 2>/dev/null
```

Observations from completed loops that have not yet been routed into plan artifacts or proposals. Drain via `da workflow fold-back create` before starting new work — they often change the shape of the next task.
