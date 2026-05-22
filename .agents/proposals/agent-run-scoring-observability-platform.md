# Agent-run scoring & observability platform

Status: proposed (2026-05-22)
Scope: project (dot-agents)

## What

dot-agents captures rich agent-run telemetry but currently does nothing above raw
capture. This proposes the next layer: score runs, run a service to compute it, a
dashboard to see it, a code-task generator/evaluator, and a human review surface.

Already shipped -- the substrate this builds on:

- `platform-session-integration` -- per-iteration token / cache / usage telemetry
  read from five coding-agent platforms (Claude Code, Codex, Cursor, Copilot,
  OpenCode).
- `.ralph-loop-streams` -- per-loop-run orchestration metrics (workers spawned,
  iterations, merge-back status, context tokens).
- MCP Tree-sitter knowledge graph -- multi-language structural code analysis.
- Workflow orchestration -- multi-agent fanout, scheduling, JSON-schema contracts.

What is missing is everything *above* capture: scoring, a service to run it, a UI
to see it, task generation, and a way to collect human judgment. The iteration
log's `linked_traces_to_outcomes` boolean is uncomputed -- the clearest marker of
the gap.

## Requirements

Requirements-level; the workflow should scope each into a plan and tasks.

### R1 -- Outcome scoring for agent runs

Compute an explainable quality/success score per session and per iteration from
already-captured signals: token/cache telemetry, verifier results, merge-back
status, scope adherence, test outcomes.

Acceptance:
- Every iteration/session gets a numeric score plus a breakdown of which signals
  drove it (explainable, not a black box).
- Scores persist alongside the existing telemetry and are CLI-queryable.
- A documented, versioned scoring rubric -- changing it is a deliberate, reviewable
  act.

Depends on: nothing -- the input signals already exist. Foundational; do first.

### R2 -- Real-time observability & evaluation dashboard

A web dashboard (React/TypeScript front end + backend API) over session telemetry,
loop-run metrics, and R1 scores -- live run health, token cost, cache efficiency,
and outcome-score trends, with drill-down from an aggregate view to a single run's
iterations and signals.

Acceptance:
- Backend API serves telemetry + scores; a React/TS UI renders run health live.
- Real-time updates as runs progress (not only post-hoc).
- Drill-down to per-iteration detail works.

Depends on: R1 (scores to show), R3 (a service to host the API).

### R3 -- Background-worker service

A long-running service hosting multiple background tasks -- telemetry ingestion,
R1 scoring, knowledge-graph staleness refresh -- replacing the current hook-and-CLI-
only model. A task framework with observability into the workers themselves.

Acceptance:
- The service runs >=2 distinct background tasks on independent schedules/triggers.
- Task health and history are observable (and feed R2).
- The service hosts the R2 API and the R5 collection endpoint.

Depends on: nothing structurally; pairs with R1/R2. Foundational; do early.

### R4 -- Code-task generation & evaluation harness

Generate diverse, verifiable programming tasks across multiple languages (drawing
on the Tree-sitter knowledge graph), and a harness that runs an agent against them
and scores the result via R1.

Acceptance:
- A generator produces verifiable tasks across >=3 languages, with difficulty
  metadata.
- A harness runs an agent end-to-end on a task and emits an R1-scored outcome.
- Generated tasks + results persist and are visible in R2.

Depends on: R1 (scoring). Benefits from R3 (run as a background task).

### R5 -- Human-in-the-loop review, labeling & access layer

A review surface in the R2 dashboard: inspect agent runs, label outcomes, and
supply feedback that becomes a reward signal feeding R1 -- turning subjective review
into a labeled dataset. Plus a role-based access layer (reviewer / admin /
read-only) with an audit trail, so the surface is safe to expose beyond one user.

Acceptance:
- A reviewer can list runs, inspect a run's evidence, and record a structured
  label + free-text feedback.
- Captured labels persist and are consumable by R1 as a reward/quality signal.
- Role-based access controls who can label vs. administer; label/edit actions are
  audit-logged.

Depends on: R2 (the surface), R3 (collection endpoint), R1 (consumes the labels).

## Sequencing

1. R1 + R3 -- foundational (scoring + the service to run it). Parallelizable.
2. R2 -- the dashboard, once R1 produces scores and R3 hosts the API.
3. R4 -- code-task generation/eval, once scoring exists.
4. R5 -- review/labeling/access last; it consumes R1-R3.

R1 alone is the highest-leverage single step -- it turns the uncomputed
`linked_traces_to_outcomes` marker into a real, explainable signal.

## Provenance

Prioritization was informed by an external requirements review of a full-stack
AI-tooling role; the requirements themselves are dot-agents-native and stand on
their own. The companion analysis lives outside this repo at
`ResumeAgent/resumes/anthropic-rl-fullstack/dot-agents-gap-closing-proposal.md`.
