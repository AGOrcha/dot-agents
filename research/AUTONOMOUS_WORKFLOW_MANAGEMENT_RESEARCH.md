# Autonomous Workflow Management Research

Status: Exploratory

Last updated: 2026-03-30

## Purpose

This document captures observed workflow patterns from past Cursor, Claude Code, and Codex sessions so `dot-agents` can evolve beyond config distribution and provide workflow-management commands for autonomous agents.

This is a research note, not a final architecture or product spec.

## Source Inputs

### Claude Code

- `/Users/nikashp/.claude/history.jsonl`
- `/Users/nikashp/.claude/plans/`
- `/Users/nikashp/.claude/tasks/`
- `/Users/nikashp/.claude/file-history/`
- `/Users/nikashp/.claude/session-env/`
- `/Users/nikashp/.claude/backups/`

### Cursor

- `/Users/nikashp/.cursor/prompt_history.json`
- `/Users/nikashp/.cursor/plans/`
- `/Users/nikashp/.cursor/projects/`
- `/Users/nikashp/.cursor/ai-tracking/ai-code-tracking.db`
- `/Users/nikashp/.cursor/cli-config.json`
- `/Users/nikashp/Library/Application Support/Cursor/User/History`

### Codex

- `/Users/nikashp/.codex/history.jsonl`
- `/Users/nikashp/.codex/session_index.jsonl`
- `/Users/nikashp/.codex/state_5.sqlite`
- `/Users/nikashp/.codex/archived_sessions/`

## High-Level Conclusion

The session evidence shows that the current tools already behave like a workflow system:

- work is resumed repeatedly across short sessions
- plans are persisted and revisited
- verification is part of the normal loop
- context is externalized into files and artifacts
- subagents are used as a workflow primitive, not just an optimization

`dot-agents` currently manages configuration and platform projection well, but it does not yet manage the workflow state that autonomous agents actually need.

The main gap is not "more config families".

The main gap is a shared workflow layer.

## Observed Cross-Platform Patterns

### 1. Resume And Re-entry Are First-Class Workflows

This is the strongest signal.

Evidence:

- Claude: `/resume` is the top command at `35` uses.
- Claude: `22` of `66` sessions start with `/resume`.
- Cursor: `8` resume-style commands (`/resume` or `/resume-chat`) in prompt history.
- Codex: `23` of `177` prompts explicitly reference `.agents/active`, handoffs, or prior implementation history.

Interpretation:

- work is often interrupted
- agents frequently need to reconstruct state
- session continuity is not reliably held in tool-local memory alone

Implication for `dot-agents`:

- `resume` should be a first-class command, not a side effect of reading random files

### 2. Planning Is Persistent, Not Just Advisory

Evidence:

- Claude: `15` saved plan docs under `.claude/plans`
- Claude: `32` history entries mention `plan` or `research`
- Cursor: `31` saved plan files under `.cursor/plans`
- Cursor: `22` prompt-history entries mention `plan`
- Codex: `49` of `177` prompts mention `plan`
- Codex: exact follow-up `Implement the plan.` appears `7` times

Plan artifacts are also structured:

- Claude plans frequently include `Context`, `Verification`, and step-based sections
- Cursor plans track todo status
- one Claude task set showed dependency-aware phases with `blocks` and `blockedBy`

Interpretation:

- planning is already an execution primitive
- users do not treat plans as one-off brainstorming artifacts

Implication for `dot-agents`:

- plans and task graphs should be canonical workflow resources

### 3. Verify-As-You-Go Is Normal

Evidence:

- Codex tool logs heavily feature `git status --short`, `go test`, and `git diff --stat`
- Cursor histories show strong verification hygiene through formatting, lint, typecheck, and tests
- Cursor edit history includes large volumes of diff review and undo/accept/reject actions
- Claude plans commonly include verification sections and audit/check prompts

Interpretation:

- verification is not a final step
- agents need to know what has already been verified and what remains

