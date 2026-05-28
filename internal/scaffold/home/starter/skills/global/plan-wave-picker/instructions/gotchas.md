# Gotchas: Plan Wave Picker

Common failure points:

## Canonical Paths

- Plans live under `.agents/workflow/plans/<plan-id>/` — never `.agents/active/`. The `.agents/active/` directory holds transient runtime artifacts (delegation bundles, merge-back, fold-back, iteration logs). If a script or instruction references `.agents/active/*.plan.md`, treat it as stale and fix it.
- Prefer `da workflow plan` (and `da workflow tasks <plan-id>`) over globbing markdown. The CLI reads `PLAN.yaml` / `TASKS.yaml` — those are canonical; the markdown is descriptive only.

## Status Detection

- Matching a loose `Completed` string in plan markdown can give false positives when the plan uses richer status text. The authoritative status is in `PLAN.yaml` and `TASKS.yaml` — query it via the CLI.
- A plan may be partially updated while still active. Do not assume that one completed checkbox in the markdown means the whole wave is done.
- TASKS.yaml `status` drifts after parallel-worker batches: tasks land as merged PRs but `delegation closeout` is missed. Always cross-check `eligible` output against merged-PR history before fanning out a task.

## Source Of Truth

- Spec documents under `.agents/workflow/specs/<id>/design.md` remain the source of truth when a plan file is thin or stale. Specs own the **what and why**; plans own the **how and in what order**.
- Existing dirty or untracked files, plus active delegation bundles, can indicate work already started. Check them before selecting the next untouched phase.
- A bundle already on disk wins over a fresh eligible-batch pick — hand off to `delegation-lifecycle` rather than re-fanning out.

## Post-Selection Hygiene

- After implementing a wave, advance the canonical task via `da workflow advance` (or, for delegated work, `da workflow delegation closeout --decision accept`). Do not leave the next agent to infer completion from code changes.
- Picking a phase that touches multiple existing packages does not justify creating a new package by default. Prefer extending the existing structure unless the plan explicitly calls for a new module.
