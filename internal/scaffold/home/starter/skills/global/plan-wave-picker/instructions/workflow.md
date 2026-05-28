# Workflow: Plan Wave Picker

Use this skill at the start of a session when multiple canonical plans exist under `.agents/workflow/plans/`.

`.agents/active/` is NOT where plans live — it holds transient runtime artifacts (delegation bundles, merge-back, fold-back, iteration logs, `active.loop.md`). Plans live at `.agents/workflow/plans/<plan-id>/` as a `PLAN.yaml` + `TASKS.yaml` + `<plan-id>.plan.md` triple.

## Selection process

1. Read plan statuses through the CLI, not by globbing markdown.

   Canonical status lives in each plan's `PLAN.yaml`. Use the CLI to enumerate active plans rather than grepping markdown — the markdown line is often stale:

   ```bash
   da workflow plan
   da --json workflow plan
   ```

   Only fall back to grepping `<plan-id>.plan.md` when the CLI surfaces drift you need to reconcile by hand.

2. Check dependency ordering and existing in-flight work.

   ```bash
   da workflow status
   da workflow orient
   ```

   `workflow status` shows active delegations and in-progress tasks; `workflow orient` shows the broader session snapshot. Combined, they tell you which plans are advancing and which are blocked.

3. Pick the lowest-priority unblocked wave or phase via the eligible surface.

   Do not pick by reading plan markdown — the eligible surface already encodes dependency satisfaction, conflict detection, and evidence confidence:

   ```bash
   da --json workflow eligible
   da --json workflow eligible --plan <plan-id-1>,<plan-id-2>
   ```

   See `instructions/eligible.md` for which fields to extract and `instructions/annotate.md` for the recommended-batch presentation. The CLI's `max_batch` is pre-computed for non-conflicting parallel execution — do not recompute it.

   Waves are typically ordered `Wave 1`, `Wave 2`, ...; phases similarly `Phase 1`, `Phase 2`, ... When dependencies allow, run waves or phases from independent plan tracks in parallel for the same loop iteration.

4. Check for existing partial work before announcing a fresh selection.

   Untracked or modified files in `git status` (or a non-empty `.agents/active/delegation-bundles/`) can indicate a phase already started:

   ```bash
   git status --short
   ls .agents/active/delegation-bundles/ 2>/dev/null
   ls .agents/active/merge-back/ 2>/dev/null
   ```

   If a delegation bundle already exists for the task `workflow eligible` would select, **do not re-fanout** — hand off to `delegation-lifecycle` with the existing bundle path.

5. Cross-check task status against shipped PRs before fanout.

   `da workflow eligible` reports tasks whose `status: pending|in_progress` in TASKS.yaml. It does not cross-check whether the work already shipped via a merged PR — status commonly drifts after parallel-worker batches. For each task in `max_batch`, run a quick merged-PR / commit search on the task ID before fanning out (`gh pr list --state merged --search "<task-id>"` or the equivalent for your forge). If the work shipped, run `da workflow delegation closeout --plan <plan> --task <task> --decision accept` instead of fanning out.
