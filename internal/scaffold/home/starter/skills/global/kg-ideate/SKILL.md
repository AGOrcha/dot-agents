---
name: kg-ideate
description: |-
  KG-grounded front-end to the whole artifact pipeline: idea/proposal → spec → plan →
  concurrent staged execution, solo or with subagents. Phases: (1) Briefing Producer —
  queries KG, research corpus, and lessons to produce a structured context block;
  (2) Spec Scaffolding — uses the briefing to resolve contradictions, convert gaps to
  open questions, write the spec; (3) Plan Scaffolding — turns the stabilized (or
  concurrently-drafted) spec into a plan with write-scopes and dependency ordering;
  (4) Execution Handoff — feeds the spec+plan into the fanout/ISP staged-execution flow.
  Use when starting a new spec/plan, when the user asks what we know about a topic, or
  when prepping work for delegation.
argument-hint: "<topic-or-task-id>"
tier: compound
calls:
  - kg-brief
  - spec-scaffold
  - plan-scaffold
  - staged-execution-handoff
verifier: batch
review_gate: default
---

# KG-Ideate

KG-grounded front-end to the whole artifact pipeline. Dispatches in order to four
molecule skills: Phase 1 briefs; Phase 2 scaffolds the spec; Phase 3 scaffolds the
plan; Phase 4 hands off to concurrent staged execution. Works whether the planner is
the orchestrator solo or a delegated subagent.

The four-tier artifact model (spec → plan → tasks → history) governs all output
(per `workflow-artifact-model`).

## Tiered composition

`kg-ideate` is a **T2 compound orchestrator**. It does not inline the phase logic; it
**dispatches in order** to four **T1 molecule** skills (the `calls:` list above).
Composition is declared, not discovered at runtime. Dispatch depth is compound → molecule
→ atom = **2 hops** — within the reliable bound and honoring "push composition into the
skill."

**Discoverability is split by design.** The skill loader scans each `skills/<scope>/`
directory ONE level deep, so only a top-level directory is a discoverable, independently
invocable skill — a molecule nested a second level under `kg-ideate/` is not. Two
molecules genuinely need standalone invocation and live as top-level `skills/global/`
siblings of `kg-ideate` for that reason: `spec-scaffold` (invoke when a briefing already
exists) and `plan-scaffold` (invoke when a spec is already stable). The other two,
`kg-brief` and `staged-execution-handoff`, are compound-only — they stay nested under
`kg-ideate/` and are dispatched only as a step of this orchestrator's own flow; they are
deliberately not independently discoverable.

| Phase | Molecule | Tier | Owns | Verifier | Standalone? |
|-------|----------|------|------|----------|-------------|
| 1 | `kg-brief` | molecule | KG/research/lessons traversal → the shared briefing block | batch | No — nested under `kg-ideate/`, compound-only |
| 2 | `spec-scaffold` | molecule | briefing → decisions/open-questions → `design.md` | batch | Yes — top-level `skills/global/spec-scaffold/` |
| 3 | `plan-scaffold` | molecule | spec → tasks/write-scopes/dep-order → PLAN/TASKS | batch | Yes — top-level `skills/global/plan-scaffold/` |
| 4 | `staged-execution-handoff` | molecule | spec+plan → fanout/ISP staged execution | batch | No — nested under `kg-ideate/`, compound-only |

`kg-brief` and `staged-execution-handoff`'s step-by-step bodies are authored in their own
`SKILL.md` nested under `kg-ideate/`. `spec-scaffold` and `plan-scaffold`'s bodies live in
top-level sibling skill directories, so they can be invoked directly — the orchestrator is
the convenience path over those two reusable primitives, not their only entry point. The
orchestrator's own logic is only: brief → (spec ⇄ plan, per the D5 concurrency fork below)
→ handoff, passing the shared briefing/spec/plan artifacts between molecules.

## Orchestration flow

Before dispatching Phase 1, review common failure points across the whole pipeline.
Load → `instructions/gotchas.md`

### Phase 1 — Briefing Producer → `kg-brief`

Dispatch to `kg-brief`. It queries the knowledge graph, research corpus, and lessons
index, then renders the structured briefing block. The briefing block is the single
shared artifact consumed by all downstream phases. Do not write any spec or plan file
before the briefing is produced and presented to the user.

See `kg-brief/SKILL.md` for the full molecule workflow (KG traversal, corpus scan,
lessons/spec-overlap check, briefing render).

