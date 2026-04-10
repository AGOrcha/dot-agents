# Workflow Automation Follow-On Spec

Status: Directional
Last updated: 2026-04-10
Depends on: `docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md`

This document captures the next-wave and currently out-of-scope workflow automation ideas that follow the MVP contract in `docs/WORKFLOW_AUTOMATION_PRODUCT_SPEC.md`.

The MVP spec remains the source of truth for the first implementation wave.

This follow-on spec exists so later planning does not need to re-synthesize the research backlog from scratch.

## Purpose

The MVP deliberately stops at:

- orient
- persist
- proposal review
- basic safety and quality hooks
- escape-hatch workflow commands

That is the correct first slice, but it leaves several real workflow concerns intentionally deferred:

- canonical plan and task artifacts
- machine-readable query surfaces for agents and subagents
- richer verification and environment-health memory
- repo preferences and user overrides
- knowledge-graph bridge and integration readiness
- bounded multi-agent coordination
- cross-repo workflow drift and sweep operations
- rollback and lifecycle controls beyond simple review approval

This document defines the intended post-MVP direction and sequencing for those ideas.

## Document Status

This spec has two levels of commitment:

- `Next wave`
  - intended to be the first planning target after MVP acceptance
  - should be considered design-ready enough for an implementation plan
- `Directional backlog`
  - product direction is clear
  - implementation details still require a focused RFC before coding

## Entry Criteria

No work from this document should start until the MVP wave is complete and stable.

The minimum gate is:

- canonical hook bundles are working in the supported MVP platforms
- orient, checkpoint, and proposal schemas are implemented without active churn
- the review queue is being used successfully for real proposals
- repo-local and user-local workflow artifact boundaries are validated in practice

## Product Principles

These principles still apply after the MVP:

- Agents remain the primary operators.
- Humans remain reviewers and decision-makers for shared behavior changes.
- Read-only machine access should come before new mutating automation.
- Shared repo behavior belongs in repo-local artifacts when it should travel with the project.
- User- or machine-specific operational memory belongs in `~/.agents/`.
- External knowledge systems should integrate through stable bridge contracts, not ad hoc prompt conventions.
- Multi-agent coordination must prefer explicit ownership over optimistic concurrency.

## Post-MVP Themes

The next work naturally groups into six themes:

1. Canonical plan and task artifacts
2. Structured query and health surfaces
3. Shared workflow preferences
4. Knowledge-graph bridge and integration readiness
5. Delegation and merge-back
6. Cross-repo sweep and drift management

## Wave 2: Canonical Plan And Task Artifacts

Status: Next wave

### Problem

The MVP deliberately reads existing Markdown plans and handoffs, but the research shows plans are not just notes. They are execution artifacts with status, dependencies, and blockers. A weaker agent should not have to infer task state from prose when the workflow itself depends on that structure.

### Goals

- Introduce canonical repo-local plan and task artifacts.
- Preserve human-readable planning while adding deterministic machine state.
- Support dependencies, blockers, verification expectations, and active-next-task resolution.
- Keep compatibility with existing `.agents/active/*.plan.md` usage during migration.

### Resolved Decisions

- Canonical plan and task state is repo-local.
- The canonical format is additive during migration, not an immediate replacement for Markdown plan docs.
- `dot-agents` should be able to index legacy Markdown plans until canonical plan bundles exist.
- A canonical plan bundle owns both high-level plan metadata and task-graph state for one plan.

### Canonical Artifacts

New repo-local artifacts introduced in this wave:

| Path | Purpose |
|------|---------|
| `.agents/workflow/plans/<plan-id>/PLAN.yaml` | canonical plan metadata and phase summary |
| `.agents/workflow/plans/<plan-id>/TASKS.yaml` | canonical task graph, task status, and dependency state |
| `.agents/workflow/plans/<plan-id>/plan.md` | optional human-readable narrative companion |

### PLAN Schema

`PLAN.yaml` should include at least:

- `schema_version`
- `id`
- `title`
- `status`
  - `draft`, `active`, `paused`, `completed`, `archived`
- `summary`
- `created_at`
- `updated_at`
- `owner`
- `success_criteria`
- `verification_strategy`
- `current_focus_task`

### TASKS Schema

`TASKS.yaml` should include:

- `schema_version`
- `plan_id`
- `tasks`
  - each task has:
    - `id`
    - `title`
    - `status`
      - `pending`, `in_progress`, `blocked`, `completed`, `cancelled`
    - `depends_on`
    - `blocks`
    - `owner`
    - `write_scope`
    - `verification_required`
    - `notes`

