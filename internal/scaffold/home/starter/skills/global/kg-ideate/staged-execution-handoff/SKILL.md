---
name: "staged-execution-handoff"
description: |-
  Phase 4 of kg-ideate: receives the spec+plan from plan-scaffold and hands them into the
  fanout/ISP staged-execution flow. Makes the direct-vs-fanout decision; passes briefing +
  spec + plan as orientation context into [[orchestrator-session-start]] / [[isp]]. Emits
  no code — the orchestrator owns the staged runtime. Feeds [[layered-pr-fanout]].
argument-hint: "<plan-id>"
tier: molecule
calls:
  - execution-handoff
verifier: batch
review_gate: default
---

# Staged-Execution Handoff

Phase 4 of `kg-ideate`. The pipeline's tail: the spec+plan produced in Phases 2–3 are
handed into the concurrent staged-execution flow (fanout / ISP). This molecule does
**NOT** implement — the orchestrator owns the staged runtime.

Load → `instructions/execution-handoff.md`

## Workflow

15. **Direct vs fanout decision**
    Decide whether the work is implemented directly (solo) or fanned out to subagents.
    For fanout, write per-task constraints into TASKS.yaml notes and produce delegation
    bundles. Cross-ref `[[delegation-lifecycle]]`.

16. **Hand to staged runtime**
    Pass the briefing + spec + plan as orientation context into the orchestrator/staged
    runtime. Cross-ref `[[orchestrator-session-start]]` (pick task → KG readback → decide)
    and `[[isp]]` (task selection → direct vs fanout → staged impl → verify → review →
    parent gate). The layered-pr-fanout flow
    (`.agents/workflow/specs/layered-pr-fanout/design.md`) is the staged-PR target this
    handoff feeds.