Implication for `dot-agents`:

- verification status should be persisted and reusable across sessions

### 4. Workflow State Is Already Externalized, But Fragmented

Evidence:

- Claude stores plans, tasks, file history, backups, and session environment snapshots
- Cursor stores plans, project-scoped approval/cache files, worker logs, trust markers, and repo metadata
- Codex stores history, thread index, archived rollout transcripts, and references to `.agents/active` and handoff artifacts

Interpretation:

- the user already relies on files as workflow memory
- the problem is inconsistency of format and location, not lack of demand

Implication for `dot-agents`:

- introduce canonical workflow artifacts and let platforms consume or augment them

### 5. Tooling Setup And Approval State Are Part Of The Workflow

Evidence:

- Claude has repeated `/mcp`, `/login`, `/remote-control`, and `/rate-limit-options` usage
- Cursor stores `mcp-approvals.json`, `mcp-cache.json`, trust markers, and CLI allowlists
- Codex logs show auth/model refresh failures and session health noise

Interpretation:

- setup friction is not separate from the workflow
- approvals, tool health, and environment readiness shape whether an agent can continue working

Implication for `dot-agents`:

- workflow commands should include tooling-health and approval-awareness

### 6. Subagent Fan-out Is A Real Workflow Primitive

Evidence:

- Codex state showed a substantial number of `worker` and `explorer` threads
- archived Codex sessions included repeated `spawn_agent` and `wait_agent` calls
- Cursor and Claude histories also show plan decomposition and staged execution patterns

Interpretation:

- parallel delegation is not an edge case
- coordination, ownership, and merge-back become workflow concerns

Implication for `dot-agents`:

- autonomous workflow support should include bounded fan-out and merge-aware commands

## Pain Points

### Context Fragmentation

Symptoms:

- repeated resume commands
- stale handoffs
- repeated rereading of the same plans, docs, and skill files

Result:

- time lost at session start
- repeated context reconstruction

### MCP, Auth, And Tool-Health Friction

Symptoms:

- repeated MCP inspection and setup commands
- login/remote-control churn
- missing or unhealthy tool outputs

Result:

- work stalls before coding begins

### Verification Churn

Symptoms:

- repeated git status/test/diff loops
- environment-specific failures such as testcontainer or service availability issues

Result:

- the agent spends time rediscovering what is already broken versus what it just caused

### Preference Relearning

Symptoms:

- follow-up corrections like test framework preference, verification expectations, plan updates, or command conventions

Result:

- small repeated mismatches add avoidable interaction overhead

### Multi-Repo Drift

Symptoms:

- repeated work across sibling repos
- repeated config/pipeline/migration tasks in similar repository families

Result:

- the same workflow is manually reconstructed repo by repo

### Redundant Delegation

Symptoms:

- Codex showed repeated first prompts across spawned threads
- similar context gathering happens multiple times

Result:

- duplicated exploration and token/tooling waste

## Common Workflow Hygiene Practices

These are behaviors worth supporting directly.

### Strong Planning Discipline

- plans are saved, not ephemeral
- plans often include explicit scope, milestones, deferred work, and verification

### Frequent Git-State Inspection

- agents routinely check git state before and during work

### Verification As A Habit

- formatting, linting, typechecking, testing, and diff review happen throughout the session

### Explicit Continuity Artifacts

- plans, tasks, handoffs, and active-history references are used to continue work without re-explaining everything

### Review Before Acceptance

- Cursor diff review patterns suggest edits are inspected, not blindly accepted

### Scoped Delegation

- larger tasks are broken into sub-work or helper roles when the workflow supports it

## What `dot-agents` Should Manage

The evidence suggests `dot-agents` should manage six workflow concerns:

1. resume context
2. plan and task state
3. verification state
4. approvals and tool health
5. repo preferences
6. delegation and handoff state

This is adjacent to configuration, but not the same thing.

## Candidate Command Surface

These commands are grouped as workflow management, not platform configuration.

