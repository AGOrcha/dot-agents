# Planner Evidence-Backed Write Scope — Canonical Plan

**Plan ID:** planner-evidence-backed-write-scope
**Status:** active
**Spec:** [design.md](../../specs/planner-evidence-backed-write-scope/design.md)
**Upstream dependencies:**
- [graph-bridge-command-readiness](../graph-bridge-command-readiness/PLAN.yaml) (completed)
- [kg-command-surface-readiness](../kg-command-surface-readiness/PLAN.yaml) — Slice 1 gates `derive-scope-command`

---

## Why this exists

The repo has canonical workflow plans with `write_scope` on every task. That scope is
currently authored as human intuition — the planner lists files that seem right, and
downstream workers execute against that list without knowing why those files were chosen
or what was intentionally excluded.

This creates three repeating failure modes:

1. **Under-scoping:** a task misses callers, tests, or mirrored paths because nothing
   ties scope authoring to graph readback.
2. **Over-scoping:** broad directory-level scope (`commands/workflow/`) because the
   planner cannot prove a tighter safe boundary.
3. **Lost context:** cold-start workers cannot reconstruct planner intent from the scope
   list alone, so they improvise on decisions that were already made.

The graph surface exists and is increasingly reliable after the bridge resurrection work.
The gap is that planning does not preserve query results as first-class evidence. This
plan closes that gap incrementally, starting with the schema and working outward to
commands, skills, and fanout integration.

---

## Key decisions and invariants (do not reopen without a fold-back)

1. **Sidecar first, TASKS.yaml schema change never (in this plan).** The sidecar lives at
   `.agents/workflow/plans/<plan>/evidence/<task>.scope.yaml`. TASKS.yaml `write_scope`
   remains the execution boundary. The sidecar is the explanation layer, not a replacement.

2. **derive-scope is read-only.** It does not auto-edit TASKS.yaml, even when it has high
   confidence. Planner review stays explicit. This is a Phase 2 constraint; if Phase 4
   ever changes it, that is a new plan decision.

3. **Degrade honestly.** When the graph is not ready, derive-scope emits `confidence: low`
   and records the missing evidence in `open_gaps`. It never pretends to have better
   evidence than it does.

4. **Two lanes, two purposes.** Scope lane (blast-radius, callers, tests) justifies
   `write_scope`. Context lane (decisions, contradictions, plan_context) fills the
   execution contract fields (`decision_locks`, `required_reads`, `stop_conditions`).
   Both lanes matter; a sidecar with only scope evidence still leaves workers guessing
   about what not to touch architecturally.

5. **Warnings before enforcement.** fanout emits warnings for missing evidence; it does
   not block. Enforcement is explicitly out of scope for this plan. Phase 4 enforcement
   belongs in a separate plan decision after Phase 2-3 adoption proves the shape is useful.

6. **derive-scope depends on kg-freshness-impl landing.** Do not start the
   `derive-scope-command` task until `kg-command-surface-readiness/kg-freshness-impl`
   is marked completed and the "graph ready" contract is published in docs.

---

## Task sequence

```
sidecar-schema
  ├─► sidecar-manual-experiment ─► derive-scope-command ─┐
  ├─► check-scope-command ◄──────────────────────────────┘
  └─► skill-upgrades ◄───────────────────────────────────┘
                │
                └─► fanout-evidence-integration ◄── check-scope-command
```

`sidecar-schema` and `sidecar-manual-experiment` can start immediately.
`derive-scope-command` is blocked on both `sidecar-manual-experiment` and
`kg-command-surface-readiness/kg-freshness-impl`.
`check-scope-command` and `skill-upgrades` can run in parallel after `derive-scope-command`.
`fanout-evidence-integration` is the final task and requires both.

---

## Sidecar artifact contract

Location: `.agents/workflow/plans/<plan_id>/evidence/<task_id>.scope.yaml`

Key fields and their purpose:

| Field | Purpose |
|---|---|
| `decision_locks` | Already-decided constraints the worker must not reopen |
| `required_reads` | Exact files the worker loads before editing |
| `seeds` | Starting symbols or paths the planner identified |
| `queries` | Graph queries run and their result summaries |
| `required_paths` | Files the planner believes are in-scope |
| `optional_paths` | Files likely to need review but not confirmed in-scope |
| `excluded_paths` | Transitive candidates intentionally left out, with rationale |
| `final_write_scope` | The normalized bounded set copied to TASKS.yaml |
| `provides` / `consumes` | Provider-consumer contract for adjacent slices |
| `verification_focus` | Concrete proof target, not just "run tests" |
| `stop_conditions` | The fold-back trigger: when the worker must escalate |
| `confidence` | `high / medium / low` — honest signal for downstream consumers |

Full schema: `schemas/workflow-scope-evidence.schema.json` (added by `sidecar-schema` task).

---

## Skill changes summary

| Skill | Change |
|---|---|
| `orchestrator-session-start` | Load sidecar as execution contract before fanout; recommend derive-scope if absent |
| `agent-start` | Surface `decision_locks` and `required_reads` from sidecar at session start |
| `plan-wave-picker` | Use sidecar confidence as tiebreaker between equally-ready tasks |
| `review-pr`, `review-delta`, `self-review` | Not in this plan — noted in spec §8 as Phase 3+ |

---

## Out of scope

- Auto-editing TASKS.yaml `write_scope` from derive-scope output
- Hard enforcement in fanout (warnings only in this plan)
- Evidence for doc-only or research-only tasks
- Changes to TASKS.yaml schema
- Graph building (owned by kg-command-surface-readiness)
- review-pr, review-delta, self-review skill upgrades (Phase 3+)
