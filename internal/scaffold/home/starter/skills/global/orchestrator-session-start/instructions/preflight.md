# Pre-flight: Orchestrator Session Start

Run these checks **before** `workflow orient/status/next`. They prevent redundant fanouts and surface in-flight state that the workflow commands won't show.

## 1. Pending proposals

```bash
ls ~/.agents/proposals/ 2>/dev/null
```

If any `.yaml` files exist (not under `archived/`), review and decide on each before starting new work:

```bash
da review show <id>
da review approve <id>   # or reject
```

Do not pick a new task while unreviewed proposals sit in the queue — they may change the shape of the work.

## 2. Active delegation bundles

```bash
ls .agents/active/delegation-bundles/
```

For each bundle, note its `task_id`. If `workflow next` later selects a task that already has a bundle, **do not re-fanout** — go directly to `delegation-lifecycle` with the existing bundle path.

Also check the delegation contract directory:

```bash
ls .agents/active/delegation/
```

A contract file without a matching bundle (or vice versa) is a stale artifact — note it before proceeding.

## 3. Worker loop context

Skim `.agents/active/loop-state.md` — read only:

- `## Current Position` — what the worker last reported as in flight
- Last 1–2 entries in `## Iteration Log` — what was committed and what's left
- `## Next Iteration Playbook` — whether the worker left a clear next action

This is read-only context. The orchestrator does not modify `loop-state.md`; that belongs to the worker. Use it to avoid selecting work the worker already has in progress.