### Migration And Compatibility

- Existing `.agents/active/*.plan.md` files remain valid.
- When a canonical plan bundle exists, it is the machine-readable source of truth.
- `plan.md` is optional and exists for human readability, not canonical machine state.
- A later migration helper may derive starter `PLAN.yaml` and `TASKS.yaml` files from Markdown plans, but that migration tool is not required in the first implementation pass of this wave.

### CLI Surface

This wave should add or extend:

- `dot-agents workflow plan`
- `dot-agents workflow plan show <plan-id>`
- `dot-agents workflow tasks <plan-id>`
- `dot-agents workflow advance <plan-id> --task <task-id> --status <status>`

### Acceptance Standard

This wave is complete when an agent can determine the active plan, active task, blockers, and next unblocked task without parsing prose-only plan files.

## Wave 3: Structured Query And Health Surface

Status: Next wave

### Problem

The MVP gives agents reliable human-readable orient output and a stable checkpoint file, but post-MVP agents will still need to read multiple files or screen-scrape Markdown to answer routine questions such as:

- what is the current verification state?
- what plans are active?
- what is the next recommended action?
- are there pending proposals?
- is the repo healthy enough to continue?

### Goals

- Provide a stable machine-readable surface over workflow state.
- Track verification and tool-health data without bloating checkpoints.
- Keep the human workflow unchanged.

### Resolved Decisions

- Stable JSON surfaces come before any dedicated workflow MCP server.
- The first machine interface is read-only.
- Verification run history is user-local, not repo-local.
- Health state is advisory; it should warn and summarize, not silently mutate workflow state.

### Canonical Artifacts

New user-local artifacts introduced in this wave:

| Path | Purpose |
|------|---------|
| `~/.agents/context/<project>/verification-log.jsonl` | append-only verification run history |
| `~/.agents/context/<project>/health.json` | current environment and workflow health snapshot |

### Verification Record Schema

Each line in `verification-log.jsonl` is one JSON object:

```json
{
  "schema_version": 1,
  "timestamp": "2026-04-09T23:45:00Z",
  "kind": "test",
  "status": "pass",
  "command": "go test ./...",
  "scope": "repo",
  "summary": "all packages passed",
  "artifacts": [],
  "recorded_by": "dot-agents workflow verify"
}
```

Rules:

- `kind` is one of `test`, `lint`, `build`, `format`, or `custom`
- `status` is one of `pass`, `fail`, `partial`, or `unknown`
- `scope` is one of `file`, `package`, `repo`, or `custom`
- `artifacts` is a list of relative or absolute paths to external logs when present

### Health Snapshot Schema

`health.json` is a compact snapshot, not a historical log:

```json
{
  "schema_version": 1,
  "timestamp": "2026-04-09T23:46:00Z",
  "git": {
    "inside_repo": true,
    "branch": "feature/workflow-automation",
    "dirty_file_count": 3
  },
  "workflow": {
    "has_active_plan": true,
    "has_checkpoint": true,
    "pending_proposals": 2
  },
  "tooling": {
    "mcp": "unknown",
    "auth": "unknown",
    "formatter": "available"
  },
  "status": "warn",
  "warnings": [
    "2 pending proposals need review"
  ]
}
```

### CLI Surface

This wave should add or extend:

- `dot-agents workflow status --json`
- `dot-agents workflow health`
- `dot-agents workflow health --json`
- `dot-agents workflow verify record --kind ... --status ... --summary ...`
- `dot-agents workflow verify log [--all]`

These are still support commands. Hooks and canonical artifacts remain the primary automation path.

### Acceptance Standard

This wave is complete when an agent can retrieve the repo’s current workflow state with one machine-readable query instead of reconstructing it from multiple files.

## Wave 4: Shared Preferences And Compatibility

Status: Next wave

### Problem

The research shows that agents repeatedly relearn repo preferences such as:

- preferred test command
- CI expectations
- review style
- plan file conventions
- verification standards

Without explicit preference storage, the same corrections repeat.

### Goals

- Persist workflow preferences explicitly.
- Separate team-shared preferences from user-local overrides.
- Route shared preference changes through the existing proposal queue.

### Resolved Decisions

- Team-shared workflow preferences are repo-local.
- User-specific overrides are user-local.
- Shared preference changes require review.
- User-local override changes do not require review.

### Canonical Artifacts

| Path | Purpose |
|------|---------|
| `.agents/workflow/preferences.yaml` | repo-shared workflow defaults |
| `~/.agents/context/<project>/preferences.local.yaml` | user-local overrides |

