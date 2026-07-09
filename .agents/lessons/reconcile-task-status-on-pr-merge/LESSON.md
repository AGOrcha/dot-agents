# Reconcile canonical task status the moment a PR merges

## Pattern

A merged PR does NOT auto-advance its task's status in `TASKS.yaml`. As soon as
you observe (or are told) a task's PR merged, run the closeout — don't defer it
and don't assume it happened automatically.

## Root cause

There is no PR-merge → closeout automation yet (tracked for the daemon in
`.agents/proposals/pr-merge-auto-reconcile.md`). Merges done out-of-band — a
human clicking Merge, or a batch of them — leave canonical task status stale:
delegated tasks stay `in_progress` / `pending`, direct tasks stay un-advanced.
Observed on `swallowed-errors-loud-atomic`: `se9-commands-loud` /
`se9-import-loud` / `se5-add-errors` read un-completed long after #365 / #366 /
#367 merged, until a manual reconciliation pass. Stale statuses then mislead
`workflow next` / `eligible`, dependency routing, and fanout gating.

## Rule

- After confirming a task's PR is **MERGED** (not merely green): a delegated
  task with an active delegation + merge-back →
  `da workflow delegation closeout --plan <plan> --task <id> --decision accept`
  (archives the merge-back AND advances to `completed`); a direct task →
  `da workflow advance <plan> --task <id> --status completed`. NEVER hand-edit
  the status in `TASKS.yaml`.
- Only close tasks whose PRs are **actually merged**; a green-but-open PR stays
  `pending` / `in_progress`.
- A superseded umbrella task (decomposed into sub-slices that all merged) →
  `advance ... --status completed` once its sub-slices are done.
- Batch it: cross-reference `gh pr list --state merged` (or per-PR
  `gh pr view <n> --json state`) against task statuses so you catch EVERY stale
  sibling in one pass — don't reconcile one and miss the others.

## How to apply

On a merge signal: map PR → task, verify `gh pr view <n> --json state` is
`MERGED`, run the closeout verb, then re-read `TASKS.yaml` to confirm it now
reads `completed`.

## Cross-references

- `[[verify-task-status-vs-pr-history]]` — TASKS.yaml lies after parallel waves;
  cross-check merged PRs before trusting task state.
- `[[worktree-isolation-defeats-status-tracking]]` — the same status drift from
  out-of-band / isolated completion.
