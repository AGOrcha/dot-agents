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

   Do not pick by reading plan markdown — the eligible surface already encodes dependency satisfaction, conflict detection, and evidence confidence. Run the same `da --json workflow eligible [--plan <scope>]` call and field walkthrough as `orchestrator-session-start/instructions/eligible-orientation.md` — this skill assumes that pass already ran this session, or that the repo has no orchestrator-session-start surface at all, and does not re-derive the walkthrough here.

   See `instructions/eligible.md` for the readiness-label mapping this skill layers on top, and `instructions/annotate.md` for the recommended-batch presentation — that labeling/presentation step is plan-wave-picker's own value-add and is not covered by eligible-orientation.md. The CLI's `max_batch` is pre-computed for non-conflicting parallel execution — do not recompute it.

   Waves are typically ordered `Wave 1`, `Wave 2`, ...; phases similarly `Phase 1`, `Phase 2`, ... When dependencies allow, run waves or phases from independent plan tracks in parallel for the same loop iteration.

4. Check for existing partial work before announcing a fresh selection.

   Same active-bundle check as `orchestrator-session-start/instructions/preflight.md` item 2 — see that file for the full walkthrough (`.agents/active/delegation-bundles/`, `.agents/active/merge-back/`, stale-contract detection). This skill additionally checks local tree state, which preflight.md does not cover:

   ```bash
   git status --short
   ```

   If a delegation bundle already exists for the task `workflow eligible` would select, **do not re-fanout** — hand off to `delegation-lifecycle` with the existing bundle path.

5. Cross-check task status against shipped PRs before fanout.

   Same stale-status drift check as `orchestrator-session-start/instructions/eligible-orientation.md` § "Stale-status drift check" — see that file for the full `gh pr list --state merged` walkthrough. If the work already shipped, run `da workflow delegation closeout --plan <plan> --task <task> --decision accept` instead of fanning out.
