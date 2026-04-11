# Loop Improvements Review — Results (2026-04-11)

## Review Scope

Reviewed the most recent loop iterations (11-13), their paired commits, the active `loop.prompt.md`, and the active `loop-state.md` to identify workflow friction in prompt sequencing and loop-state maintenance.

## Main Findings

- The loop was effectively producing two commits per iteration: one implementation commit and one follow-up `loop-state:` commit. That weakens atomicity and adds avoidable branch noise.
- `## Command Coverage` and `## Scenario Coverage` were drifting from the traces they summarize. The clearest example was `workflow sweep --dry-run` being logged in iteration 13 while `workflow sweep` still showed as untested.
- `## Next Iteration Playbook` had become append-only, so newer guidance sat beside stale candidate-path blocks instead of replacing them.
- Scenario tags were not always using the canonical row names from `## Scenario Coverage`, which makes coverage reconciliation brittle.
- Recent iterations were still getting value from CLI checks, but they leaned too often on repeated `workflow health` confirmations even when a closer or less-covered surface existed.

## Changes Made

### Prompt updates

- Reworked the loop prompt so read-in starts from a compact subset of loop-state sections instead of encouraging broad rereads.
- Changed the workflow sequencing to prefer one final commit containing implementation plus loop-state/history updates, with diff stats taken from `git diff --cached --stat`.
- Added an evidence budget rule: one primary chain plus at most one secondary probe.
- Added explicit guidance to avoid back-to-back `workflow health` or `status` traces unless they are the closest justified surface.
- Required canonical scenario-tag names and a per-command status line for mixed-outcome trace chains.
- Added explicit reconciliation instructions for `## Command Coverage`, `## Scenario Coverage`, and section rewrites in `## Next Iteration Playbook`.

### Loop-state updates

- Added a `## Loop Health` section to capture meta-quality issues found in the recent iterations and to document the operating rules for iteration 14+.
- Rewrote `## Next Iteration Playbook` into one current operating block instead of two appended candidate lists.
- Corrected stale `## Command Coverage` rows for `explain`, `workflow status`, `workflow health`, `workflow drift`, `workflow sweep`, and `kg health`.
- Backfilled the missing `## Error Log` entry for iteration 2 and updated the corresponding outcome-quality scenario row.

## Files Updated

- `.agents/active/loop.prompt.md`
- `.agents/active/loop-state.md`
- `.agents/history/loop-improvements-review/loop-improvements-review.plan.md`

## No Product Code Changes

This review updated loop-process artifacts only. No Go production code or tests changed.
