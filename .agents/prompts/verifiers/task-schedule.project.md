# Task-schedule verifier — repo project overlay (DAG soundness)

Use this file as **`--prompt-file`** when the delegated role is **dependency-graph verification of a generated task schedule only**: read **`impl-handoff.yaml`**, confirm the `TASKS.yaml` DAG the ideation task produced is **sound** — every `depends_on` id resolves, there are no cycles, and `da workflow eligible` reports a consistent ready/blocked partition with no conflicts — then emit **`.agents/active/verification/<task_id>/task-schedule.result.yaml`** validated against **`schemas/verification-result.schema.json`**.

This is the **third** verifier in the ideation sequence (`schema-check → citation-check → task-schedule`): it runs once the artifact is structurally valid and its references resolve. It checks the **schedule encoded in the design artifact**, not code — a dangling dependency or a cycle silently stalls or misorders the downstream implementation wave.

## Role boundary

| Surface | Responsibility |
|--------|----------------|
| Shared stage instruction base (parent-resolved; not an `app_type` profile) | Stable evidence, scope, and sandbox discipline; no merge-back or parent closeout |
| Stage-safe repo project overlay | Paths, dependency-format rules, guardrails |
| **This file (`verifiers/task-schedule.project.md`)** | Repo wording for **task-schedule** turns: resolve deps, detect cycles, cross-check `da workflow eligible`, record **`task-schedule.result.yaml`** |
| Delegation bundle | Canonical `plan_id`, `task_id`, `feedback_goal`; authoring scope is **not** yours |

Do **not** edit the task graph to make it schedulable. Prefer failing the run with a clear `summary` and `status: fail` when a dependency dangles or a cycle exists.

## Preconditions

1. **Cold-start from** `.agents/active/verification/<task_id>/impl-handoff.yaml` (see Phase 8 impl-handoff in `docs/LOOP_ORCHESTRATION_SPEC.md`).
2. Confirm `ready_for_verification: true` before treating a clean partition as meaningful; if `false`, record `status: partial` or `unknown` with explanation.
3. Use **`write_scope_touched`** to find the `TASKS.yaml` (and any companion `PLAN.yaml`) the task authored or changed. Note the dependency format: a `depends_on` string is **cross-plan** when it contains `/` (`<plan-id>/<task-id>`); a bare id is local to the same plan.

## Commands (resolve → acyclic → partition)

**Order:**

1. **Dependency resolution (required):** every `depends_on` id resolves to a task that exists — locally within the same plan, or, for the `<plan-id>/<task-id>` cross-plan form, to a task in the named plan. A dep pointing at a non-existent task is a **dangling dependency** → `fail`.
2. **Acyclicity (required):** the dependency graph has no cycles. Report the participating ids of the first cycle found → `fail`.
3. **Eligibility cross-check (required):** `da workflow eligible --json`
   The ready/blocked partition it reports must be consistent with the graph — a task with all deps satisfied is `ready`, a task with an unsatisfied dep is `blocked` — and it must report **no conflicts**. A conflict or an inconsistent partition is a `fail`; cite the offending task id.

If a required check fails, you may skip the later checks but must set `status: fail` and name the first offending task/dep in `summary`.

## Result artifact

**Path:** `.agents/active/verification/<task_id>/task-schedule.result.yaml`

Minimal shape (schema-enforced):

| Field | Value |
|-------|--------|
| `schema_version` | `1` |
| `task_id` | Same as bundle / impl-handoff |
| `parent_plan_id` | Canonical plan id |
| `verifier_type` | `task-schedule` |
| `status` | `pass` \| `fail` \| `partial` \| `unknown` |
| `summary` | Deps resolved/dangling, any cycle (ids), eligible partition consistency + conflicts |
| `recorded_at` | RFC3339 timestamp |
| `commands` | The `da workflow eligible --json` line and any resolution checks run |
| `artifact_paths` | Optional: captured eligible output, if saved |

Optional: `delegation_id`, `recorded_by` when tied to fanout or automation.

## Evidence classification

Classify the verification story in prose (and optionally in `summary`): `ok`, `ok-warning`, `impl-bug`, `tool-bug`, `missing-feature`, `blocked` — align with Phase 8 taxonomy in `docs/LOOP_ORCHESTRATION_SPEC.md`. A cycle or dangling dep is `impl-bug` (the authoring stage produced an unschedulable graph); an inconsistency that traces to `da workflow eligible` itself is `tool-bug`.
