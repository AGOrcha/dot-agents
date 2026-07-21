# Lesson: close a plan via `plan update --status completed`, never `archive --force`

## Symptom

At perf-plan closeout I advanced all tasks to `completed`, then ran
`da workflow plan archive --plan <id>`. It errored — plan header was still
`draft` (`expected completed`). Instead of transitioning the header, I reached
for `--force`, which archived a `draft` plan directly, skipping the
`draft → completed` step. The user corrected it.

## Root cause

**Task status and PLAN header status are separate.** `da workflow advance`
moves TASK statuses; it does NOT roll the PLAN header from `draft`→`completed`.
The header is only advanced by `da workflow plan update <id> --status completed`
(the plan-status lifecycle is `draft|active|paused|completed|archived`).
Because I drove the phases as direct PR work (not the `start-task`/`close-task`
loop), the header never left `draft`. `plan archive` guards on
`status == completed`; `--force` bypasses that guard rather than satisfying it.

## Fix — the closeout sequence

```sh
# 1. every task completed (direct work: advance; delegated: delegation closeout)
da workflow advance <plan> --task <id> --status completed   # per task

# 2. transition the PLAN header (the step that's easy to forget)
da workflow plan update <plan> --status completed

# 3. archive — no --force needed once status=completed
da workflow plan archive --plan <plan> [--no-commit]
```

`--force` is only for genuinely abandoning/archiving an incomplete plan on
purpose — never as a shortcut around a header you simply forgot to advance.

## Notes

- There is **no un-archive command**. If you mis-archive, and you used
  `--no-commit` (so nothing was committed), restore by moving
  `.agents/history/<id>/{PLAN,TASKS}.yaml` back to
  `.agents/workflow/plans/<id>/` and the linked `design.md` back to
  `.agents/workflow/specs/<id>/`, then redo steps 2–3.
- `--no-commit` is the safe archive mode in a dirty checkout: `plan archive`'s
  default commit can sweep repo-wide; stage the archive move and commit it
  scoped (or in the batched session commit) instead.
- Sibling of [[reconcile-task-status-on-pr-merge]] and
  [[stale-plan-status-vs-reality]] — canonical status must be driven by the
  CLI, not left to drift.
