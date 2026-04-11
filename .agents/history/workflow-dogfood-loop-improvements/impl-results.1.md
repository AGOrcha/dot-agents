# Workflow Dogfood Loop Improvements — Results (2026-04-11)

## Review Inputs

Reviewed:
- `README.md`
- `docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md`
- `research/AGENT_AS_OPERATOR_RESEARCH.md`
- `../payout/.agents/active/go-modular-monolith-migration.loop.prompt.md`
- `../payout/.agents/active/go-modular-monolith-migration.loop-state.md`

## Intended Product Loop

The local docs are consistent about the intended operating model:

- `orient -> work -> persist -> propose`
- agents operate the workflow system; humans steer and review
- canonical workflow state should be explicit, inspectable, and portable
- `dot-agents workflow ...` commands are the readback/debugging surface for that workflow layer

For this repo, the most relevant practical surfaces are:
- `workflow orient` for session-start context
- `workflow status` for checkpoint-backed readback
- `workflow plan` / `workflow tasks` for canonical plan and task state
- `workflow checkpoint` / `workflow verify` for persist surfaces

## Transferable Practices From `../payout`

Relevant ideas adopted:

- Treat workflow/KG readback as an explicit operating surface, not only as incidental evidence.
- Keep a live baseline section in loop-state for the command surfaces the loop can rely on today.
- Separate baseline reality from iteration-specific verification traces.
- Use the tool’s own readback to document known-empty, known-warning, and known-stale states explicitly instead of rediscovering them each iteration.
- Make the startup order explicit: orient first, then current-status readback, then canonical-plan inventory, then choose work.

Ideas not copied directly:

- `../payout`’s regression matrix and live-testing queue are domain-specific to that migration program.
- Its dev-binary-from-sibling-repo workflow is useful there because `payout` is consuming `dot-agents` as an external tool; this repo can run the local CLI directly.
- Its verification-surface list is broader because it mixes app test surfaces with `dot-agents` readback; here the focus is tighter on workflow dogfooding.

## Changes Made

### Prompt

Updated `.agents/active/loop.prompt.md` to:

- introduce an explicit workflow dogfood rhythm at the top
- require `workflow orient`, `workflow status`, and `workflow plan` at session start
- use `workflow tasks <id>` when the selected wave has a canonical plan
- treat stale `workflow status` next-action output as a tracked freshness issue rather than silently following it
- add `workflow verify log` to the read-only command set
- prefer sandboxed `workflow checkpoint` / `workflow verify record` as the default persist-path dogfooding mode when real `~/.agents` writes are not approved
- extend self-assessment to record whether the iteration used workflow orient/status, aligned with canonical tasks, and persisted via workflow commands

### Loop State

Updated `.agents/active/loop-state.md` to:

- mark iteration `15` as the current loop-state baseline
- add explicit current-position bullets for the now-usable workflow-command surfaces
- add a `## Workflow Command Baseline` section summarizing live readback for orient/status/health/plan/tasks/verify-log
- make the stale `workflow status` next action explicit as a baseline workflow issue
- update the playbook so the next iteration starts with workflow-command readback and prefers sandboxed persist surfaces at closeout
- update scenario coverage so `canonical-plan-present` is no longer incorrectly marked as uncovered
- clarify that `## Command Coverage` is last loop-traced usage, not the entire live-capability picture

## Live Baseline Confirmed During Review

- `go run ./cmd/dot-agents workflow orient`
- `go run ./cmd/dot-agents workflow status`
- `go run ./cmd/dot-agents workflow plan`
- `go run ./cmd/dot-agents workflow tasks crg-kg-integration`
- `go run ./cmd/dot-agents workflow health`
- `go run ./cmd/dot-agents workflow verify log`

Observed:
- canonical plans are present and readable
- canonical tasks are readable for `crg-kg-integration`
- verification log is a clean empty-state
- `workflow status` next action is stale checkpoint text and should be treated as such

## No Product Code Changes

This work updated loop-process artifacts only. No Go production code or tests changed.