### Phase 2 — Spec Scaffolding → `spec-scaffold`

Dispatch to `spec-scaffold` with the Phase 1 briefing as input. It guides prior
decision review, gap → open question conversion, contradiction framing (adapter-
conditional per `kg-brief`'s kg-queries.md preflight), done criteria draft, and writes
`.agents/workflow/specs/<id>/design.md`. Canonical spec root is always
`.agents/workflow/specs/` — never the bare `workflow/specs/` path.

See `[[spec-scaffold]]` (top-level `skills/global/spec-scaffold/SKILL.md`) for the full
molecule workflow (steps 5–9).

### Phase 3 — Plan Scaffolding → `plan-scaffold` (spec⇄plan concurrency fork)

**D5 — Spec↔plan concurrency fork (this compound owns the decision).**
Before dispatching to `plan-scaffold`, decide the concurrency mode per the
`workflow-artifact-model` rule "spec before plan… unless the work is purely mechanical":

- **Sequential (default):** dispatch `plan-scaffold` only after `spec-scaffold` has
  confirmed the spec's open questions. Use when the spec still has behavioral questions
  whose answers change task ordering or write-scopes.
- **Concurrent / folded:** dispatch `plan-scaffold` interleaved with `spec-scaffold`
  when the work is purely mechanical or all decisions are already clear from the
  briefing (no open questions that affect ordering or write-scopes). A thin spec may
  be co-authored alongside the plan.

The chosen mode and rationale are passed to `plan-scaffold` and recorded in plan notes.

See `[[plan-scaffold]]` (top-level `skills/global/plan-scaffold/SKILL.md`) for the full
molecule workflow (steps 10–14: concurrency decision, task breakdown, write-scope
derivation from Phase 1 impact radius, verification strategy, plan authoring via
`da workflow plan create <id>`).

### Phase 4 — Execution Handoff → `staged-execution-handoff`

Dispatch to `staged-execution-handoff` with the briefing + spec + plan as orientation
context. It makes the direct-vs-fanout decision and hands the artifacts into the
orchestrator/staged runtime. Cross-ref `[[orchestrator-session-start]]` and `[[isp]]`.
The layered-pr-fanout flow (`.agents/workflow/specs/layered-pr-fanout/design.md`) is
the staged-PR target this handoff feeds. Cross-ref `[[delegation-lifecycle]]` for
fanout bundle authoring.

`kg-ideate` produces no code. The orchestrator owns the staged runtime.

See `staged-execution-handoff/SKILL.md` for the full molecule workflow (steps 15–16).

## Subagent-planning path

`kg-ideate` works whether the planner is the orchestrator solo or a delegated subagent:

- **Solo:** the orchestrator runs all four phases in one session.
- **Delegated:** the orchestrator runs Phase 1 (or hands a topic to a spawned planner
  that runs Phase 1 itself), then delegates Phases 2–3 to a planning subagent. The
  Phase 1 briefing block is the cold-start context the subagent consumes — it is
  self-contained by design, so a spawned planner needs only the briefing + topic to
  scaffold spec/plan without further orientation. The subagent returns the spec + plan;
  the orchestrator runs Phase 4 handoff. Cross-ref `[[delegation-lifecycle]]`.

## Integration notes

- **agent-start**: run `kg-ideate` after `[[agent-start]]` when the session goal is new
  work (a spec, a plan, or both).
- **orchestrator-session-start / isp**: Phase 4 passes briefing + spec + plan into
  `[[orchestrator-session-start]]` (pick task → KG readback → decide) and `[[isp]]`
  (task selection → direct vs fanout → staged impl → verify → review → parent gate).
  `kg-ideate` is the front-end; the orchestrator owns the staged runtime.
- **workflow-artifact-model**: the four-tier model governs all output. Phase 2 writes
  the spec tier only (no file paths, function names, task lists). Phase 3 owns the plan
  tier. The D5 concurrency fork above is a direct application of the model's
  "spec before plan… unless purely mechanical" rule.
- **layered-pr-fanout**: `[[layered-pr-fanout]]` is the staged-PR target Phase 4 feeds.
- **KG query surface**: Phase 1 is CLI-first (`da kg query`), MCP fallback. Structural
  contradiction traversal (Phase 2 step 7) is adapter-gated and degrades gracefully:
  enabled when the active adapter ships `contradicting_claims`
  (e.g. `dotagents-builtin:graph/citation@^1.0`), falls back to competing-decisions
  treatment otherwise. No other phase depends on it.
