# Agent-run scoring & observability platform — spec

**Status:** accepted (proposal 2026-05-25), promoted to spec 2026-05-25.
**Origin:** `.agents/proposals/agent-run-scoring-observability-platform.md` (proposal kept as the umbrella narrative; this spec governs the work).
**Scope:** project (dot-agents).

## Problem

dot-agents captures rich agent-run telemetry (per-iteration token/cache/usage from 5 coding-agent platforms; per-loop-run orchestration metrics in `.ralph-loop-streams`; a multi-language MCP Tree-sitter knowledge graph; workflow orchestration with JSON-schema contracts) but currently does nothing above raw capture. The iteration log's `linked_traces_to_outcomes` boolean is uncomputed — the clearest single marker of the gap.

## Goal

Land the layer above capture: score runs (R1), expose them through a service (R3) and dashboard (R2), generate-and-evaluate code tasks (R4), and collect human judgment (R5).

## Decisions

### D1 — One spec, four plans

R1 already shipped via the canonical plan `r1-outcome-scoring` (7/7 tasks completed; plan status `completed`). R2–R5 are each substantial enough to own a separate plan; trying to fold them into one would mix radically different surface areas (frontend, infra service, evaluation harness, RBAC). The umbrella spec sequences them; each plan owns its own `design.md` (if any) + `PLAN.yaml` + `TASKS.yaml`.

### D2 — R3 hosts both R2's API and R5's collection endpoint

R3 is the "background worker service" but also the natural home for the R2 dashboard backend and the R5 review-collection endpoint. Plans R2 and R5 depend on R3 being live to host their HTTP surface; R3's design must reserve API mount points for both.

### D3 — Scoring rubric is versioned

The R1 plan already shipped a versioned rubric. R4 and R5 changes that affect the rubric must bump the version — they DO NOT mutate the existing version's outputs. Re-scoring under a new rubric version is a deliberate, auditable act.

### D4 — Real-time vs. post-hoc for R2

R2's "real-time updates as runs progress" requirement implies the backend must push (SSE or WebSocket) — polling is not acceptable. This shapes R3's task framework: tasks must be able to publish events, not only persist them. Implementation tech (SSE vs. WS vs. polling-with-watermark) is a per-plan decision in R2; R3 only owns the publish primitive.

### D5 — RBAC scope (R5)

Three roles: reviewer (label runs), admin (manage users + rubric versions), read-only (dashboard view only). No further granularity in R5 v1. Per-tenant isolation is out of scope; the platform is single-tenant.

## Done criteria (umbrella)

The platform is "done" when:

1. R1: every iteration/session has a numeric score + breakdown; CLI-queryable; versioned rubric.  ✅ shipped (2026-05-25).
2. R3: the service runs ≥2 background tasks on independent schedules; task health is observable; the service hosts R2 + R5 endpoints.
3. R2: backend API serves telemetry + scores; React/TS UI renders live run health with drill-down; real-time updates.
4. R4: generator produces verifiable tasks across ≥3 languages with difficulty metadata; harness emits R1-scored outcomes; results visible in R2.
5. R5: reviewer can list/inspect/label runs; labels feed R1 as a reward signal; RBAC enforced; label/edit actions audit-logged.

## Open questions (must resolve before the relevant plan opens)

- **R3 hosting model.** Standalone daemon, sidecar to existing CLI, or a long-running `da` subcommand? Tradeoff: deployability vs. UX. Owner: R3 plan.
- **R3 task framework.** Build a minimal in-process scheduler, or adopt an existing Go task library (e.g. `asynq`, `river`)? Owner: R3 plan.
- **R2 frontend tooling.** Plain Vite + React + TanStack Query? Next.js? Tradeoff: SSR/static hosting fit vs. build complexity. Owner: R2 plan.
- **R2 storage.** Read directly from existing telemetry (`.ralph-loop-streams`, per-platform session files) or denormalize to SQLite/Postgres first? Owner: R2 plan (depends on R3 ingestion design).
- **R4 task corpus.** Generate from scratch via templates over Tree-sitter KG, or seed from an open benchmark (HumanEval, SWE-bench)? Owner: R4 plan.
- **R4 sandboxing.** How does the harness isolate agent execution (docker, fresh worktree, locked filesystem)? Owner: R4 plan.
- **R5 audit log format + retention.** Owner: R5 plan.

## Sequencing

```
R1 ─── shipped (2026-05-25, plan: r1-outcome-scoring/)
 │
 ├── R3 ─── service (foundational; can start now)
 │    ├── R2 ─── dashboard (depends on R3 + R1 scores)
 │    │    └── R5 ─── review/labeling (depends on R2 surface, R3 collection endpoint, R1 to consume labels)
 │    └── R4 ─── task gen+eval (depends on R1; benefits from R3 as a background task)
```

R3 is the highest-leverage next step — it unblocks R2 (which unblocks R5), and gives R4 a home.

## Relationship to existing plans

- **`r1-outcome-scoring`** (completed) — supplies the scores R2 displays, R4 emits, R5 augments.
- **`graph-backend-adapter-contract`** (draft) — R4's task generation will consume the KG; the adapter contract pins how.
- **`workflow-commit-command` + `workflow-client-commands`** (completed in PR #53) — supply the iteration-close telemetry R3 consumes.
- **`graphstore-concurrency-contract`** (active) — R3's KG-staleness-refresh task must respect the gcc1 contract.

## Verification (umbrella)

- `R1`: existing `da score iteration` + `da score run` CLIs query and produce scores; rubric is at `internal/scoring/rubric.go`.
- `R2`: `curl -s http://localhost:<port>/api/runs` returns JSON; UI loads at `http://localhost:<port>/`; a running iteration produces a UI event within 2s of the iteration-log write.
- `R3`: `da service status` (or equivalent) shows running tasks with health; restarting the service does not lose in-flight task state.
- `R4`: `da eval run --language go` produces an R1-scored outcome and a row in R2.
- `R5`: a reviewer account labels a run via the UI; the label appears in R1's signal mix on the next score recomputation.

## Per-R deferred to plan-level design docs

Each plan owns:
- `design.md` (this spec covers requirements; per-plan design covers implementation decisions)
- `PLAN.yaml` (status, success_criteria, verification_strategy)
- `TASKS.yaml` (concrete task breakdown)

This umbrella spec freezes only the cross-cutting decisions (D1–D5), the sequencing, and the umbrella done criteria. Plans must not contradict it without amending it here.
