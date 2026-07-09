# PR-merge event → auto-reconcile canonical task status

**Date:** 2026-07-09
**Scope:** project-local (per `proposal-routing.md` — targets dot-agents' own `da service` daemon + `da workflow` CLI; would not matter if this repo left dot-agents management)
**Target:** `da service` (`r3-background-worker-service`) gains a PR-merge subscription that runs the task closeout; rides `unified-event-contract`.
**Status:** proposal — captures a gap observed live on 2026-07-09 during the swallowed-errors plan closeout.

## Problem

When a task's PR merges, its canonical status in `TASKS.yaml` does **not** advance automatically. The closeout — `da workflow delegation closeout --decision accept` (delegated task) or `da workflow advance --status completed` (direct task) — is a MANUAL step someone must run after observing the merge.

Observed live on `swallowed-errors-loud-atomic`: after PRs #365 / #366 / #367 merged out-of-band, `se9-commands-loud` still read `in_progress`, and `se9-import-loud` / `se5-add-errors` still `pending`; a reconciliation pass had to run the closeout sequence by hand. This is the failure mode the lessons `verify-task-status-vs-pr-history` and `worktree-isolation-defeats-status-tracking` name ("TASKS.yaml lies after parallel waves"). Stale statuses mislead `workflow next` / `eligible`, dependency routing, and fanout gating — and a missed reconciliation goes unnoticed until someone audits PR history against task state.

## Proposed capability (daemon-owned)

The long-running `da service` (`r3-background-worker-service`) already hosts filesystem watchers plus a notification bus. Add a PR-merge → closeout job:

1. **Source the merge event.** GitHub webhook (push-based, preferred) into the service's HTTP surface, or a periodic `gh pr list --state merged` poll where no webhook endpoint is configured.
2. **Map PR → canonical task.** Resolve the merged PR number / head branch to its task id. This needs a *durable* PR↔task link: today the PR URL lives only in freeform `notes:`, so promote it to a structured `pr:` / `head_branch:` field on the task, populated at `workflow fanout` / PR-open time.
3. **Run the closeout, idempotently.** Delegated task with an active delegation + merge-back → `delegation closeout --decision accept`; direct task → `advance --status completed`. No-op if already `completed`.
4. **Emit the reconcile on the bus** (`unified-event-contract`) so the R2 dashboard / R5 review queue observe the status change in real time.

## Relationship

- **Extends** `r3-background-worker-service` — the host + watcher + bus. Its v1 is scoped to iter-log scoring; a PR-merge watcher is a natural second subscriber on the same service.
- **Rides** `unified-event-contract` for the event surface.
- **Depends on** a structured PR↔task link field (small `TASKS.yaml` schema addition; `workflow fanout` / the PR-create path populate it).

## Interim (until the daemon ships)

The operating agent MUST run the closeout for a task **as soon as it observes that task's PR merged** — `delegation closeout --decision accept` (delegated) / `advance --status completed` (direct) — not defer it and not assume it is automatic. Captured as lesson `reconcile-task-status-on-pr-merge`.