### Resume And Continuity

- `dot-agents workflow resume`
  - collect the active plan, last handoff, recent git state, recent verification, and likely next step
- `dot-agents workflow checkpoint`
  - persist files touched, commands run, tests run, blockers, and next action before pause
- `dot-agents workflow handoff`
  - package state into a continuation bundle for another agent or a future session

### Planning And Tasking

- `dot-agents workflow plan`
  - create or update a canonical plan artifact
- `dot-agents workflow tasks`
  - manage task graphs, statuses, dependencies, and blockers
- `dot-agents workflow advance`
  - promote future work to active work, split into subplans, or archive completed work

### Verification And Drift

- `dot-agents workflow verify`
  - run repo-appropriate checks and persist results
- `dot-agents workflow drift`
  - compare plan state, repo state, and emitted config state to surface mismatches
- `dot-agents workflow doctor`
  - validate workflow health, not only config health

### Tooling And Approval State

- `dot-agents workflow approvals`
  - inspect and normalize MCP/plugin/trust approvals where possible
- `dot-agents workflow tool-health`
  - surface auth expiry, rate-limit risk, tool unavailability, and environment prerequisites
- `dot-agents workflow env capture`
  - save reusable runtime or dev environment assumptions for future sessions

### Preferences And Reuse

- `dot-agents workflow prefs`
  - persist per-repo habits such as test commands, CI expectations, plan location, and review preferences
- `dot-agents workflow sweep`
  - apply repeated workflow operations across sibling repos and report drift

### Fan-out And Delegation

- `dot-agents workflow fanout`
  - spawn bounded workers with ownership constraints and merge summaries
- `dot-agents workflow merge-back`
  - collect child summaries, verification results, and unresolved conflicts into one parent continuation artifact

## Candidate Canonical Resources

If `dot-agents` adds a workflow layer, these resource types would likely be needed:

- `PLAN.yaml`
- `TASKS.yaml`
- `CHECKPOINT.json`
- `HANDOFF.md`
- `VERIFY.json`
- `APPROVALS.json`
- `PREFS.json`

These could live under a new top-level bucket such as:

```text
~/.agents/workflows/<scope>/<workflow-name>/
```

or repo-local state if portability is more important than user-home centralization.

This remains open.

## Relationship To Existing `dot-agents` Scope

Current `dot-agents` strengths:

- canonical config ownership
- platform-specific emission
- import/refresh/status/doctor around config distribution

What the session evidence adds:

- autonomous agents also need reusable workflow state
- workflow state has its own lifecycle, drift, portability, and health concerns

In other words:

- config management is necessary
- workflow management is the next missing layer

## Suggested MVP

The smallest useful workflow-management MVP would likely be:

1. `dot-agents workflow resume`
2. `dot-agents workflow checkpoint`
3. `dot-agents workflow verify`

Why these three first:

- resume solves the strongest repeated pain point
- checkpoint makes resume reliable
- verify prevents repeated rediscovery of repo health

## Questions To Explore Next

These need more research before an implementation RFC.

1. Should workflow state live in `~/.agents/`, in the repo, or split between the two?
2. Which workflow artifacts should be portable and committed versus local and ephemeral?
3. Should plan/task/checkpoint/handoff be one family or several smaller families?
4. How should `dot-agents` merge platform-native workflow artifacts with canonical workflow artifacts?
5. What is the minimum machine-readable JSON surface autonomous agents need before new CLI commands are added?
6. How should fan-out ownership be expressed so subagents do not overlap in write scope?
7. How much approval/tool-health state can `dot-agents` manage directly versus only report?
8. What should the compatibility story be for existing plan or handoff files already used in repos?

## Recommended Next Step

Do not jump straight to implementation.

Write a follow-on workflow RFC that answers:

- canonical workflow artifact layout
- command surface and JSON outputs
- portability and commit policy
- interaction with existing config/resource architecture
- staged rollout plan
