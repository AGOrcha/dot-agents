# Planning Evidence-Backed Write Scope

Status: active

## Goal

- Audit how planning currently works across `~/.cursor`, `~/.claude`, `~/.codex`, `.agents/history`, and `.agents/workflow/plans/`.
- Turn the first concrete improvement into a repo-local spec under `.agents/workflow/specs/`.
- Define how `write_scope` should be justified by graph-backed evidence instead of hand-authored path guesses alone.

## Findings

1. Cursor and Claude plans are strong at narrative analysis, decomposition, and file-level reasoning, but they are not canonical execution contracts.
2. Codex has durable session history and backlog suggestions, but not a structured planning artifact that explains why a task's file scope is complete.
3. Canonical workflow plans in `.agents/workflow/plans/` are the strongest execution surface in this repo, but `write_scope` is still a freeform list with no provenance or completeness check.
4. The repo already has the raw graph/query surface needed for better planning:
   - `dot-agents kg impact`
   - `dot-agents kg changes`
   - `dot-agents kg bridge query --intent ...`
   - `dot-agents workflow graph query --intent ...`
5. Current graph usage is concentrated in review, debugging, and context lookup. Plan authoring does not yet require or preserve graph evidence.

## First Deliverable

1. Add `.agents/workflow/specs/planner-evidence-backed-write-scope/design.md`.
2. Define an evidence sidecar model that can justify `write_scope` without immediately forcing `TASKS.yaml` schema churn.
3. Outline follow-on command and skill work needed to make the planning flow operational.
4. Record the graph-bridge readiness dependency explicitly so planning automation does not outrun the command surface.

## Follow-On Slices

1. Add a read-only planner command that derives candidate scope from graph queries plus planner seeds.
2. Add a read-only checker that compares actual changed files to the task's evidenced scope.
3. Decide when missing evidence should warn versus block `workflow fanout` for code tasks.

## Current Blocker

- Graph-backed planner automation is blocked on [graph-bridge-command-readiness-resurrection.plan.md](/Users/nikashp/Documents/dot-agents/.agents/active/graph-bridge-command-readiness-resurrection.plan.md).
- The bridge/routing surface was marked complete historically, but current command behavior is not dependable enough yet for planner scope derivation.