### Preference Precedence

The precedence order is:

1. `~/.agents/context/<project>/preferences.local.yaml`
2. `.agents/workflow/preferences.yaml`
3. built-in defaults

### Preference Categories

The initial supported categories should be:

- `verification`
  - preferred test command
  - preferred lint command
  - whether full-regression verification is required before handoff
- `planning`
  - preferred plan directory
  - whether plan updates are required before code changes
- `review`
  - preferred review order
  - whether findings-first output is required
- `execution`
  - package manager preference
  - formatter preference when multiple valid tools exist

### CLI Surface

This wave should add or extend:

- `dot-agents workflow prefs`
- `dot-agents workflow prefs show`
- `dot-agents workflow prefs set-local <key> <value>`

Shared repo preference changes should still flow through proposal files and `dot-agents review`.

### Acceptance Standard

This wave is complete when an agent can discover repo workflow expectations from canonical artifacts instead of inferring them from repeated corrections.

## Wave 5: Knowledge-Graph Bridge And Integration Readiness

Status: Next wave

### Problem

The knowledge-graph and ingestion layer should remain its own product, but `dot-agents` should be integration-ready so agents can query external graph-backed context through deterministic workflow-aware interfaces instead of rediscovering conventions in prompts.

### Goals

- Make `dot-agents` a stable bridge to external knowledge systems used by agents.
- Normalize query intents and response shapes for workflow-related graph access.
- Keep graph ingestion, indexing, and storage outside the `dot-agents` product boundary.

### Resolved Decisions

- `dot-agents` is not the knowledge graph and does not own ingestion pipelines.
- The first graph bridge is read-only.
- Query semantics should be canonical and transport-neutral.
- Provider-specific graph adapters may vary, but the query intents and normalized outputs should not.
- DKG-style shared memory and verification protocols belong to the knowledge-graph layer, not the `dot-agents` core workflow layer.

### Bridge Scope

The initial bridge should support deterministic query intents such as:

- `plan_context`
  - get supporting decisions, specs, and lessons for a plan or task
- `decision_lookup`
  - retrieve prior decisions and rationale by topic or identifier
- `entity_context`
  - retrieve graph context for a subsystem, service, or project area
- `workflow_memory`
  - retrieve prior handoffs, learnings, or linked artifacts relevant to current work
- `contradictions`
  - surface conflicting or stale knowledge relevant to the active workflow state

### Canonical Artifacts

This wave may introduce:

| Path | Purpose |
|------|---------|
| `.agents/workflow/graph-bridge.yaml` | repo-local bridge policy, allowed query intents, and context-mapping rules |
| `~/.agents/context/<project>/graph-bridge-health.json` | local adapter availability and last-query health |

### Normalized Query Contract

Every graph bridge query should resolve to a normalized shape with:

- `intent`
- `project`
- `scope`
- `query`
- `results`
- `warnings`
- `provider`
- `timestamp`

The point is not to hide provider differences completely. The point is to ensure the agent always has one deterministic contract to ask through.

### CLI Surface

These are likely escape-hatch commands for the first bridge:

- `dot-agents workflow graph query --intent ...`
- `dot-agents workflow graph health`

### Acceptance Standard

This wave is complete when an agent can obtain graph-backed workflow context through one deterministic query contract without needing repo-specific prompt conventions.

## Wave 6: Delegation And Merge-Back

Status: Directional backlog

### Problem

The research clearly shows subagents are a real workflow primitive, but the MVP intentionally assumes a single active writer per repo. That assumption will break down once `dot-agents` starts helping orchestrate multi-agent work.

### Goals

- Make delegated work explicit and bounded.
- Require ownership of write scope.
- Produce merge-back artifacts that reduce integration guesswork.

### Resolved Direction

- Delegation coordination artifacts should be repo-local because they describe the work itself.
- Parent agents should remain responsible for orchestration and final integration.
- `dot-agents` may assist with structure, but should not auto-resolve overlapping write scopes.
- Hermes-style coordination semantics are in scope, but literal marker strings are not the canonical storage format.

### Candidate Artifacts

| Path | Purpose |
|------|---------|
| `.agents/active/delegation/<task-id>.yaml` | delegated task contract |
| `.agents/active/merge-back/<task-id>.md` | subagent return summary for parent integration |

### Delegation Contract Requirements

Every delegated task should declare:

- parent task or plan reference
- write scope
- success criteria
- verification expectations
- whether the task may mutate shared workflow state

### Coordination Intents

The Hermes-inspired coordination pattern is useful here, but it should be modeled as transport-neutral semantics:

