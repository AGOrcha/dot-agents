# Gotchas: Plan Wave Picker

Common failure points:

## Status Detection

- Matching a loose `Completed` string can give false positives when the plan uses richer status text. Check the full `Status:` line instead.
- A plan may be partially updated while still active. Do not assume the presence of one completed checkbox means the whole wave is done.

## Source Of Truth

- Spec documents under `docs/` or `.agents/workflow/specs/` remain the source of truth when a plan file is thin or stale.
- Existing dirty or untracked files can indicate work already started. Check them before selecting the next untouched phase.

## Post-Selection Hygiene

- After implementing a wave, update the plan status line rather than leaving the next agent to infer completion from code changes.
- Picking a phase that touches multiple existing packages does not justify creating a new package by default. Prefer extending the existing structure unless the plan explicitly calls for a new module.
