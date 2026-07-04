---
name: plan-scaffold
description: |-
  spec → tasks/write-scopes/dep-order → PLAN/TASKS. Turns a stabilized spec into a
  plan with concrete bounded tasks, explicit depends_on ordering (cross-plan deps as
  <plan-id>/<task-id>), write-scopes grounded from the Phase 1 impact radius, and
  per-task verification strategies that trace back to the spec's done criteria.
  Sequential by default; concurrent/folded only when no open questions change task
  ordering or write-scopes. Authors the plan via da workflow plan create <id>.
argument-hint: "<spec-id>"
tier: molecule
calls:
  - task-breakdown
  - write-scope-derive
  - plan-write
verifier: batch
review_gate: default
---

# Plan Scaffold

Turns a stabilized spec into a full plan: task breakdown with dependency ordering,
write-scopes grounded from the Phase 1 impact radius, and per-task verification
strategies that trace back to the spec's done criteria.

Invoke independently when a spec is already stable, or as Phase 3 of `kg-ideate`
after Phase 2 (`spec-scaffold`) has confirmed the spec's open questions.

## Spec↔plan concurrency fork

Spec↔plan concurrency is a deliberate fork, governed by the `workflow-artifact-model`
rule "Spec before plan… unless the work is purely mechanical." Decide the mode before
drafting:

- **Sequential (default):** the plan waits on the spec stabilizing. Run this skill only
  after Phase 2's spec decisions and open questions are confirmed. Use when the spec still
  has open behavioral questions whose answers change the task breakdown.
- **Concurrent / folded:** the plan can be drafted at the same time as (or folded into) the
  spec when the work is purely mechanical or the decisions are already clear from the
  briefing (no unresolved open questions that affect ordering or write-scopes). In this
  mode, Phase 2 and Phase 3 run interleaved against the same briefing, and a thin spec may
  be co-authored alongside the plan rather than fully stabilized first.

Load → `instructions/plan-scaffolding.md`

## Workflow

10. **Concurrency decision**
    Apply the heuristic above. State the chosen mode and why (which open questions, if any,
    block sequential→concurrent). This decision is itself recorded in the plan notes.

11. **Task breakdown + dependency ordering**
    Load → `instructions/task-breakdown.md`
    Turn spec requirements into concrete, bounded tasks. Order them with explicit
    depends_on relationships. Cross-plan deps use `<plan-id>/<task-id>` form.

12. **Write-scopes per task**
    Load → `instructions/write-scope-derive.md`
    Derive each task's write_scope. Where Phase 1 ran get_impact_radius_tool, use the
    impact radius to ground write-scopes rather than guessing.

13. **Verification strategy per task**
    Each task names how it will be verified; the plan's success criteria trace back to the
    spec's done criteria (Phase 2 step 8). Do not invent contradicting criteria.

14. **Write plan**
    Load → `templates/plan-output.md`
    Author the plan via the canonical CLI surface (`da workflow plan create <id>`), then keep
    PLAN.yaml / TASKS.yaml / <id>.plan.md in sync. Do not hand-edit task status.