- `status_request`
- `review_request`
- `escalation_notice`
- `ack`

Canonical coordination artifacts should store these as enums or fields, not as raw chat syntax.

Chat-based transports may still render them as literal markers such as `[STATUS_REQUEST]` or `[ACK]`, but:

- `@mentions`
- one-message-per-turn rules
- channel or thread routing
- terminal reply mechanics

belong to the transport adapter or runtime protocol, not to canonical repo storage.

### Candidate CLI Surface

These are not committed yet, but are the likely escape-hatch commands:

- `dot-agents workflow fanout`
- `dot-agents workflow merge-back`

### Blocking Risks

- write-scope overlap is a correctness problem, not just a UX problem
- naive append-only coordination will create drift
- transport-specific agent protocols should not be baked into canonical storage too early

### Acceptance Standard

This wave should only start after the single-agent workflow model is stable and verified in real use.

## Wave 7: Cross-Repo Sweep And Drift

Status: Directional backlog

### Problem

Users operate multiple repos, and the same workflow drift can recur across them:

- stale proposal queues
- missing checkpoints
- inconsistent hook rollout
- preferences or rules diverging from intended defaults

### Goals

- surface workflow drift across managed repos
- allow safe, reviewable sweep operations
- avoid turning `dot-agents` into a continuously running control plane

### Resolved Direction

- cross-repo operations should use the existing managed-project inventory in `~/.agents/config.json`
- default behavior is read-only reporting
- mutating sweep actions should require explicit confirmation or proposal review

### Candidate CLI Surface

- `dot-agents workflow sweep`
- `dot-agents workflow drift`

### Candidate Outputs

- missing or stale checkpoints
- proposal backlog older than a threshold
- inconsistent hook or workflow preference rollout
- repos missing required active-plan structure

### Acceptance Standard

This wave is complete when a human or agent can identify workflow drift across managed repos without manual repo-by-repo inspection.

## MCP And Runtime Query Surface

Status: Directional backlog

The research repeatedly suggests an MCP surface for workflow state. That is likely correct, but the order matters.

### Resolved Direction

- Do not build a workflow MCP server until the JSON query surface from Wave 2 is stable.
- The first workflow MCP surface should be read-only and should mirror existing JSON schemas rather than inventing new ones.
- Write actions should continue to go through canonical files, hooks, and review commands until read patterns are proven.

### Initial Candidate MCP Capabilities

- get orient state
- get workflow status
- get canonical plan and task state
- get verification history summary
- get pending proposals
- get workflow preferences with precedence applied
- get graph-bridge query results

## Rollback And Lifecycle

Status: Directional backlog

The MVP review queue supports approve and reject, but longer-lived workflow automation will need clearer lifecycle controls.

### Follow-On Needs

- revert approved proposals cleanly
- expire or close stale proposals
- archive or compact old verification logs
- rotate health snapshots and generated artifacts if they become noisy

### Resolved Direction

- git history in `~/.agents/` remains the primary rollback mechanism for shared config mutations
- lifecycle automation should not silently delete workflow history

## Ideas Explicitly Not Committed

These ideas are interesting but should remain outside the committed roadmap until further research:

- a generic unified hook runner shared across all platforms
- continuous background daemons for workflow maintenance
- fully autonomous git pull/push behavior across all managed repos
- cryptographically verified shared memory or DKG-style coordination
- automatic deletion of rules or skills based only on inferred disuse
- transport-specific intent-marker protocols embedded directly into canonical storage

## Guardrails For Future Planning

- Do not reopen the MVP storage split between repo-local `.agents/` and user-local `~/.agents/` without strong evidence.
- Do not add write-capable runtime query surfaces before read-only schemas stabilize.
- Do not let health tooling or preferences bypass the approval gradient for shared behavior.
- Do not assume multi-agent coordination can be solved with last-write-wins once multiple writers are active.
- Do not let cross-repo operations become default-mutable.

## Recommended Next Planning Order

After MVP delivery, the planning order should be:

1. Wave 2: canonical plan and task artifacts
2. Wave 3: structured query and health surface
3. Wave 4: shared preferences and compatibility
4. Wave 5: knowledge-graph bridge and integration readiness
5. Wave 6 RFC: delegation and merge-back
6. Wave 7 RFC: cross-repo sweep and drift

This keeps the progression disciplined:

- first make workflow execution state deterministic
- then make state easier to read and query
- then make preferences explicit
- then bridge to external knowledge systems
- then coordinate multiple agents
- finally coordinate multiple repos
